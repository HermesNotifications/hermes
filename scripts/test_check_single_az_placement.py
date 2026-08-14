#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the single-AZ datastore placement check.

Every test asserts the gate FAILS on a defect and PASSES without it. A gate that cannot be
shown to fail is the defect class it exists to catch.

Run: python3 -m unittest discover -s scripts -p 'test_*.py'
"""

import unittest

import check_single_az_placement as check


def _env(single_az=False, single_nat=True, region="us-east-1", path="loadtest.tfvars"):
    return {"single_az": single_az, "single_nat": single_nat, "region": region, "path": path}


def _db(az=None, instance_count=1):
    spec = {"instanceClass": "db.r7g.xlarge", "instanceCount": instance_count}
    if az is not None:
        spec["availabilityZone"] = az
    return {"kind": "HermesDatabaseClaim", "metadata": {"name": "hermes-database"}, "spec": spec}


def _cache(az=None, node_count=1):
    spec = {"nodeType": "cache.r7g.large", "nodeCount": node_count}
    if az is not None:
        spec["availabilityZone"] = az
    return {"kind": "HermesCacheClaim", "metadata": {"name": "hermes-cache"}, "spec": spec}


def _run(envs, claims):
    return check.evaluate(envs, claims)


class SingleAzPlacementTest(unittest.TestCase):
    def test_matching_pins_pass(self):
        failures = _run(
            {"loadtest": _env(single_az=True)},
            {"loadtest": [("claims/loadtest/database.yaml", _db("us-east-1a")),
                          ("claims/loadtest/cache.yaml", _cache("us-east-1a"))]},
        )
        self.assertEqual(failures, [])

    def test_claims_naming_different_zones_fail(self):
        failures = _run(
            {"loadtest": _env(single_az=True)},
            {"loadtest": [("claims/loadtest/database.yaml", _db("us-east-1a")),
                          ("claims/loadtest/cache.yaml", _cache("us-east-1b"))]},
        )
        self.assertTrue(any("different availability zones" in f for f in failures), failures)

    def test_missing_pin_fails(self):
        """The copy-staging's-claim-as-a-starting-point case."""
        failures = _run(
            {"loadtest": _env(single_az=True)},
            {"loadtest": [("claims/loadtest/database.yaml", _db("us-east-1a")),
                          ("claims/loadtest/cache.yaml", _cache(None))]},
        )
        self.assertTrue(any("do not pin availabilityZone" in f for f in failures), failures)

    def test_stray_pin_in_multi_az_environment_fails(self):
        """The dangerous direction: silently collapsing production into one zone."""
        failures = _run(
            {"production": _env(single_az=False, single_nat=False, path="production.tfvars")},
            {"production": [("claims/production/database.yaml", _db("us-east-1a"))]},
        )
        self.assertTrue(any("does not set single_az_workloads" in f for f in failures), failures)

    def test_unpinned_multi_az_environment_passes(self):
        failures = _run(
            {"production": _env(single_az=False, single_nat=False, path="production.tfvars")},
            {"production": [("claims/production/database.yaml", _db(None)),
                            ("claims/production/cache.yaml", _cache(None))]},
        )
        self.assertEqual(failures, [])

    def test_pin_outside_configured_region_fails(self):
        failures = _run(
            {"loadtest": _env(single_az=True, region="us-west-2")},
            {"loadtest": [("claims/loadtest/database.yaml", _db("us-east-1a")),
                          ("claims/loadtest/cache.yaml", _cache("us-east-1a"))]},
        )
        self.assertTrue(any("not in aws_region" in f for f in failures), failures)

    def test_replicas_pinned_into_one_zone_fail(self):
        failures = _run(
            {"loadtest": _env(single_az=True)},
            {"loadtest": [("claims/loadtest/database.yaml", _db("us-east-1a", instance_count=2)),
                          ("claims/loadtest/cache.yaml", _cache("us-east-1a"))]},
        )
        self.assertTrue(any("instanceCount=2" in f for f in failures), failures)

    def test_cache_node_count_uses_its_own_field_name(self):
        failures = _run(
            {"loadtest": _env(single_az=True)},
            {"loadtest": [("claims/loadtest/database.yaml", _db("us-east-1a")),
                          ("claims/loadtest/cache.yaml", _cache("us-east-1a", node_count=3))]},
        )
        self.assertTrue(any("nodeCount=3" in f for f in failures), failures)

    def test_single_az_without_single_nat_fails(self):
        failures = _run(
            {"loadtest": _env(single_az=True, single_nat=False)},
            {"loadtest": [("claims/loadtest/database.yaml", _db("us-east-1a")),
                          ("claims/loadtest/cache.yaml", _cache("us-east-1a"))]},
        )
        self.assertTrue(any("vpc_single_nat_gateway" in f for f in failures), failures)

    def test_single_az_environment_with_no_claims_yet_passes(self):
        """Terraform can land before the claims do; that is not a defect."""
        failures = _run({"loadtest": _env(single_az=True)}, {"loadtest": []})
        self.assertEqual(failures, [])


class TfvarsParsingTest(unittest.TestCase):
    def test_bool_parsing_ignores_comments_and_spacing(self):
        text = 'single_az_workloads    = true  # pinned, see ADR 0023\n'
        self.assertTrue(check._tfvars_bool(text, "single_az_workloads"))

    def test_bool_absent_is_false(self):
        self.assertFalse(check._tfvars_bool("environment = \"staging\"\n", "single_az_workloads"))

    def test_bool_false_is_false(self):
        self.assertFalse(check._tfvars_bool("single_az_workloads = false\n", "single_az_workloads"))

    def test_commented_out_assignment_is_not_read_as_set(self):
        """A commented line must not satisfy the check it appears to satisfy."""
        self.assertFalse(check._tfvars_bool("# single_az_workloads = true\n", "single_az_workloads"))

    def test_substring_key_does_not_match(self):
        """`vpc_single_nat_gateway` must not be read as `single_nat_gateway`."""
        text = "vpc_single_nat_gateway = true\n"
        self.assertFalse(check._tfvars_bool(text, "single_nat_gateway"))

    def test_string_parsing(self):
        self.assertEqual(check._tfvars_str('aws_region  = "us-west-2"\n', "aws_region"), "us-west-2")


class RepositoryTest(unittest.TestCase):
    """The gate against the real tree, so the committed configuration is covered too."""

    def test_repository_is_consistent(self):
        try:
            import yaml  # noqa: F401
        except ImportError:
            self.skipTest("PyYAML not installed")
        import os
        root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
        envs = check.read_environments(root)
        self.assertIn("loadtest", envs, "loadtest.tfvars missing from the environments directory")
        self.assertTrue(envs["loadtest"]["single_az"], "loadtest should be pinned to one AZ")
        claims = {e: check.read_claims(root, e, yaml) for e in envs}
        self.assertEqual(check.evaluate(envs, claims), [])


if __name__ == "__main__":
    unittest.main()
