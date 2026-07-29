#!/usr/bin/env python3
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.

"""Tests for the NetworkPolicy selector check.

Run: python3 -m unittest discover -s scripts -p 'test_*.py'

stdlib unittest deliberately — this is the only Python in the repo, and adding a test
runner dependency for one script is a worse trade than the slightly wordier assertions.
"""

import unittest

import check_networkpolicy_selectors as check


class TestSelects(unittest.TestCase):
    """LabelSelector semantics, including the empty-match case finding 47 turned on."""

    def test_empty_selector_matches_every_pod(self):
        # `podSelector: {}` is how default-deny-all covers the namespace.
        self.assertTrue(check.selects({}, {"app": "anything"}))
        self.assertTrue(check.selects({}, {}))

    def test_absent_selector_matches_nothing(self):
        self.assertFalse(check.selects(None, {"app": "x"}))

    def test_match_labels_requires_every_pair(self):
        selector = {"matchLabels": {"a": "1", "b": "2"}}
        self.assertTrue(check.selects(selector, {"a": "1", "b": "2", "c": "3"}))
        self.assertFalse(check.selects(selector, {"a": "1"}))
        self.assertFalse(check.selects(selector, {"a": "1", "b": "WRONG"}))

    def test_the_finding_47_case(self):
        # part-of never reached the pod template, so this selector matched nothing.
        selector = {"matchLabels": {"app.kubernetes.io/part-of": "hermes"}}
        without = {"app.kubernetes.io/name": "hermes-admin"}
        with_label = dict(without, **{"app.kubernetes.io/part-of": "hermes"})
        self.assertFalse(check.selects(selector, without))
        self.assertTrue(check.selects(selector, with_label))

    def test_match_expressions_operators(self):
        in_expr = {"matchExpressions": [{"key": "k", "operator": "In", "values": ["a", "b"]}]}
        self.assertTrue(check.selects(in_expr, {"k": "a"}))
        self.assertFalse(check.selects(in_expr, {"k": "z"}))
        self.assertFalse(check.selects(in_expr, {}))

        not_in = {"matchExpressions": [{"key": "k", "operator": "NotIn", "values": ["a"]}]}
        self.assertFalse(check.selects(not_in, {"k": "a"}))
        self.assertTrue(check.selects(not_in, {"k": "z"}))

        exists = {"matchExpressions": [{"key": "k", "operator": "Exists"}]}
        self.assertTrue(check.selects(exists, {"k": ""}))
        self.assertFalse(check.selects(exists, {"other": "1"}))

        missing = {"matchExpressions": [{"key": "k", "operator": "DoesNotExist"}]}
        self.assertTrue(check.selects(missing, {"other": "1"}))
        self.assertFalse(check.selects(missing, {"k": "1"}))


class TestPodTemplateLabels(unittest.TestCase):
    def test_tolerates_a_template_with_no_metadata(self):
        # Exactly what rendering produced before the fix: a bare pod template. Reading
        # this naively raised KeyError, which is why the helper exists.
        self.assertEqual(check.pod_template_labels({"template": {}}), {})
        self.assertEqual(check.pod_template_labels({}), {})
        self.assertEqual(check.pod_template_labels({"template": {"metadata": {}}}), {})

    def test_reads_labels_when_present(self):
        spec = {"template": {"metadata": {"labels": {"a": "1"}}}}
        self.assertEqual(check.pod_template_labels(spec), {"a": "1"})


class TestWorkloads(unittest.TestCase):
    def test_collects_every_pod_producing_kind(self):
        docs = [
            {"kind": "Deployment", "metadata": {"name": "d"},
             "spec": {"template": {"metadata": {"labels": {"x": "1"}}}}},
            {"kind": "StatefulSet", "metadata": {"name": "s"},
             "spec": {"template": {"metadata": {"labels": {"x": "2"}}}}},
            {"kind": "CronJob", "metadata": {"name": "c"},
             "spec": {"jobTemplate": {"spec": {"template": {"metadata": {"labels": {"x": "3"}}}}}}},
            {"kind": "Service", "metadata": {"name": "svc"}, "spec": {}},
        ]
        got = check.workloads(docs)
        self.assertEqual([n for n, _, _ in got], ["d", "s", "c"])
        self.assertEqual([l["x"] for _, _, l in got], ["1", "2", "3"])


