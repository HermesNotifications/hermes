#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the CA-issuer admission policy gate (ADR 0005 phase 4's named residual)."""

import copy
import unittest

from check_ca_issuer_policy import evaluate

ISSUER = "hermes-internal-ca"
POLICY = "hermes-internal-ca-namespace-scope"


def ca_cluster_issuer(name=ISSUER):
    return {
        "kind": "ClusterIssuer",
        "metadata": {"name": name},
        "spec": {"ca": {"secretName": "hermes-internal-ca-tls"}},
    }


def policy(name=POLICY, issuer=ISSUER):
    return {
        "kind": "ValidatingAdmissionPolicy",
        "metadata": {"name": name},
        "spec": {
            "failurePolicy": "Fail",
            "matchConstraints": {
                "resourceRules": [
                    {
                        "apiGroups": ["cert-manager.io"],
                        "apiVersions": ["v1"],
                        "operations": ["CREATE", "UPDATE"],
                        "resources": ["certificates", "certificaterequests"],
                    }
                ]
            },
            "matchConditions": [
                {
                    "name": "targets-the-hermes-internal-ca",
                    "expression": f"object.spec.issuerRef.name == '{issuer}'",
                }
            ],
            "validations": [{"expression": "request.namespace == 'hermes'"}],
        },
    }


def binding(policy_name=POLICY, actions=("Deny",)):
    return {
        "kind": "ValidatingAdmissionPolicyBinding",
        "metadata": {"name": policy_name},
        "spec": {"policyName": policy_name, "validationActions": list(actions)},
    }


class TestCAIssuerPolicy(unittest.TestCase):
    def test_ca_issuer_with_a_denying_bound_policy_passes(self):
        failures, checked = evaluate([ca_cluster_issuer(), policy(), binding()])
        self.assertEqual(failures, [])
        self.assertEqual(checked, 1)

    def test_ca_issuer_with_no_policy_at_all_fails(self):
        """The residual ADR 0005 phase 4 names: a ClusterIssuer is usable from any namespace."""
        failures, _ = evaluate([ca_cluster_issuer()])
        self.assertEqual(len(failures), 1)
        self.assertIn("ClusterIssuer/hermes-internal-ca", failures[0])

    def test_policy_without_a_binding_fails(self):
        """An unbound ValidatingAdmissionPolicy is defined and never evaluated. It looks
        identical to a working one in a rendered diff — finding 47's failure class."""
        failures, _ = evaluate([ca_cluster_issuer(), policy()])
        self.assertEqual(len(failures), 1)
        self.assertIn("no ValidatingAdmissionPolicyBinding", failures[0])

    def test_binding_that_only_warns_fails(self):
        failures, _ = evaluate(
            [ca_cluster_issuer(), policy(), binding(actions=("Warn", "Audit"))]
        )
        self.assertEqual(len(failures), 1)
        self.assertIn("Warn/Audit", failures[0])

    def test_failure_policy_ignore_fails(self):
        """An admission control that admits what it cannot evaluate fails open."""
        open_policy = copy.deepcopy(policy())
        open_policy["spec"]["failurePolicy"] = "Ignore"
        failures, _ = evaluate([ca_cluster_issuer(), open_policy, binding()])
        self.assertEqual(len(failures), 1)
        self.assertIn("failurePolicy: Ignore", failures[0])

    def test_policy_covering_only_certificates_does_not_count(self):
        """A CertificateRequest can be created directly, with no Certificate object at all."""
        narrow = copy.deepcopy(policy())
        narrow["spec"]["matchConstraints"]["resourceRules"][0]["resources"] = ["certificates"]
        failures, _ = evaluate([ca_cluster_issuer(), narrow, binding()])
        self.assertEqual(len(failures), 1)
        self.assertIn("no ValidatingAdmissionPolicy covering", failures[0])

    def test_policy_naming_a_different_issuer_does_not_count(self):
        """A second `ca` ClusterIssuer inherits the same hole and must not be covered by the
        first one's policy."""
        failures, _ = evaluate(
            [ca_cluster_issuer("other-ca"), policy(), binding(), ca_cluster_issuer()]
        )
        self.assertEqual(len(failures), 1)
        self.assertIn("ClusterIssuer/other-ca", failures[0])

    def test_self_signed_cluster_issuer_is_not_checked(self):
        """hermes-selfsigned-bootstrap signs the CA and nothing else; it holds no key and
        grants nothing worth scoping."""
        failures, checked = evaluate(
            [
                {
                    "kind": "ClusterIssuer",
                    "metadata": {"name": "hermes-selfsigned-bootstrap"},
                    "spec": {"selfSigned": {}},
                },
                ca_cluster_issuer(),
                policy(),
                binding(),
            ]
        )
        self.assertEqual(failures, [])
        self.assertEqual(checked, 1)

    def test_no_ca_issuers_at_all_is_reported_by_main_not_here(self):
        """evaluate() returns a zero count; main() turns that into an error rather than a
        pass, the same rule check_ca_key_location.py follows."""
        failures, checked = evaluate([{"kind": "Deployment", "metadata": {"name": "x"}}])
        self.assertEqual(checked, 0)
        self.assertEqual(failures, [])


if __name__ == "__main__":
    unittest.main()
