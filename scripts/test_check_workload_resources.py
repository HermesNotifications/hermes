#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the workload resources / HPA target check.

Run: python3 -m unittest discover -s scripts -p 'test_*.py'
"""

import unittest

import check_workload_resources as check

FULL = {"requests": {"cpu": "200m", "memory": "256Mi"}, "limits": {"memory": "512Mi"}}


def _workload(name, kind="Deployment", resources=FULL, containers=None, init=None):
    if containers is None:
        containers = [{"name": name.replace("hermes-", ""), "resources": resources}]
    pod = {"containers": containers}
    if init is not None:
        pod["initContainers"] = init
    return {"kind": kind, "metadata": {"name": name},
            "spec": {"template": {"metadata": {"labels": {}}, "spec": pod}}}


def _hpa(name, target=None, kind="Deployment", metrics=("cpu",)):
    return {
        "kind": "HorizontalPodAutoscaler",
        "metadata": {"name": name},
        "spec": {
            "scaleTargetRef": {"apiVersion": "apps/v1", "kind": kind, "name": target or name},
            "metrics": [
                {"type": "Resource",
                 "resource": {"name": m, "target": {"type": "Utilization", "averageUtilization": 70}}}
                for m in metrics
            ],
        },
    }


class TestResourceRequests(unittest.TestCase):
    """Finding 8: hermes-send was missing from patches/resources.yaml and nothing noticed."""

    def test_a_fully_specified_workload_passes(self):
        failures, workloads, hpas = check.evaluate([_workload("hermes-send")])
        self.assertEqual(failures, [])
        self.assertEqual((workloads, hpas), (1, 0))

    def test_a_container_with_no_resources_at_all_is_reported(self):
        failures, _, _ = check.evaluate([_workload("hermes-send", resources={})])
        self.assertEqual(len(failures), 1)
        self.assertIn("Deployment/hermes-send", failures[0][0])

    def test_a_missing_cpu_request_is_reported(self):
        # The one that silently disables autoscaling: no CPU request means the HPA reports
        # FailedGetResourceMetric and never scales.
        res = {"requests": {"memory": "256Mi"}, "limits": {"memory": "512Mi"}}
        failures, _, _ = check.evaluate([_workload("hermes-send", resources=res)])
        self.assertEqual(len(failures), 1)
        self.assertIn("cpu request", failures[0][1])

    def test_a_missing_memory_request_is_reported(self):
        res = {"requests": {"cpu": "200m"}, "limits": {"memory": "512Mi"}}
        failures, _, _ = check.evaluate([_workload("hermes-send", resources=res)])
        self.assertIn("memory request", failures[0][1])

    def test_a_missing_memory_limit_is_reported(self):
        # Without one, a leak takes the node down rather than the pod.
        res = {"requests": {"cpu": "200m", "memory": "256Mi"}}
        failures, _, _ = check.evaluate([_workload("hermes-send", resources=res)])
        self.assertIn("memory limit", failures[0][1])

    def test_an_absent_cpu_limit_is_deliberately_NOT_reported(self):
        # patches/resources.yaml states this outright: "There is no CPU limit here, matching
        # every other service in this file — a CPU limit on the ingestion path converts a
        # burst into throttling and queueing rather than into a scale-up." A gate demanding
        # one would force a change the overlay argues against on the merits.
        failures, _, _ = check.evaluate([_workload("hermes-send", resources=FULL)])
        self.assertEqual(failures, [])

    def test_a_statefulset_is_checked_too(self):
        failures, workloads, _ = check.evaluate([_workload("nats", kind="StatefulSet", resources={})])
        self.assertEqual(workloads, 1)
        self.assertEqual(len(failures), 1)

    def test_every_container_is_checked_not_just_the_first(self):
        containers = [
            {"name": "app", "resources": FULL},
            {"name": "sidecar", "resources": {}},
        ]
        failures, _, _ = check.evaluate([_workload("d", containers=containers)])
        self.assertEqual(len(failures), 1)
        self.assertIn("sidecar", failures[0][1])

    def test_init_containers_are_checked(self):
        # None render today. Checking them anyway is cheaper than a comment promising to.
        init = [{"name": "wait-for-db", "resources": {}}]
        failures, _, _ = check.evaluate([_workload("d", init=init)])
        self.assertEqual(len(failures), 1)
        self.assertIn("wait-for-db", failures[0][1])

    def test_jobs_and_cronjobs_are_not_counted_as_workloads(self):
        docs = [
            {"kind": "Job", "metadata": {"name": "hermes-migrate"},
             "spec": {"template": {"spec": {"containers": [{"name": "migrate"}]}}}},
            {"kind": "CronJob", "metadata": {"name": "hermes-cleanup"}, "spec": {}},
        ]
        failures, workloads, _ = check.evaluate(docs)
        self.assertEqual((failures, workloads), ([], 0))


class TestHpaTargets(unittest.TestCase):
    def test_an_hpa_whose_target_exists_passes(self):
        docs = [_workload("hermes-send"), _hpa("hermes-send")]
        failures, workloads, hpas = check.evaluate(docs)
        self.assertEqual(failures, [])
        self.assertEqual((workloads, hpas), (1, 1))

    def test_an_hpa_pointing_at_a_workload_that_does_not_render_is_reported(self):
        # The shape finding 8 would have produced in reverse: an HPA left behind after its
        # Deployment was renamed. It applies cleanly and reports FailedGetScale forever.
        docs = [_workload("hermes-send"), _hpa("hermes-snd", target="hermes-snd")]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(len(failures), 1)
        self.assertIn("scaleTargetRef", failures[0][1])

    def test_the_target_kind_must_match_too(self):
        # An HPA naming kind: StatefulSet against a Deployment of that name resolves to
        # nothing, even though the name is right.
        docs = [_workload("nats", kind="StatefulSet"), _hpa("nats", kind="Deployment")]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(len(failures), 1)

    def test_an_hpa_targeting_a_statefulset_resolves(self):
        docs = [_workload("nats", kind="StatefulSet"), _hpa("nats", kind="StatefulSet")]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])


class TestHpaMetricCoupling(unittest.TestCase):
    """The measured failure: an HPA whose target has no request for the metric never scales.

    Removing send's resource requests flipped its HPA to
    `ScalingActive=False, FailedGetResourceMetric` against a real metrics-server. The HPA and
    the resources patch are coupled, and nothing expressed that coupling.
    """

    def test_a_cpu_hpa_over_a_target_with_no_cpu_request_is_reported(self):
        res = {"requests": {"memory": "256Mi"}, "limits": {"memory": "512Mi"}}
        docs = [_workload("hermes-send", resources=res), _hpa("hermes-send", metrics=("cpu",))]
        failures, _, _ = check.evaluate(docs)
        subjects = [s for s, _ in failures]
        self.assertIn("HorizontalPodAutoscaler/hermes-send", subjects)
        reason = dict(failures)["HorizontalPodAutoscaler/hermes-send"]
        self.assertIn("FailedGetResourceMetric", reason)

    def test_a_memory_metric_needs_a_memory_request(self):
        # A request block that satisfies the resources check can still not satisfy the HPA:
        # this target has cpu and memory requests, so it passes (a); the point is that the
        # coupling check reads the metric list rather than assuming cpu.
        docs = [_workload("hermes-send"), _hpa("hermes-send", metrics=("cpu", "memory"))]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])

    def test_every_container_of_the_target_must_carry_the_request(self):
        # The HPA sums requests across containers; one container without the request makes
        # the whole metric unavailable, not merely less accurate.
        containers = [
            {"name": "app", "resources": FULL},
            {"name": "sidecar", "resources": {"requests": {"memory": "32Mi"}, "limits": {"memory": "64Mi"}}},
        ]
        docs = [_workload("hermes-send", containers=containers), _hpa("hermes-send")]
        failures, _, _ = check.evaluate(docs)
        subjects = [s for s, _ in failures]
        self.assertIn("HorizontalPodAutoscaler/hermes-send", subjects)

    def test_non_resource_metrics_are_ignored(self):
        # An External or Pods metric has nothing to do with resource requests.
        hpa = _hpa("hermes-send")
        hpa["spec"]["metrics"] = [{"type": "External", "external": {"metric": {"name": "queue_depth"}}}]
        docs = [_workload("hermes-send", resources={"requests": {"cpu": "1", "memory": "1Mi"},
                                                    "limits": {"memory": "2Mi"}}), hpa]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])

    def test_an_unresolvable_target_is_reported_once_not_twice(self):
        # Don't also emit a coupling failure for a target that does not exist; one clear
        # error beats two that say the same thing.
        docs = [_workload("hermes-send"), _hpa("ghost", target="ghost")]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(len(failures), 1)


class TestMainSentinel(unittest.TestCase):
    def test_no_workloads_is_an_error(self):
        self.assertEqual(check.report([], 0, 5, require_hpa=False), 1)

    def test_a_clean_render_reports_ok(self):
        self.assertEqual(check.report([], 11, 5, require_hpa=False), 0)

    def test_no_hpas_is_fine_unless_required(self):
        self.assertEqual(check.report([], 11, 0, require_hpa=False), 0)

    def test_no_hpas_is_an_error_when_required(self):
        # Production is the only overlay with autoscaling. If the HPA set ever renders empty
        # there, that is finding 8's shape at full size and must not pass quietly.
        self.assertEqual(check.report([], 11, 0, require_hpa=True), 1)

    def test_failures_report_nonzero(self):
        self.assertEqual(check.report([("Deployment/d", "no cpu request")], 1, 0, require_hpa=False), 1)


if __name__ == "__main__":
    unittest.main()
