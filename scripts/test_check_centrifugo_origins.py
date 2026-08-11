#!/usr/bin/env python3
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.

"""Tests for the Centrifugo allowed_origins check.

Run: python3 -m unittest discover -s scripts -p 'test_*.py'
"""

import json
import unittest

import check_centrifugo_origins as check


def _deployment(name="centrifugo", env=None, image="centrifugo/centrifugo:v5"):
    container = {"name": "centrifugo", "image": image}
    if env is not None:
        container["env"] = env
    return {
        "kind": "Deployment",
        "metadata": {"name": name},
        "spec": {"template": {"spec": {"containers": [container]}}},
    }


def _env(value):
    return [{"name": check.ENV_VAR, "value": value}]


def _configmap(payload, name="centrifugo-config-abc123"):
    return {
        "kind": "ConfigMap",
        "metadata": {"name": name},
        "data": {"config.json": json.dumps(payload)},
    }


class TestTheActualBug(unittest.TestCase):
    """No allowed_origins anywhere: healthy, Ready, and refusing every browser."""

    def test_no_env_and_no_config_fails(self):
        failures, checked = check.evaluate([_deployment()])
        self.assertEqual(checked, 1)
        self.assertEqual(len(failures), 1)
        self.assertIn("403", failures[0][1])

    def test_empty_env_fails(self):
        failures, _ = check.evaluate([_deployment(env=_env(""))])
        self.assertEqual(len(failures), 1)
        self.assertIn("same-origin only", failures[0][1])

    def test_empty_config_list_fails(self):
        docs = [_deployment(), _configmap({"allowed_origins": []})]
        failures, _ = check.evaluate(docs)
        self.assertEqual(len(failures), 1)


class TestAcceptedShapes(unittest.TestCase):
    def test_a_real_origin_in_the_env_var_passes(self):
        failures, _ = check.evaluate([_deployment(env=_env("https://app.example.com"))])
        self.assertEqual(failures, [])

    def test_several_space_separated_origins_pass(self):
        failures, _ = check.evaluate([
            _deployment(env=_env("https://a.example.com https://*.b.example.com"))
        ])
        self.assertEqual(failures, [])

    def test_origins_from_the_config_file_pass(self):
        docs = [_deployment(), _configmap({"allowed_origins": ["http://localhost:5173"]})]
        failures, _ = check.evaluate(docs)
        self.assertEqual(failures, [])

    def test_v6_nested_client_block_passes(self):
        # Kept working deliberately: the Helm sub-chart runs v6, where the key moved under
        # `client`. A gate that only understood v5 would pass by finding nothing.
        docs = [_deployment(), _configmap({"client": {"allowed_origins": ["https://x.example.com"]}})]
        failures, _ = check.evaluate(docs)
        self.assertEqual(failures, [])


class TestPlaceholder(unittest.TestCase):
    """Committed placeholders must pass verify, and must fail a deploy pipeline."""

    def test_placeholder_passes_by_default(self):
        failures, _ = check.evaluate([_deployment(env=_env(check.PLACEHOLDER))])
        self.assertEqual(failures, [])

    def test_placeholder_fails_when_forbidden(self):
        failures, _ = check.evaluate(
            [_deployment(env=_env(check.PLACEHOLDER))], forbid_placeholder=True
        )
        self.assertEqual(len(failures), 1)
        self.assertIn("substitute the real origins", failures[0][1])

    def test_a_real_value_still_passes_when_forbidden(self):
        failures, _ = check.evaluate(
            [_deployment(env=_env("https://app.example.com"))], forbid_placeholder=True
        )
        self.assertEqual(failures, [])


class TestPrecedence(unittest.TestCase):
    def test_env_var_wins_over_the_config_file(self):
        # Centrifugo's env vars override the config file, so an empty env var must fail even
        # when the mounted config looks fine — otherwise the gate reports on a value that is
        # not the one in force.
        docs = [_deployment(env=_env("")), _configmap({"allowed_origins": ["https://ok.example.com"]})]
        failures, _ = check.evaluate(docs)
        self.assertEqual(len(failures), 1)

    def test_valuefrom_is_treated_as_absent(self):
        # The gate cannot see what a secretKeyRef resolves to. Claiming it satisfies the check
        # would be exactly the false assurance this family of gates exists to prevent.
        env = [{"name": check.ENV_VAR, "valueFrom": {"secretKeyRef": {"name": "s", "key": "k"}}}]
        failures, _ = check.evaluate([_deployment(env=env)])
        self.assertEqual(len(failures), 1)


class TestSentinels(unittest.TestCase):
    def test_no_centrifugo_workload_is_an_error_not_a_pass(self):
        self.assertEqual(check.report([], 0), 1)

    def test_a_non_centrifugo_deployment_is_not_checked(self):
        other = _deployment(name="hermes-admin", image="hermes/admin:1", env=None)
        failures, checked = check.evaluate([other])
        self.assertEqual(checked, 0)
        self.assertEqual(failures, [])

    def test_matching_is_by_image_so_a_rename_stays_covered(self):
        failures, checked = check.evaluate([_deployment(name="realtime")])
        self.assertEqual(checked, 1)
        self.assertEqual(len(failures), 1)

    def test_clean_run_reports_ok(self):
        self.assertEqual(check.report([], 2), 0)


if __name__ == "__main__":
    unittest.main()
