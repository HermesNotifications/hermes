#!/usr/bin/env python3
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"""Fail if a single-AZ environment's datastore claims disagree about which AZ that is.

ADR 0023 pins the loadtest environment's workloads to one availability zone to avoid
cross-AZ data transfer charges. The pin is expressed in two places that cannot see each
other, and that split is the residual risk the ADR records:

  * Terraform pins the EKS node groups, via `single_az_workloads` in
    infra/terraform/environments/<env>.tfvars, and reports the AZ it chose as the
    `workload_availability_zone` output.

  * Crossplane pins Aurora and ElastiCache, via `availabilityZone` on the claims in
    infra/crossplane/claims/<env>/. Terraform does not own those resources and cannot
    set the field.

A mismatch does not fail anything. The environment deploys clean, every query succeeds,
and the database simply sits in an availability zone with no pods in it -- so 100% of
database and cache traffic crosses a billed boundary, where an unpinned environment would
have crossed on roughly half. The pin makes it WORSE than doing nothing, silently, and the
only signal is a line item on next month's bill under "EC2-Other".

What this gate can and cannot see
---------------------------------
It reads source files, not AWS. It therefore CANNOT confirm that the AZ named in the
claims is the one Terraform actually pinned -- `local.azs` is a slice of the
aws_availability_zones data source, and resolving it needs credentials. That half is
covered by infra/scripts/check-single-az.sh, which compares live node, Aurora and
ElastiCache placement and is wired into the load-test workflow's preflight.

What it does catch, on every pull request, with no credentials:

  1. **Claims that disagree with each other.** database.yaml pinned to us-east-1a and
     cache.yaml to us-east-1b: half the traffic crosses no matter where the nodes are.
  2. **A missing pin.** An environment with single_az_workloads but a claim that never
     sets availabilityZone -- the default state, and the easy one to introduce by copying
     staging's claim as a starting point.
  3. **A stray pin.** availabilityZone set in an environment that is NOT single-AZ, which
     silently collapses that environment's datastore redundancy into one zone. This is the
     dangerous direction: it would reduce production's availability with no other signal.
  4. **A pin in the wrong region.** us-east-1a in an environment whose aws_region is
     us-west-2 -- AWS rejects it at create time, but hours after the plan looked fine.
  5. **Replicas pinned into one AZ.** instanceCount or nodeCount above 1 alongside a pin:
     paying for a replica that shares the writer's failure domain.
  6. **A NAT layout that wastes gateways.** single_az_workloads without
     vpc_single_nat_gateway. Terraform has a precondition for this, but a precondition
     only fires for whoever runs plan; this fires in CI.

Usage:
    check_single_az_placement.py [--source-root=.]
"""

import argparse
import glob
import os
import re
import sys

# Claim kinds that accept an availabilityZone, mapped to the field naming the number of
# instances -- a pin alongside more than one of them is spend without redundancy.
PINNABLE_KINDS = {
    "HermesDatabaseClaim": "instanceCount",
    "HermesCacheClaim": "nodeCount",
}

# `single_az_workloads = true`, tolerating any spacing. Terraform booleans are unquoted.
_TFVARS_BOOL = r"^\s*{key}\s*=\s*(true|false)\s*(?:#.*)?$"
_TFVARS_STR = r'^\s*{key}\s*=\s*"([^"]*)"\s*(?:#.*)?$'


def _tfvars_bool(text, key):
    m = re.search(_TFVARS_BOOL.format(key=re.escape(key)), text, re.MULTILINE)
    return m.group(1) == "true" if m else False


def _tfvars_str(text, key):
    m = re.search(_TFVARS_STR.format(key=re.escape(key)), text, re.MULTILINE)
    return m.group(1) if m else None


def read_environments(root):
    """Map environment name -> {single_az, single_nat, region} from the tfvars files."""
    envs = {}
    pattern = os.path.join(root, "infra", "terraform", "environments", "*.tfvars")
    for path in sorted(glob.glob(pattern)):
        name = os.path.basename(path)[: -len(".tfvars")]
        with open(path) as fh:
            text = fh.read()
        envs[name] = {
            "single_az": _tfvars_bool(text, "single_az_workloads"),
            "single_nat": _tfvars_bool(text, "vpc_single_nat_gateway"),
            "region": _tfvars_str(text, "aws_region"),
            "path": os.path.relpath(path, root),
        }
    return envs


def read_claims(root, env, yaml):
    """Every pinnable claim for an environment, as (relpath, doc) pairs."""
    out = []
    pattern = os.path.join(root, "infra", "crossplane", "claims", env, "*.yaml")
    for path in sorted(glob.glob(pattern)):
        with open(path) as fh:
            for doc in yaml.safe_load_all(fh):
                if doc and doc.get("kind") in PINNABLE_KINDS:
                    out.append((os.path.relpath(path, root), doc))
    return out


