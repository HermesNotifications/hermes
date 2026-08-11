#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the CA-key-location gate (ADR 0005 phase 4)."""

import unittest

from check_ca_key_location import evaluate

APP_NS = "hermes"


def ca_cert(namespace):
    return {
        "kind": "Certificate",
        "metadata": {"name": "hermes-internal-ca", "namespace": namespace},
        "spec": {"isCA": True, "secretName": "hermes-internal-ca-tls"},
    }


def leaf_cert(namespace, issuer_kind):
    return {
        "kind": "Certificate",
        "metadata": {"name": "nats-server-tls", "namespace": namespace},
        "spec": {
            "secretName": "nats-server-tls",
            "issuerRef": {"name": "hermes-internal-ca", "kind": issuer_kind},
        },
    }


def namespaced_ca_issuer(namespace):
    return {
        "kind": "Issuer",
        "metadata": {"name": "hermes-internal-ca", "namespace": namespace},
        "spec": {"ca": {"secretName": "hermes-internal-ca-tls"}},
    }


class TestCAKeyLocation(unittest.TestCase):
    def test_ca_certificate_outside_the_app_namespace_passes(self):
        docs = [ca_cert("cert-manager"), leaf_cert(APP_NS, "ClusterIssuer")]
        failures, checked = evaluate(docs, APP_NS)
        self.assertEqual(failures, [])
        self.assertEqual(checked, 2)

    def test_ca_certificate_in_the_app_namespace_fails(self):
        """The exact regression this gate exists for: the kustomize namespace transformer
        rewriting the CA Certificate's namespace back to the application namespace."""
        docs = [ca_cert(APP_NS), leaf_cert(APP_NS, "ClusterIssuer")]
        failures, _ = evaluate(docs, APP_NS)
        self.assertEqual(len(failures), 1)
        self.assertIn("hermes-internal-ca", failures[0])
        self.assertIn(APP_NS, failures[0])

    def test_namespaced_ca_issuer_in_the_app_namespace_fails(self):
        """A namespaced Issuer of type `ca` can only read its Secret from its own namespace,
        so its presence in the app namespace means the key is there — even if no CA
        Certificate is rendered alongside it."""
        docs = [namespaced_ca_issuer(APP_NS), leaf_cert(APP_NS, "Issuer")]
        failures, _ = evaluate(docs, APP_NS)
        self.assertEqual(len(failures), 1)
        self.assertIn("Issuer/hermes-internal-ca", failures[0])

    def test_self_signed_namespaced_issuer_is_allowed(self):
        """Only `ca` issuers hold a signing key. A selfSigned Issuer holds nothing."""
        docs = [
            {
                "kind": "Issuer",
                "metadata": {"name": "bootstrap", "namespace": APP_NS},
                "spec": {"selfSigned": {}},
            },
            leaf_cert(APP_NS, "ClusterIssuer"),
        ]
        failures, _ = evaluate(docs, APP_NS)
        self.assertEqual(failures, [])

    def test_a_ca_certificate_in_another_namespace_entirely_is_fine(self):
        docs = [ca_cert("hermes-pki"), leaf_cert(APP_NS, "ClusterIssuer")]
        failures, _ = evaluate(docs, APP_NS)
        self.assertEqual(failures, [])

    def test_no_certificates_at_all_is_an_error_not_a_pass(self):
        """A gate that checked nothing must not report success — the same rule the
        NetworkPolicy checker follows."""
        failures, checked = evaluate([{"kind": "Deployment", "metadata": {"name": "x"}}], APP_NS)
        self.assertEqual(checked, 0)
        self.assertEqual(failures, [])


if __name__ == "__main__":
    unittest.main()
