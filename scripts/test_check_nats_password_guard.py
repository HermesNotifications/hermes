#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Tests for the Centrifugo NATS password guard gate (finding 53)."""

import copy
import unittest

from check_nats_password_guard import PASSWORD_VAR, evaluate

GUARD_SCRIPT = f'if [ -z "${{{PASSWORD_VAR}:-}}" ]; then exit 1; fi'


def guard_container(name="require-centrifugo-password"):
    return {
        "name": name,
        "image": "nats:2-alpine",
        "command": ["/bin/sh", "-euc"],
        "args": [GUARD_SCRIPT],
        "env": [
            {
                "name": PASSWORD_VAR,
                "valueFrom": {
                    "secretKeyRef": {
                        "name": "nats-nkeys",
                        "key": PASSWORD_VAR,
                        "optional": True,
                    }
                },
            }
        ],
    }


def nats_statefulset(args, init_containers=None):
    return {
        "kind": "StatefulSet",
        "metadata": {"name": "nats"},
        "spec": {
            "template": {
                "spec": {
                    "initContainers": init_containers if init_containers is not None else [],
                    "containers": [{"name": "nats", "image": "nats:2-alpine", "args": args}],
                }
            }
        },
    }


CONFIG_ARGS = ["-c", "/etc/nats-config/nats.conf", "--name", "$(POD_NAME)"]
LOCAL_ARGS = ["--jetstream", "--store_dir=/data", "-m", "8222"]


class TestNATSPasswordGuard(unittest.TestCase):
    def test_config_file_with_the_guard_passes(self):
        failures, checked = evaluate(
            [nats_statefulset(CONFIG_ARGS, [guard_container()])]
        )
        self.assertEqual(failures, [])
        self.assertEqual(checked, 1)

    def test_config_file_without_the_guard_fails(self):
        """The regression this gate exists for: the server reads nats-accounts.conf, declares
        the `centrifugo` password user, and nothing ensures the password is non-empty."""
        failures, _ = evaluate([nats_statefulset(CONFIG_ARGS)])
        self.assertEqual(len(failures), 1)
        self.assertIn("StatefulSet/nats", failures[0])
        self.assertIn(PASSWORD_VAR, failures[0])

    def test_local_overlay_without_the_guard_passes(self):
        """The local overlay drops `-c nats.conf`, so it never reads the accounts file and has
        no centrifugo user to leave unauthenticated. Its `$patch: delete` is correct, not a
        violation — the invariant is conditional on reading a config file."""
        failures, checked = evaluate([nats_statefulset(LOCAL_ARGS)])
        self.assertEqual(failures, [])
        self.assertEqual(checked, 1)

    def test_guard_renamed_out_of_the_local_patch_is_still_a_pass_locally(self):
        """A guard left in the local render is harmless — it has no Secret but also no accounts
        file to protect. This gate deliberately does not demand its absence; the local failure
        mode is loud (Init:CrashLoopBackOff) and does not need a static check."""
        failures, _ = evaluate([nats_statefulset(LOCAL_ARGS, [guard_container()])])
        self.assertEqual(failures, [])

    def test_initcontainer_that_mounts_the_variable_but_never_reads_it_does_not_count(self):
        mounted_only = copy.deepcopy(guard_container())
        mounted_only["args"] = ["echo starting"]
        failures, _ = evaluate([nats_statefulset(CONFIG_ARGS, [mounted_only])])
        self.assertEqual(len(failures), 1)

    def test_initcontainer_that_reads_the_variable_but_is_not_given_it_does_not_count(self):
        """Without the env entry the script sees an unset variable and passes vacuously."""
        script_only = copy.deepcopy(guard_container())
        script_only["env"] = []
        failures, _ = evaluate([nats_statefulset(CONFIG_ARGS, [script_only])])
        self.assertEqual(len(failures), 1)

    def test_the_guard_may_be_renamed_freely(self):
        """The gate matches on behaviour, not on the container's name — renaming it in base is
        legitimate as long as the local overlay's delete patch is renamed with it."""
        failures, _ = evaluate(
            [nats_statefulset(CONFIG_ARGS, [guard_container(name="something-else")])]
        )
        self.assertEqual(failures, [])

    def test_every_config_flag_spelling_still_requires_the_guard(self):
        """`nats-server --help` documents `-c, --config <file>`. A spelling this gate does not
        recognise does not raise a false alarm — it silently stops requiring the guard, which
        is the failure mode the gate exists to prevent."""
        for args in (
            ["-c", "/etc/nats-config/nats.conf"],
            ["--config", "/etc/nats-config/nats.conf"],
            ["-c=/etc/nats-config/nats.conf"],
            ["--config=/etc/nats-config/nats.conf"],
        ):
            with self.subTest(args=args):
                failures, _ = evaluate([nats_statefulset(args)])
                self.assertEqual(len(failures), 1, f"{args} should require the guard")

    def test_a_config_flag_in_command_rather_than_args_still_counts(self):
        doc = nats_statefulset([])
        container = doc["spec"]["template"]["spec"]["containers"][0]
        container["command"] = ["nats-server", "--config", "/etc/nats-config/nats.conf"]
        container["args"] = []
        failures, _ = evaluate([doc])
        self.assertEqual(len(failures), 1)

    def test_non_nats_statefulsets_are_ignored(self):
        other = {
            "kind": "StatefulSet",
            "metadata": {"name": "postgres"},
            "spec": {
                "template": {
                    "spec": {"containers": [{"name": "postgres", "image": "postgres:16"}]}
                }
            },
        }
        failures, checked = evaluate([other])
        self.assertEqual(failures, [])
        self.assertEqual(checked, 0)

    def test_no_nats_statefulset_at_all_is_reported_by_main_not_here(self):
        failures, checked = evaluate([{"kind": "Deployment", "metadata": {"name": "x"}}])
        self.assertEqual(checked, 0)
        self.assertEqual(failures, [])


if __name__ == "__main__":
    unittest.main()
