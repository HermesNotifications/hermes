#!/usr/bin/env python3
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the PodDisruptionBudget selector check.

Run: python3 -m unittest discover -s scripts -p 'test_*.py'
"""

import unittest

import check_pdb_selectors as check


def _pdb(name, selector, min_available=None, max_unavailable=None):
    spec = {"selector": selector}
    if min_available is not None:
        spec["minAvailable"] = min_available
    if max_unavailable is not None:
        spec["maxUnavailable"] = max_unavailable
    return {"kind": "PodDisruptionBudget", "metadata": {"name": name}, "spec": spec}


def _deployment(name, labels, replicas=2):
    return {"kind": "Deployment", "metadata": {"name": name},
            "spec": {"replicas": replicas, "template": {"metadata": {"labels": labels}}}}


def _statefulset(name, labels, replicas=3):
    return {"kind": "StatefulSet", "metadata": {"name": name},
            "spec": {"replicas": replicas, "template": {"metadata": {"labels": labels}}}}


def _job(name, labels):
    return {"kind": "Job", "metadata": {"name": name},
            "spec": {"template": {"metadata": {"labels": labels}}}}


SEND = {"app.kubernetes.io/name": "hermes-send"}


class TestSelectorMatching(unittest.TestCase):
    """The `hermes-sned` defect: a typo drops expectedPods to 0 and nothing complains."""

    def test_a_matching_selector_passes(self):
        docs = [_deployment("hermes-send", SEND, replicas=3),
                _pdb("hermes-send", {"matchLabels": SEND}, max_unavailable=1)]
        failures, checked, pods = check.evaluate(docs)
        self.assertEqual(failures, [])
        self.assertEqual((checked, pods), (1, 1))

    def test_the_hermes_sned_typo_is_reported(self):
        # Verified in-cluster by the unit that filed this: a one-character typo in the send
        # PDB's selector took `expectedPods` from 3 to 0 with no error from kustomize or
        # kubectl. The budget then protects nothing, silently and indefinitely.
        docs = [_deployment("hermes-send", SEND, replicas=3),
                _pdb("hermes-send", {"matchLabels": {"app.kubernetes.io/name": "hermes-sned"}},
                     max_unavailable=1)]
        failures, checked, _ = check.evaluate(docs)
        self.assertEqual(checked, 1)
        self.assertEqual([name for name, _ in failures], ["hermes-send"])
        self.assertIn("matches none", failures[0][1])

    def test_a_null_selector_matches_nothing(self):
        docs = [_deployment("hermes-send", SEND), _pdb("p", None, max_unavailable=1)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual([name for name, _ in failures], ["p"])

    def test_an_empty_selector_matches_every_pod(self):
        # policy/v1 semantics: `selector: {}` covers the whole namespace. Unusual but valid.
        docs = [_deployment("hermes-send", SEND), _pdb("p", {}, max_unavailable=1)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])

    def test_reports_every_inert_pdb_not_just_the_first(self):
        docs = [_deployment("hermes-send", SEND)] + [
            _pdb(f"p{i}", {"matchLabels": {"missing": "x"}}, max_unavailable=1) for i in range(4)
        ]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(len(failures), 4)

    def test_a_statefulset_counts_as_a_workload(self):
        nats = {"app.kubernetes.io/name": "nats"}
        docs = [_statefulset("nats", nats, replicas=3),
                _pdb("nats", {"matchLabels": nats}, min_available=2)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])


class TestJobPods(unittest.TestCase):
    """`pdb/README.md`: a PDB over a Job's pods can wedge a drain forever.

    A Job pod that is evicted is not replaced, so the budget can never be satisfied again.
    The README names hermes-migrate, hermes-natsprovision and the cleanup CronJob as
    deliberately uncovered; this makes "deliberately" enforceable.
    """

    def test_a_pdb_matching_only_job_pods_is_reported(self):
        prov = {"app.kubernetes.io/name": "hermes-natsprovision"}
        docs = [_deployment("hermes-send", SEND), _job("hermes-natsprovision", prov),
                _pdb("prov", {"matchLabels": prov}, max_unavailable=1)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual([name for name, _ in failures], ["prov"])
        self.assertIn("Job", failures[0][1])

    def test_a_pdb_matching_a_deployment_as_well_is_not_reported(self):
        # An overly broad selector that happens to sweep in a Job pod alongside real
        # replicas is a different (and much milder) thing; the drain still proceeds.
        shared = {"app.kubernetes.io/part-of": "hermes"}
        docs = [_deployment("hermes-send", dict(SEND, **shared)),
                _job("hermes-migrate", dict(shared, **{"app.kubernetes.io/name": "m"})),
                _pdb("broad", {"matchLabels": shared}, max_unavailable=1)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])


class TestPermanentlyBlockedBudgets(unittest.TestCase):
    """`minAvailable` >= the replica count pins `disruptionsAllowed: 0` forever.

    `pdb/README.md` names this as the classic footgun and says it does not arise today "but
    it would the moment someone adds a workload at `replicas: 1`". Nothing enforced that.
    """

    def test_min_available_equal_to_replicas_is_reported(self):
        docs = [_deployment("solo", {"a": "1"}, replicas=1),
                _pdb("solo", {"matchLabels": {"a": "1"}}, min_available=1)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual([name for name, _ in failures], ["solo"])
        self.assertIn("disruptionsAllowed: 0", failures[0][1])

    def test_min_available_above_replicas_is_reported(self):
        docs = [_deployment("two", {"a": "1"}, replicas=2),
                _pdb("two", {"matchLabels": {"a": "1"}}, min_available=3)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(len(failures), 1)

    def test_the_nats_quorum_budget_is_accepted(self):
        # minAvailable 2 of 3 leaves exactly one disruption. This is the real production
        # shape and must not be flagged.
        nats = {"app.kubernetes.io/name": "nats"}
        docs = [_statefulset("nats", nats, replicas=3),
                _pdb("nats", {"matchLabels": nats}, min_available=2)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])

    def test_max_unavailable_at_one_replica_is_not_reported(self):
        # maxUnavailable: 1 always permits one eviction; it cannot wedge a drain.
        docs = [_deployment("solo", {"a": "1"}, replicas=1),
                _pdb("solo", {"matchLabels": {"a": "1"}}, max_unavailable=1)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])

    def test_min_available_as_a_full_percentage_is_reported(self):
        docs = [_deployment("d", {"a": "1"}, replicas=3),
                _pdb("p", {"matchLabels": {"a": "1"}}, min_available="100%")]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(len(failures), 1)

    def test_a_partial_percentage_is_accepted(self):
        docs = [_deployment("d", {"a": "1"}, replicas=3),
                _pdb("p", {"matchLabels": {"a": "1"}}, min_available="50%")]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])

    def test_replicas_are_summed_across_matched_workloads(self):
        # A selector deliberately covering two Deployments has their combined replicas
        # available, so minAvailable 2 is satisfiable at 1+1.
        docs = [_deployment("a", {"g": "x"}, replicas=1), _deployment("b", {"g": "x"}, replicas=1),
                _pdb("p", {"matchLabels": {"g": "x"}}, min_available=2)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual([name for name, _ in failures], ["p"])

        docs[2] = _pdb("p", {"matchLabels": {"g": "x"}}, min_available=1)
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])

    def test_an_absent_replicas_field_defaults_to_one(self):
        # Kubernetes defaults spec.replicas to 1 when it is omitted, so a PDB with
        # minAvailable: 1 over it is wedged just as surely as the explicit case.
        docs = [{"kind": "Deployment", "metadata": {"name": "d"},
                 "spec": {"template": {"metadata": {"labels": {"a": "1"}}}}},
                _pdb("p", {"matchLabels": {"a": "1"}}, min_available=1)]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(len(failures), 1)


class TestMainSentinel(unittest.TestCase):
    def test_no_workloads_is_an_error(self):
        self.assertEqual(check.report([], 3, 0), 1)

    def test_no_pdbs_is_an_error(self):
        self.assertEqual(check.report([], 0, 5), 1)

    def test_a_clean_render_reports_ok(self):
        self.assertEqual(check.report([], 11, 11), 0)

    def test_failures_report_nonzero(self):
        self.assertEqual(check.report([("p", "matches none of the 11 workloads")], 1, 11), 1)


class TestSelectorSemanticsAreShared(unittest.TestCase):
    """This gate must not grow a second copy of Kubernetes label-selector semantics.

    Two implementations drift, and the drift is invisible: each gate keeps passing its own
    tests while disagreeing about the same manifest.
    """

    def test_it_reuses_the_networkpolicy_gate_primitives(self):
        import check_networkpolicy_selectors as netpol
        self.assertIs(check.selects, netpol.selects)
        self.assertIs(check.pod_template_labels, netpol.pod_template_labels)


if __name__ == "__main__":
    unittest.main()