def _policy(name, selector):
    return {"kind": "NetworkPolicy", "metadata": {"name": name}, "spec": {"podSelector": selector}}


def _deployment(name, labels):
    return {"kind": "Deployment", "metadata": {"name": name},
            "spec": {"template": {"metadata": {"labels": labels}}}}


class TestEvaluate(unittest.TestCase):
    """The gate itself: which policies are reported as selecting nothing."""

    def test_passes_when_every_policy_selects_something(self):
        docs = [
            _deployment("admin", {"app.kubernetes.io/part-of": "hermes"}),
            _policy("egress", {"matchLabels": {"app.kubernetes.io/part-of": "hermes"}}),
        ]
        failures, checked, pods = check.evaluate(docs)
        self.assertEqual(failures, [])
        self.assertEqual((checked, pods), (1, 1))

    def test_reports_the_policy_that_selects_nothing(self):
        docs = [
            _deployment("admin", {"app.kubernetes.io/name": "hermes-admin"}),
            _policy("inert", {"matchLabels": {"app.kubernetes.io/part-of": "hermes"}}),
            _policy("live", {"matchLabels": {"app.kubernetes.io/name": "hermes-admin"}}),
        ]
        failures, checked, _ = check.evaluate(docs)
        self.assertEqual(checked, 2)
        self.assertEqual([name for name, _ in failures], ["inert"])

    def test_reports_every_inert_policy_not_just_the_first(self):
        docs = [_deployment("admin", {"a": "1"})] + [
            _policy(f"p{i}", {"matchLabels": {"missing": "x"}}) for i in range(4)
        ]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(len(failures), 4)


class TestPeerSelectors(unittest.TestCase):
    """`from:`/`to:` selectors are the same defect class as podSelector.

    A peer selector that matches nothing does not make a policy permissive — it makes the
    rule unsatisfiable, so required traffic is denied. Verified in-cluster: pointing the
    `to:` selector of allow-egress-to-nats at a label no pod carries dropped
    hermes -> nats:4222 (curl exit 7) while the podSelector-only gate still reported ok.
    """

    def _policy_with_peer(self, name, direction, peer):
        return {"kind": "NetworkPolicy", "metadata": {"name": name},
                "spec": {"podSelector": {}, direction: [{"to" if direction == "egress" else "from": [peer]}]}}

    def test_reports_a_peer_selector_matching_nothing(self):
        docs = [
            _deployment("admin", {"app.kubernetes.io/name": "hermes-admin"}),
            self._policy_with_peer("egress-to-nats", "egress",
                                   {"podSelector": {"matchLabels": {"app.kubernetes.io/name": "TYPO"}}}),
        ]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual([name for name, _ in failures], ["egress-to-nats (egress peer)"])

    def test_accepts_a_peer_selector_that_matches(self):
        docs = [
            _deployment("nats", {"app.kubernetes.io/name": "nats"}),
            self._policy_with_peer("egress-to-nats", "egress",
                                   {"podSelector": {"matchLabels": {"app.kubernetes.io/name": "nats"}}}),
        ]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])

    def test_ignores_cross_namespace_and_ipblock_peers(self):
        # These name pods this manifest does not contain, so "matches nothing" here is
        # not evidence of a defect. Flagging them would make the gate cry wolf.
        docs = [
            _deployment("admin", {"app.kubernetes.io/name": "hermes-admin"}),
            self._policy_with_peer("ns-peer", "ingress",
                                   {"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "ingress-nginx"}}}),
            self._policy_with_peer("both", "ingress",
                                   {"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "observability"}},
                                    "podSelector": {"matchLabels": {"app": "prometheus"}}}),
            self._policy_with_peer("ip", "egress", {"ipBlock": {"cidr": "10.0.0.0/8"}}),
        ]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])

    def test_empty_peer_selector_matches_every_pod(self):
        docs = [
            _deployment("admin", {"app.kubernetes.io/name": "hermes-admin"}),
            self._policy_with_peer("monitor", "ingress", {"podSelector": {}}),
        ]
        failures, _, _ = check.evaluate(docs)
        self.assertEqual(failures, [])


if __name__ == "__main__":
    unittest.main()