def evaluate(envs, claims_by_env):
    """Return a list of failure strings. Empty means the configuration agrees with itself."""
    failures = []

    for env, cfg in sorted(envs.items()):
        claims = claims_by_env.get(env, [])
        pins = {}
        for relpath, doc in claims:
            spec = doc.get("spec") or {}
            az = spec.get("availabilityZone")
            if az is not None:
                pins[relpath] = az

        if cfg["single_az"]:
            # 6. NAT layout. Mirrors the Terraform precondition, without needing a plan.
            if not cfg["single_nat"]:
                failures.append(
                    f"{env}: single_az_workloads is true but vpc_single_nat_gateway is not "
                    f"({cfg['path']}). Workloads run in one AZ, so the NAT gateways in every "
                    f"other AZ are billed hourly and never routed through."
                )

            if not claims:
                # Nothing to check, and nothing to be wrong about yet.
                continue

            # 2. A missing pin.
            unpinned = [p for p, _ in claims if p not in pins]
            if unpinned:
                failures.append(
                    f"{env}: single_az_workloads is set, but these claims do not pin "
                    f"availabilityZone: {', '.join(sorted(unpinned))}. AWS will place them, and "
                    f"an unpinned datastore in the AZ the nodes are NOT in pays cross-AZ transfer "
                    f"on every query -- the cost single_az_workloads exists to remove."
                )

            # 1. Claims disagreeing with each other.
            if len(set(pins.values())) > 1:
                detail = ", ".join(f"{p} -> {az}" for p, az in sorted(pins.items()))
                failures.append(
                    f"{env}: datastore claims name different availability zones ({detail}). "
                    f"Whichever AZ the nodes are in, traffic to the other datastore crosses a "
                    f"billed boundary."
                )

            # 4. Region agreement.
            region = cfg["region"]
            if region:
                for relpath, az in sorted(pins.items()):
                    if not az.startswith(region):
                        failures.append(
                            f"{env}: {relpath} pins availabilityZone {az!r}, which is not in "
                            f"aws_region {region!r} ({cfg['path']}). AWS rejects this at create "
                            f"time, long after the change looked fine in review."
                        )

            # 5. Replicas sharing the writer's failure domain.
            for relpath, doc in claims:
                if relpath not in pins:
                    continue
                field = PINNABLE_KINDS[doc["kind"]]
                count = (doc.get("spec") or {}).get(field, 1)
                if isinstance(count, int) and count > 1:
                    failures.append(
                        f"{env}: {relpath} pins availabilityZone and sets {field}={count}. "
                        f"Replicas in the writer's own AZ are spend without redundancy -- either "
                        f"drop to 1 or stop pinning."
                    )
        else:
            # 3. A stray pin in a multi-AZ environment. The dangerous direction.
            for relpath, az in sorted(pins.items()):
                failures.append(
                    f"{env}: {relpath} pins availabilityZone {az!r}, but {cfg['path']} does not "
                    f"set single_az_workloads. This collapses the datastore into a single "
                    f"availability zone while the rest of the environment is spread across "
                    f"several -- reducing availability with nothing else to signal it."
                )

    return failures


def main(argv):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--source-root", default=".", help="Repository root (default: .)")
    args = parser.parse_args(argv)

    try:
        import yaml
    except ImportError:
        print("SKIP: PyYAML not installed; single-AZ placement check not run", file=sys.stderr)
        return 0

    root = args.source_root
    envs = read_environments(root)
    if not envs:
        print(
            f"ERROR: no tfvars files under {root}/infra/terraform/environments/. Passing "
            f"vacuously would be worse than failing -- check --source-root.",
            file=sys.stderr,
        )
        return 1

    claims_by_env = {env: read_claims(root, env, yaml) for env in envs}
    failures = evaluate(envs, claims_by_env)

    if failures:
        print("FAIL: single-AZ placement is inconsistent\n", file=sys.stderr)
        for f in failures:
            print(f"  - {f}\n", file=sys.stderr)
        print(
            "The AZ Terraform pinned is its `workload_availability_zone` output:\n"
            "  ./infra/terraform/scripts/tfenv.sh <env> output -raw workload_availability_zone\n"
            "See ADR 0023 for why this pairing is not enforced automatically, and\n"
            "infra/scripts/check-single-az.sh for the live check against AWS.",
            file=sys.stderr,
        )
        return 1

    pinned = sorted(e for e, c in envs.items() if c["single_az"])
    summary = ", ".join(pinned) if pinned else "none"
    total = sum(len(c) for c in claims_by_env.values())
    print(f"ok: {len(envs)} environment(s), {total} claim(s); single-AZ: {summary}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
