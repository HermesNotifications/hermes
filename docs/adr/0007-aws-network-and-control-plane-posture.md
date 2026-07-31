---
id: 0007
title: Fix the AWS network address plan and the EKS control-plane posture before either environment exists
status: Accepted
affects:
  - infra/terraform/**
  - infra/scripts/**
  - infra/crossplane/provider/**
source: docs/reviews/2026-07-27-architecture-review.md — findings 5, 6, 7 and 38
---

# ADR 0007: AWS network address plan and EKS control-plane posture

**Status:** Accepted (2026-07-31)
**Date:** 2026-07-31
**Author:** Daryl Robbins

---

## Context

Neither the staging nor the production AWS environment exists yet. Nothing has been
applied. That fact is the whole reason this batch was scheduled ahead of work that looks
more urgent: every decision below is one line today and an environment rebuild after the
first `terraform apply`.

Four findings from the 2026-07-27 review share that property.

**Finding 38 — one CIDR for two environments.** `var.vpc_cidr` defaulted to `10.0.0.0/16`
and neither tfvars file overrode it. Both VPCs would have been built on identical ranges
and could never have been peered. `aws_vpc.cidr_block` is `ForceNew`, and changing it after
apply takes the VPC, subnets, NAT gateways, EKS cluster, node groups and every attached
Aurora and ElastiCache with it.

**Finding 5 — the API server was open to the internet.** `endpoint_public_access = true`
with `public_access_cidrs` defaulting to `["0.0.0.0/0"]`, and the root module never
overrode it. Compounded by no control-plane audit logging and no customer-managed
encryption key for etcd secrets.

**Finding 6 — the Crossplane IAM role was account-wide.** `rds:*`, `elasticache:*`,
`secretsmanager:*` and `ssm:*` on `Resource = "*"`. `secretsmanager:*` on `*` meant an
in-cluster controller could read or overwrite every secret in the account, including the
ones the External Secrets role in the same file is carefully scoped away from.

**Finding 7 — one environment's identity applied to all of them.** The Crossplane
`DeploymentRuntimeConfig` hardcoded a staging role ARN, and `deploy/argocd/crossplane-infra.yaml`
syncs `infra/crossplane` recursively to every cluster.

## Decision

### 1. A /16 per environment, from a written register

```
10.0.0.0/16    RESERVED, never allocated
10.10.0.0/16   reserved for a future dev/sandbox environment
10.20.0.0/16   staging
10.30.0.0/16   production
10.40.0.0/16+  unallocated
```

`10.0.0.0/16` is deliberately left empty rather than given to staging. It is the default
in most Terraform module examples and in the console's VPC wizard, so a future peer, an
acquired account or a vendor VPC that took the default still does not collide with us.

The register lives beside the variable in `infra/terraform/variables.tf`, not only here,
because that is where someone adding a fourth environment will be looking.

The root `vpc_cidr` variable has **no default**. The original defect was not the value; it
was that a missing override was silent. It is now a plan-time error.

Within a /16: public subnets are `/24` (they hold only NAT and load-balancer ENIs) and
private subnets are `/20`. The previous `/24` private subnets meant roughly 250 pods per
availability zone under the VPC CNI, where every pod consumes a VPC address — a ceiling
production's node group could reach on its own, and `aws_subnet.cidr_block` is `ForceNew`
too.

### 2. Who may reach the API server is an input, not a default

`public_access_cidrs` has no default and is required. It is deliberately **not set in
either tfvars file**, so `terraform plan` refuses to run until an operator supplies it.

We are not guessing a range. Nothing in this repository determines the answer: ArgoCD,
Kargo and the Crossplane providers all run in-cluster against `kubernetes.default.svc` and
never touch the public endpoint, which leaves a human operator and possibly CI — and
GitHub-hosted runner egress is not a list worth allowlisting. A plausible-looking range
invented to keep the plan running would be the same defect wearing a number.

Two resource preconditions guard it: an empty list is rejected, and `0.0.0.0/0` is
rejected unless `allow_public_access_from_anywhere` is explicitly set.

The empty-list rule is not pedantry. In AWS provider 5.100.0 both the create and update
paths guard on a non-empty set, so `public_access_cidrs = []` sends nothing to the EKS API
and EKS then applies its own default of `0.0.0.0/0`. Setting `[]` reads as "allow nothing"
and means "allow everything", with no diff to warn you. `endpoint_public_access = false`
is the only expression of "no public access" this resource has.

### 3. Control-plane logging on, and a customer-managed key for secrets

All five `enabled_cluster_log_types` are on, with a Terraform-owned CloudWatch log group
created before the cluster (EKS auto-creates that group, and `aws_cloudwatch_log_group`
has no already-exists tolerance, so ordering is the only thing that avoids a collision).

A customer-managed KMS key encrypts Kubernetes secrets. **This corrects the review's
framing**, and the correction is recorded because it changes how the decision reads:
finding 5 says envelope encryption cannot be added to a live cluster. It can —
`AssociateEncryptionConfig` exists for exactly that, the provider models absent → present
as an in-place update, and from Kubernetes 1.28 EKS already envelope-encrypts with an
AWS-owned key by default. This is therefore a customer-managed KEK replacing an AWS-owned
one, not encryption replacing none.

It is still done now, because what *is* irreversible is the rest of it: AWS cannot disable
secrets encryption once enabled; removing the block from the configuration forces a full
cluster replacement; and changing `key_arn` afterwards is a silent no-op that reports
success and does nothing. Treat the key as permanent for the life of the cluster.

### 4. IAM scoped by prefix where a prefix exists, by type where it does not

`secretsmanager` is scoped to `secret:hermes/*` and `ssm` to `parameter/hermes/*`, the same
shape as the External Secrets role. `rds` and `elasticache` are scoped by resource type and
account only, because Crossplane derives those external names and there is no stable prefix
to match; prefix-scoping them requires a naming convention enforced in the compositions
first, which is a larger change.

The EC2 statement is left broad on purpose. Narrowing it wants a tag condition, and whether
upjet tags a security group at creation or in a follow-up call decides whether such a
condition wedges provisioning entirely. With no cluster to find out on, a policy that
deadlocks the provider would be worse than the one being replaced.

### 5. Cluster identity is generated at bootstrap, not committed

The `DeploymentRuntimeConfig` manifest is deleted. `infra/scripts/bootstrap-cluster.sh`
generates it from the role ARN the operator passes — a value the script previously
*required* and then discarded — and validates all three supplied role ARNs against the
cluster name before installing anything. Terraform names every one of these roles
`<cluster-name>-<purpose>`, so the cluster fully determines the expected name.

Generating rather than templating is deliberate: ArgoCD's directory sync picks up any
`*.yaml` in that directory, so a placeholder file would be applied over the generated one.
A `kubectl`-created object carries no ArgoCD tracking metadata and is neither overwritten
nor pruned.

## Consequences

**`terraform plan` now requires an extra input.** Supplying `eks_public_access_cidrs` is
mandatory, per environment, and there is no checked-in value. This is a real usability cost
and it is the point: the alternative is a default nobody re-examines. If CI ever runs
`plan`, it needs the variable wired in.

**The KMS key is effectively permanent.** Deleting it degrades the cluster beyond recovery
(hence a 30-day deletion window), removing the block from configuration forces a
replacement, and rotating `key_arn` silently does nothing.

**The Crossplane policy is proven well-formed, not proven sufficient.** No AWS account was
reachable during this work. The first apply plus a live claim is what will establish
whether an action is missing; expect `AccessDenied` to name any gap precisely. Likely
candidates: `iam:CreateServiceLinkedRole` in an account that has never used RDS or
ElastiCache, and `kms:DescribeKey`.

**Almost nothing here is verified against AWS.** What was executed: `terraform validate`,
`terraform fmt -check`, `terraform plan` far enough to hit the credential wall, variable
validation and precondition truth tables in `terraform console`, a harness rendering the
IAM policy from the real file text, and the bootstrap role guard end to end. What was not:
any apply, any live API call, any assertion that these settings behave as documented.

**The account ID remains in git history** and in six other checked-in files outside this
unit. It is an account ID, not a credential; parameterising it is a portability follow-up.

## Alternatives considered

**Pick a CIDR for the API allowlist and move on.** Rejected. Any value chosen here would be
invented, and an invented allowlist that nobody revisits is the defect being fixed, only
harder to notice next time.

**Set `endpoint_public_access = false` and require a bastion or VPN.** This is the stronger
posture and is where this should end up. Rejected *for now* only because this repository
provides neither, so shipping it would mean shipping an environment that cannot be
bootstrapped. The variable exists and flipping it is a one-line change once a bastion does.

**Keep the `DeploymentRuntimeConfig` in git with a placeholder ARN and exclude it in
`deploy/argocd/`.** This is the pattern `provider/environment-config.yaml` already follows,
so it would have been more consistent. Rejected because the exclusion lives in a file this
unit does not own, which would have made the fix depend on an edit that had not been made —
and, until that edit landed, ArgoCD would have applied the placeholder over the generated
object and broken IRSA in *both* environments rather than one.

**Automate the connection-secret assembly in the Crossplane composition (finding 12).**
Deferred, with the researched route recorded in
`infra/crossplane/compositions/aws/secrets.yaml`. It is achievable — and the objection that
it would render a password into a managed-resource spec turns out not to apply, because
`SecretVersion` at the pinned provider version has no literal `secretString` field. It was
not shipped because it cannot be rendered or tested here, because Crossplane is installed
unpinned and the required function returns nothing silently below 1.20, and because
Crossplane owning that secret would revert the hand-seeded values it cannot derive.
`infra/scripts/seed-connection-secret.sh` is the explicit operator step in the meantime.

## Revisit triggers

- A bastion, VPN or Session Manager path into the VPC exists → set
  `eks_endpoint_public_access = false` and delete the allowlist entirely.
- A fourth environment is proposed → add a row to the register in
  `infra/terraform/variables.tf` before allocating anything.
- The first `terraform apply` → record which IAM actions the Crossplane policy actually
  needed, and tighten or widen it against evidence rather than reasoning.
- Crossplane's Helm chart gets pinned → revisit automating the secrets composition.
