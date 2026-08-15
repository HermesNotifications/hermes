#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.
"""Assert every ${VAR} in the load-test manifests is exported by run-k8s.sh.

`envsubst` has no strict mode. A variable the script does not export is substituted with the
empty string, and the result is valid YAML that means something other than what was asked for:

  * `CONNECTIONS=""` -> the scenario falls back to its own default, so a run commissioned for
    100,000 connections measures 100 and reports success. Nothing in the output mentions it.
  * `RUNNER_CPU_REQ=""` -> `resources.requests.cpu: ""`, which the API server rejects. Loud,
    but only after the seed Job has already run.

Nine variables were referenced by testrun.yaml and set nowhere, which is how the first form of
this failure reached a real cluster. This check is the cheap half: it compares the two files as
text, needs no cluster, and runs in verify-manifests.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

# `export A B \` continued across lines, which is how run-k8s.sh writes it.
EXPORT_RE = re.compile(r"^export ([A-Z_][A-Z0-9_ \\\n]*)", re.MULTILINE)
VAR_RE = re.compile(r"\$\{([A-Z_][A-Z0-9_]*)\}")

SCRIPT = "loadtest/scripts/run-k8s.sh"
MANIFESTS = ("loadtest/k8s/testrun.yaml", "loadtest/k8s/loadseed-job.yaml")


def exported_names(script_text: str) -> set[str]:
    """Names run-k8s.sh exports, including line-continued export blocks."""
    names: set[str] = set()
    for block in EXPORT_RE.findall(script_text):
        names |= {tok for tok in block.replace("\\", "").split() if tok}
    return names


def referenced_names(manifest_texts: dict[str, str]) -> dict[str, set[str]]:
    """${VAR} references, keyed by the file they appear in."""
    return {path: set(VAR_RE.findall(text)) for path, text in manifest_texts.items()}


def find_missing(script_text: str, manifest_texts: dict[str, str]) -> dict[str, list[str]]:
    """Referenced-but-unexported names, keyed by manifest path."""
    exported = exported_names(script_text)
    missing: dict[str, list[str]] = {}
    for path, used in referenced_names(manifest_texts).items():
        gap = sorted(used - exported)
        if gap:
            missing[path] = gap
    return missing


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", default=".", help="repository root")
    args = parser.parse_args()

    root = Path(args.source_root)
    script = root / SCRIPT
    if not script.is_file():
        print(f"{SCRIPT} not found under {root}", file=sys.stderr)
        return 1

    manifests = {}
    for rel in MANIFESTS:
        path = root / rel
        if not path.is_file():
            print(f"{rel} not found under {root}", file=sys.stderr)
            return 1
        manifests[rel] = path.read_text()

    missing = find_missing(script.read_text(), manifests)
    if missing:
        for path, names in missing.items():
            for name in names:
                print(
                    f"{path}: ${{{name}}} is referenced but never exported by {SCRIPT}; "
                    "envsubst would render it empty",
                    file=sys.stderr,
                )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
