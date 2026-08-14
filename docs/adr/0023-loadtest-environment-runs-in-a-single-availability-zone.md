---
id: 0023
title: The loadtest environment runs in a single availability zone
status: Accepted
affects:
  - infra/terraform/**
  - infra/crossplane/xrds/**
  - infra/crossplane/compositions/aws/**
  - infra/crossplane/claims/loadtest/**
  - infra/scripts/check-single-az.sh
  - deploy/k8s/overlays/loadtest/**
  - deploy/argocd/loadtest.yaml
  - scripts/check_single_az_placement.py
  - loadtest/k8s/node-pool.md
  - .github/workflows/loadtest.yml
related:
  - docs/adr/0007-aws-network-and-control-plane-posture.md
---

# ADR 0023: The loadtest environment runs in a single availability zone

**Status:** Accepted (2026-08-13)
**Date:** 2026-08-13
**Author:** Daryl Robbins

---

## Context

Load testing has so far run against staging, on the cluster named by
`secrets.STAGING_CLUSTER` in `.github/workflows/loadtest.yml`, including a nightly four-hour
soak. That has two problems, and only one of them is about money.

**Staging is a rehearsal, and a rehearsal being hammered is not a rehearsal.** Anyone
looking at staging during a soak sees the soak. It also means the load generators and the
services under test compete for the same nodes, so a run partly measures the generators.

**Cross-AZ data transfer is the dominant marginal cost of a load test, and it is invisible.**
AWS bills in-region traffic that crosses an availability-zone boundary at $0.01/GB out
*and* $0.01/GB in. Hermes is an event-driven pipeline: one send request becomes a NATS
publish, a Dispatch read of Postgres and Redis, a fan-out to delivery subjects, a worker
delivery, an event publish and an Event Writer insert. Every one of those hops is a
candidate boundary crossing. In a two-AZ environment with random placement, roughly half of
them cross. At the throughput this rig is built for — the load-testing plan targets 50k+ RPS
— that is hundreds of gigabytes per hour, billed twice, on a line item that appears under
"EC2-Other" rather than anywhere anyone associates with a load test.

The two costs pull in the same direction: give load testing its own environment, and make
that environment single-AZ.

Availability is not a requirement here. A load-test rig that dies with its AZ is an
inconvenience during business hours. That is exactly the trade staging and production must
not make, which is why this is a per-environment decision rather than a global one.

[ADR 0007](0007-aws-network-and-control-plane-posture.md) anticipated a third environment
and left a revisit trigger for it: *"A fourth environment is proposed → add a row to the
register in `infra/terraform/variables.tf` before allocating anything."* This ADR follows
that instruction.

## Decision

**We will add a `loadtest` environment on `10.40.0.0/16`, and pin everything in it that
moves bytes to one availability zone.**

### 1. The VPC still spans two AZs. The workloads do not.

EKS rejects a cluster whose `vpc_config` names subnets in fewer than two availability
zones, and an RDS DB subnet group needs two as well. "Single-AZ" therefore cannot mean a
single-AZ VPC. It means:

- `vpc_az_count = 2` — subnets exist in two AZs; the second AZ's subnets stay empty, and an
  empty subnet costs nothing.
- `single_az_workloads = true` — the EKS node groups schedule into the **first private
  subnet only**.
- `vpc_single_nat_gateway = true` — one NAT gateway, in the public subnet of that same AZ,
  so egress is same-AZ too. A precondition on `aws_nat_gateway.main` rejects
  `single_az_workloads` combined with per-AZ NAT gateways, because those extra gateways
  would be provisioned, billed hourly, and never routed through.

The pinning is expressed as a subset relationship rather than an AZ name: the root module
computes `local.workload_subnet_ids`, and a precondition on the node group requires it to
be a non-empty subset of the cluster's subnets. An AZ name that EKS has never heard of
fails at apply with a generic `InvalidParameterException`; a subset check fails at plan.

### 2. Aurora and ElastiCache are pinned by the claim, not by Terraform

Terraform does not own the datastores — Crossplane does. Placement is therefore expressed
as an optional `availabilityZone` field on the `HermesDatabase` and `HermesCache` XRDs,
**with no default**. An absent field makes the composition's patch a no-op: Aurora and
ElastiCache place instances themselves, which is what staging and production want.
`infra/crossplane/claims/loadtest/` sets it.

This leaves one value duplicated between Terraform and a claim. Nothing in either tool can
enforce the pairing, so it is enforced from outside by two checks — see §5. Terraform
exports the AZ it pinned as `workload_availability_zone` so there is a single source to
check against:

```
./infra/terraform/scripts/tfenv.sh loadtest output -raw workload_availability_zone
```

A mismatch does not fail. It puts the database in an AZ with no pods in it and every query
pays the transfer this ADR exists to avoid — a silent regression to worse-than-baseline,
since a pinned-wrong environment crosses on *100%* of queries where an unpinned one crosses
on about half. Correcting it is a rebuild rather than an edit: `availability_zone` on
`aws_rds_cluster_instance` is `Optional, Computed, Forces new resource`, and the upjet
provider is generated from that schema.

### 3. Load generators get their own tainted pool, in the same AZ

`loadtest/k8s/node-pool.md` has specified a `loadtest-generators` pool — taint
`loadtest=true:NoSchedule`, label `pool=loadtest-generators` — since the load-testing work
landed, and said the pool "is created by Terraform / Crossplane under `infra/`". Nothing
created it. `aws_eks_node_group.loadtest_generators` now does, driven by a
`loadtest_generator_node_pool` variable that defaults to `null`, so staging and production
build no pool and generators cannot land there.

It schedules into the same subnets as the main pool. Generator-to-service traffic *is* the
load test; generators in another AZ would bill every request twice while measuring a network
path production does not have.

`min_size = 0`: an idle loadtest environment runs no generator instances.

### 4. Environment topology stops being inferred from the environment name

`main.tf` decided topology by string comparison:

```hcl
single_nat_gateway = var.environment == "staging"
az_count           = var.environment == "production" ? 3 : 2
```

`loadtest` is not `"staging"`, so it would have silently inherited production's per-AZ NAT
gateways — three of them, for the environment whose entire point is not paying for cross-AZ
anything. These are now `vpc_single_nat_gateway` and `vpc_az_count`, required in every
tfvars file, with no defaults, for the same reason `vpc_cidr` has none: both are
ForceNew-adjacent, and a silently inherited default is the failure mode worth ruling out.

Staging and production keep the values they had. The refactor is a no-op for them.

### 5. The pin is checked from outside, in two layers

Neither Terraform nor Crossplane can validate the other, so the pairing is checked by
tooling that sees both. The split is deliberate: one layer runs on every pull request with
no credentials, the other runs against live AWS when it matters.

**`scripts/check_single_az_placement.py`** — in `make verify-manifests`, so every PR. Reads
the tfvars and the claims and rejects six shapes: claims naming *different* zones; a claim
that never sets `availabilityZone` in a pinned environment; a stray pin in an environment
that is *not* single-AZ (the dangerous direction — it would silently collapse production's
datastore redundancy into one zone); a pin outside the environment's `aws_region`;
`instanceCount`/`nodeCount` above 1 alongside a pin; and `single_az_workloads` without
`vpc_single_nat_gateway`. Seventeen unit tests, each asserting the gate *fails* on the
defect — a gate that cannot be shown to fail is the defect class it exists to catch.

It cannot confirm that the AZ named in the claims is the one Terraform actually pinned:
`local.azs` is a slice of the `aws_availability_zones` data source and resolving it needs
credentials. That is the residual, and it is what the second layer covers.

**`infra/scripts/check-single-az.sh`** — the load-test workflow's preflight, and runnable
by hand. Asks AWS where things really are: node zones from
`topology.kubernetes.io/zone` (so, where pods genuinely run), the Aurora *instance's*
AvailabilityZone (not the cluster's, which reports its two-AZ subnet group and says nothing
about placement), each ElastiCache `NodeGroupMember`, and the NAT gateway's subnet. Any
disagreement fails the run before it starts. Exercised against stubbed `aws`/`kubectl` for
all five drift shapes plus the aligned case.

Both are worth having. The static one catches the mistake in review, where it is free; the
live one catches what no amount of reading the config can — that AWS reordered an AZ list,
or that someone changed a claim without reapplying, or that a node group quietly acquired a
second subnet.

## Consequences

**The loadtest environment has no availability story.** Lose the AZ and the rig is gone —
cluster nodes, NAT, Aurora, ElastiCache. This is intended. It is also a reason not to copy
this environment's tfvars as the starting point for anything that serves users.

**AZ capacity can block a run.** With the generator pool confined to one AZ, a
`c7g.2xlarge` shortfall there cannot be absorbed elsewhere. The failure is at least
visible — nodes stay unschedulable rather than quietly landing across a boundary.

**One value is duplicated across two tools, and the checks are outside both.** The gates in
§5 mean a disagreement fails CI or the run rather than the bill, but neither is a
constraint the way a Terraform precondition is: someone can apply Crossplane claims without
running either. The strongest remaining option is a validating admission policy comparing
the claim to the EnvironmentConfig at apply time — recorded as a revisit trigger rather
than built here, because it needs the AZ plumbed into the EnvironmentConfig first and that
object is applied to every cluster (see Alternatives).

**The nightly soak moves clusters and needs new GitHub secrets.**
`.github/workflows/loadtest.yml` now uses a `loadtest` environment and
`secrets.LOADTEST_CLUSTER`. Until that environment and its secrets exist, the workflow fails
at `aws eks update-kubeconfig`. It fails loudly and it fails before doing anything, which is
the right failure.

**The environment deploys the whole platform, shaped after production rather than
staging.** `deploy/k8s/overlays/loadtest/` and `deploy/argocd/loadtest.yaml` complete it: a
3-peer JetStream cluster with `HERMES_NATS_STREAM_REPLICAS=3` (staging runs a single
unreplicated peer, and a throughput number from that shape would not transfer), production's
resource requests, and multiple replicas per service. It differs from production by dropping
`hpa/` and `pdb/` — fixed replicas are what make two runs comparable, since an HPA in the
loop measures how fast the autoscaler reacted — and by dropping the zone-spread constraints,
which have one topology domain here and would read as a high-availability control that
cannot do anything.

One thing only this overlay needs: `network-policies/allow-loadtest-generators.yaml`. The
generators run in the `loadtest` namespace and reach services over cluster DNS rather than
through ingress-nginx, so `default-deny` plus an ingress-only allowlist would have let the
environment deploy perfectly clean and failed every scenario at connect.

**Image tags here are not Kargo's.** There is no `loadtest` Stage in `deploy/kargo/stages/`
and the Application carries no `authorized-stage` annotation. Kargo promotes on new Freight,
which for a four-hour soak would roll every Deployment mid-run and split the result across
two builds. Tags are set deliberately in `deploy/k8s/overlays/loadtest/images/`. The cost is
that they will go stale unless someone updates them before a run — a visible cost, unlike
the alternative.

**None of this is verified against AWS.** What was run:

- `terraform fmt -check -recursive` and `terraform validate`.
- The `loadtest_generator_node_pool` validation exercised through its truth table in an
  isolated module (null, the loadtest values, `max < desired`, empty instance types,
  `max_size = 0`).
- The `render-instances` Go template extracted from `compositions/aws/database.yaml` and
  executed against both a pinned and an unpinned composite spec: the pinned case emits
  `availabilityZone` and the unpinned case omits it, and both render as parseable YAML.
- Field names checked against the AWS provider schema at v5.100.0:
  `preferred_cache_cluster_azs` (list, optional) and `aws_rds_cluster_instance
  .availability_zone` (optional, computed, ForceNew).
- `kustomize build` of the loadtest overlay, and all twelve manifest gates in
  `verify-manifests` run against the result — network policy selectors, route reachability,
  CA key location, Job hooks, workload resources, NATS password guard, CA issuer policy,
  Centrifugo origins and engine, JetStream replicas, shutdown budget. All pass, and the
  overlay is now wired into each of them so it cannot drift silently.
- 17 unit tests for `check_single_az_placement.py`, each asserting failure on a specific
  defect and no failure without it.
- `check-single-az.sh` driven against stubbed `aws` and `kubectl` for five drift shapes
  (nodes spanning two AZs, Aurora elsewhere, ElastiCache elsewhere, NAT elsewhere) plus the
  aligned case; `shellcheck` clean, and `mapfile` avoided so it runs on macOS's bash 3.2.

What was not: any apply, any plan against the loadtest backend key, and any live render of
the ElastiCache `preferredCacheClusterAzs` patch — that one is reasoned from the P&T
Optional-policy semantics and the provider schema, not observed.

**Cost is asserted, not measured.** The transfer figures above are arithmetic on AWS's
published rate and the load-testing plan's throughput target, not an observed bill. The
first full-rate run against this environment is what will establish the real number.

## Alternatives considered

**Keep running load tests on staging and accept the transfer cost.** Rejected on both
counts: it leaves staging unusable as a rehearsal environment during runs, and it keeps the
cost invisible rather than making it a decision.

**One AZ for the VPC itself, not just the workloads.** Not available. EKS requires
control-plane subnets in at least two AZs and RDS requires a two-AZ DB subnet group. The
empty second AZ is a constraint of the platform, not a hedge.

**Pin the AZ from the Crossplane EnvironmentConfig instead of the claim.** This would remove
the duplication, since `infra/scripts/bootstrap-cluster.sh` already populates that object
from Terraform outputs. Rejected because the EnvironmentConfig is applied to every cluster:
making the AZ available there means either a key present in all environments — where an
empty value would patch an empty AZ into staging and production rather than being skipped —
or environment-conditional logic in a composition that is otherwise environment-agnostic.
The claim is the place where this environment's other unusual choices already live and is
where a reviewer would look.

**Use a placement group or an AZ-affinity scheduler plugin instead of pinning subnets.**
Rejected as more machinery for the same result. Pinning the node group's subnet is one
attribute and is visible in `terraform plan`.

**Give the generator pool its own IAM role.** Rejected. It pulls images from the same ECR
repositories and needs the same CNI and worker permissions as any other node; nothing about
running k6 wants more, and a second role is a second thing to keep in step.

## Revisit triggers

- **First full-rate run** → compare the actual cross-AZ line item against the estimate above
  and record the real number.
- **A validating admission policy for claims becomes available** → enforce
  `availabilityZone` against the EnvironmentConfig at apply time, which is the one place
  the pairing can be a constraint rather than a check that someone might not run.
- **The loadtest environment needs to survive an AZ failure** — i.e. someone starts
  depending on it — → this ADR is the wrong trade for that environment; supersede it rather
  than quietly widening `single_az_workloads`.
- **A fourth environment is proposed** → add a row to the register in
  `infra/terraform/variables.tf` before allocating anything. (Inherited from ADR 0007 and
  still in force; `10.50.0.0/16` is the next free block.)
