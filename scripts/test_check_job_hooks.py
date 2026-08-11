#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the ArgoCD Job hook check.

Run: python3 -m unittest discover -s scripts -p 'test_*.py'

stdlib unittest, matching test_check_networkpolicy_selectors.py — this is the only Python
in the repo and one script does not justify a test-runner dependency.
"""

import unittest

import check_job_hooks as check


def _job(name, annotations=None):
    meta = {"name": name}
    if annotations is not None:
        meta["annotations"] = annotations
    return {"apiVersion": "batch/v1", "kind": "Job", "metadata": meta, "spec": {}}


def _hooked(name, phase="Sync", delete="BeforeHookCreation"):
    return _job(name, {
        "argocd.argoproj.io/hook": phase,
        "argocd.argoproj.io/hook-delete-policy": delete,
    })


class TestHookAnnotations(unittest.TestCase):
    """The two annotations that together make a Job re-appliable."""

    def test_a_job_with_both_annotations_passes(self):
        failures, checked = check.evaluate([_hooked("hermes-migrate", phase="PreSync")])
        self.assertEqual(failures, [])
        self.assertEqual(checked, 1)

    def test_the_natsprovision_defect_a_job_with_no_hook_at_all(self):
        # Exactly what deploy/k8s/base/nats-provision-job.yaml rendered before PR #80: the
        # annotations were present only as YAML comments, which render as nothing. The
        # second Kargo promotion then fails with `spec.template: field is immutable`.
        docs = [
            _hooked("hermes-migrate", phase="PreSync"),
            _job("hermes-natsprovision", {"kubernetes.io/description": "declares streams"}),
        ]
        failures, checked = check.evaluate(docs)
        self.assertEqual(checked, 2)
        self.assertEqual([name for name, _ in failures], ["hermes-natsprovision"])
        self.assertIn("argocd.argoproj.io/hook", failures[0][1])

    def test_a_job_with_no_annotations_block_at_all(self):
        # A Job whose metadata has no `annotations` key must not raise; commenting the whole
        # block out is the shape this gate exists to catch.
        failures, _ = check.evaluate([_job("bare")])
        self.assertEqual([name for name, _ in failures], ["bare"])

    def test_a_hook_without_a_delete_policy_is_reported(self):
        # `hook` alone does not fix immutability. ArgoCD's default happens to delete before
        # creation today, but ADR 0006 chose BeforeHookCreation on evidence and both Jobs
        # state it; depending on an upstream default for the property that prevents the
        # failure is the same shape as the defect.
        docs = [_job("j", {"argocd.argoproj.io/hook": "PreSync"})]
        failures, _ = check.evaluate(docs)
        self.assertEqual([name for name, _ in failures], ["j"])
        self.assertIn("hook-delete-policy", failures[0][1])


class TestPhaseIsNotPinned(unittest.TestCase):
    """The two Jobs use different phases for reasons, so the gate must not demand one.

    hermes-migrate is PreSync: Aurora is external and already exists. hermes-natsprovision
    cannot be, because the NATS StatefulSet it provisions is created by the same Application
    during the Sync phase — PostSync and a lone later sync-wave were both measured to
    deadlock (PR #80). A gate asserting `PreSync` would have forced the broken variant.
    """

    def test_every_real_argocd_phase_is_accepted(self):
        for phase in ("PreSync", "Sync", "PostSync", "SyncFail", "Skip"):
            with self.subTest(phase=phase):
                failures, _ = check.evaluate([_hooked("j", phase=phase)])
                self.assertEqual(failures, [])

    def test_comma_separated_phases_are_accepted(self):
        # ArgoCD permits a Job to register for more than one phase.
        failures, _ = check.evaluate([_hooked("j", phase="PreSync,PostSync")])
        self.assertEqual(failures, [])

    def test_a_misspelled_phase_is_reported(self):
        # This is the mutant that matters: ArgoCD silently ignores an unrecognised phase, so
        # the Job reverts to an ordinary tracked resource and the immutability failure comes
        # straight back — with the annotation visibly present in review.
        for phase in ("presync", "PRESYNC", "Presync", "pre-sync", "sync "):
            with self.subTest(phase=phase):
                failures, _ = check.evaluate([_hooked("j", phase=phase)])
                self.assertEqual([name for name, _ in failures], ["j"])
                self.assertIn("not an ArgoCD hook phase", failures[0][1])

    def test_an_empty_phase_is_reported(self):
        failures, _ = check.evaluate([_hooked("j", phase="")])
        self.assertEqual([name for name, _ in failures], ["j"])


class TestDeletePolicy(unittest.TestCase):
    def test_before_hook_creation_is_accepted(self):
        failures, _ = check.evaluate([_hooked("j", delete="BeforeHookCreation")])
        self.assertEqual(failures, [])

    def test_combined_policy_including_before_hook_creation_is_accepted(self):
        failures, _ = check.evaluate([_hooked("j", delete="BeforeHookCreation,HookSucceeded")])
        self.assertEqual(failures, [])

    def test_hook_succeeded_alone_is_reported(self):
        # ADR 0006 rejected this on evidence: HookSucceeded leaves a *failed* Job in place,
        # the next sync applies over it, and the original `field is immutable` error
        # returns. A gate that accepted it would bless the variant already measured broken.
        failures, _ = check.evaluate([_hooked("j", delete="HookSucceeded")])
        self.assertEqual([name for name, _ in failures], ["j"])
        self.assertIn("BeforeHookCreation", failures[0][1])

    def test_hook_failed_alone_is_reported(self):
        failures, _ = check.evaluate([_hooked("j", delete="HookFailed")])
        self.assertEqual([name for name, _ in failures], ["j"])

    def test_a_misspelled_delete_policy_is_reported(self):
        failures, _ = check.evaluate([_hooked("j", delete="beforehookcreation")])
        self.assertEqual([name for name, _ in failures], ["j"])


class TestScope(unittest.TestCase):
    def test_cronjobs_are_not_checked(self):
        # hermes-cleanup is a CronJob. Each run creates a fresh Job with a generated name, so
        # there is no immutable template to re-apply and no reason for it to be a hook. If
        # this were counted, `checked` would be non-zero on a manifest holding no Jobs at all
        # and the zero-sentinel below would stop protecting.
        docs = [{"kind": "CronJob", "metadata": {"name": "hermes-cleanup"}, "spec": {}}]
        failures, checked = check.evaluate(docs)
        self.assertEqual((failures, checked), ([], 0))

    def test_other_kinds_are_ignored(self):
        docs = [
            {"kind": "Deployment", "metadata": {"name": "d"}, "spec": {}},
            {"kind": "Service", "metadata": {"name": "s"}, "spec": {}},
        ]
        failures, checked = check.evaluate(docs)
        self.assertEqual((failures, checked), ([], 0))

    def test_reports_every_unhooked_job_not_just_the_first(self):
        failures, checked = check.evaluate([_job(f"j{i}") for i in range(4)])
        self.assertEqual(checked, 4)
        self.assertEqual(len(failures), 4)


class TestMainSentinel(unittest.TestCase):
    """A manifest with no Jobs must be an error, not a pass.

    The gate's whole purpose is that an absent control looks identical to a working one.
    "Zero Jobs checked, ok" is that failure mode wearing the gate's own uniform.
    """

    def test_no_jobs_is_an_error(self):
        self.assertEqual(check.report([], 0), 1)

    def test_a_clean_render_reports_ok(self):
        self.assertEqual(check.report([], 2), 0)

    def test_failures_report_nonzero(self):
        self.assertEqual(check.report([("j", "no hook")], 1), 1)


if __name__ == "__main__":
    unittest.main()
