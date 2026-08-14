#!/usr/bin/env bash
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.
#
# Verify that a single-AZ environment (ADR 0023) really is in one availability zone.
#
# scripts/check_single_az_placement.py checks that the configuration agrees with ITSELF, on
# every pull request, with no credentials. It cannot check the thing that actually costs
# money: whether AWS put the database, the cache and the nodes where the configuration
# asked. `local.azs` is a slice of the aws_availability_zones data source, resolving it
# needs credentials, and AWS does not guarantee that list's order is stable.
#
# This script closes that gap by asking AWS directly. It is the preflight for a load test:
# it needs exactly the credentials and kubeconfig a run already has, and it takes seconds
# against a run that takes hours.
#
# What a failure means: nothing is broken. Every query still succeeds. The environment is
# simply paying cross-AZ transfer in both directions on 100% of its datastore traffic --
# worse than never having pinned at all, and invisible until the bill arrives.
#
# Usage:
#   infra/scripts/check-single-az.sh <cluster-name> [region]
#
# Example:
#   infra/scripts/check-single-az.sh hermes-loadtest us-east-1
#
set -euo pipefail

CLUSTER="${1:?Usage: $0 <cluster-name> [region]}"
REGION="${2:-us-east-1}"
ENVIRONMENT="${CLUSTER##hermes-}"

fail=0
note() { printf '  %s\n' "$*"; }
problem() {
  printf 'MISMATCH: %s\n' "$*" >&2
  fail=1
}

for tool in aws kubectl jq; do
  command -v "$tool" >/dev/null || {
    echo "ERROR: $tool is required but not on PATH" >&2
    exit 2
  }
done

echo "==> Checking single-AZ placement for ${CLUSTER} (${REGION})"

# ---------------------------------------------------------------------------
# 1. Where the nodes are.
# ---------------------------------------------------------------------------
#
# The kubelet sets topology.kubernetes.io/zone from instance metadata, so this is where
# pods genuinely run rather than where anything intended them to run.
#
# Generator nodes are included deliberately: they scale from zero, so this list is empty
# between runs and populated during one, and a generator pool that drifted into a second AZ
# is precisely the expensive case -- generator-to-service traffic IS the load test.
# `while read` rather than `mapfile`: macOS ships bash 3.2, which has no mapfile, and this
# script is meant to be runnable by a developer before a run as well as by CI.
NODE_ZONES=()
while IFS= read -r zone; do
  [ -n "$zone" ] && NODE_ZONES+=("$zone")
done < <(
  kubectl get nodes -o json |
    jq -r '.items[].metadata.labels["topology.kubernetes.io/zone"] // empty' |
    sort -u
)

if [ "${#NODE_ZONES[@]}" -eq 0 ]; then
  echo "ERROR: no nodes reported a topology.kubernetes.io/zone label. Is the kubeconfig" >&2
  echo "       pointing at ${CLUSTER}?" >&2
  exit 2
fi

note "node zones:      ${NODE_ZONES[*]}"

if [ "${#NODE_ZONES[@]}" -gt 1 ]; then
  problem "nodes span ${#NODE_ZONES[@]} availability zones (${NODE_ZONES[*]}). Every pod-to-pod
          hop that crosses is billed in both directions. Check single_az_workloads in
          infra/terraform/environments/${ENVIRONMENT}.tfvars, and whether a node group was
          given more than one subnet."
fi

EXPECTED="${NODE_ZONES[0]}"

# ---------------------------------------------------------------------------
# 2. Where Aurora is.
# ---------------------------------------------------------------------------
#
# Crossplane derives the identifiers, so there is no stable name to look up -- the same
# reason the IAM policy in modules/eks scopes RDS by type rather than by prefix (ADR 0007).
# Filtering on the managed-by tag is what makes this findable, and it is set by the
# compositions in infra/crossplane/compositions/aws/.
#
# Instances, not the cluster: an Aurora CLUSTER reports the AZs its subnet group spans,
# which is always two here by RDS requirement and says nothing about placement. The
# INSTANCE reports where it actually sits.
# shellcheck disable=SC2016  # the backticks are JMESPath literals, not command substitution
DB_ZONES="$(
  aws rds describe-db-instances --region "$REGION" \
    --query 'DBInstances[?TagList[?Key==`managed-by` && Value==`crossplane`]].{az:AvailabilityZone,id:DBInstanceIdentifier}' \
    --output json 2>/dev/null || echo '[]'
)"

if [ "$(echo "$DB_ZONES" | jq 'length')" -eq 0 ]; then
  note "aurora:          none found (no Crossplane-managed RDS instances in ${REGION})"
else
  echo "$DB_ZONES" | jq -r '.[] | "  aurora:          \(.az)  (\(.id))"'
  while read -r az id; do
    [ -z "$az" ] && continue
    if [ "$az" != "$EXPECTED" ]; then
      problem "Aurora instance ${id} is in ${az}, nodes are in ${EXPECTED}. EVERY database
          query crosses an AZ boundary and is billed both ways. Fix availabilityZone in
          infra/crossplane/claims/${ENVIRONMENT}/database.yaml -- note that the field is
          ForceNew, so correcting it replaces the instance."
    fi
  done < <(echo "$DB_ZONES" | jq -r '.[] | "\(.az) \(.id)"')
fi

# ---------------------------------------------------------------------------
# 3. Where ElastiCache is.
# ---------------------------------------------------------------------------
#
# NodeGroupMembers carries the per-node AZ; the replication group itself has no single
# zone. With nodeCount 1 there is exactly one member.
CACHE_ZONES="$(
  aws elasticache describe-replication-groups --region "$REGION" \
    --query 'ReplicationGroups[].{id:ReplicationGroupId,members:NodeGroups[].NodeGroupMembers[].{az:PreferredAvailabilityZone,node:CacheClusterId}}' \
    --output json 2>/dev/null || echo '[]'
)"

if [ "$(echo "$CACHE_ZONES" | jq '[.[] | .members[]?] | length')" -eq 0 ]; then
  note "elasticache:     none found (no replication groups in ${REGION})"
else
  echo "$CACHE_ZONES" | jq -r '.[] | .id as $g | .members[]? | "  elasticache:     \(.az)  (\($g)/\(.node))"'
  while read -r az node; do
    [ -z "$az" ] && continue
    if [ "$az" != "$EXPECTED" ]; then
      problem "ElastiCache node ${node} is in ${az}, nodes are in ${EXPECTED}. Every cache read
          and write crosses an AZ boundary. Fix availabilityZone in
          infra/crossplane/claims/${ENVIRONMENT}/cache.yaml."
    fi
  done < <(echo "$CACHE_ZONES" | jq -r '.[] | .members[]? | "\(.az) \(.node)"')
fi

# ---------------------------------------------------------------------------
# 4. Where the NAT gateway is.
# ---------------------------------------------------------------------------
#
# Not free to get wrong: every byte a pod sends to the internet -- webhook deliveries, SES,
# image pulls that miss the ECR VPC endpoint -- crosses to the NAT's AZ and back.
NAT_SUBNETS=()
while IFS= read -r subnet; do
  [ -n "$subnet" ] && NAT_SUBNETS+=("$subnet")
done < <(
  aws ec2 describe-nat-gateways --region "$REGION" \
    --filter "Name=tag:Name,Values=hermes-${ENVIRONMENT}-nat-*" "Name=state,Values=available" \
    --query 'NatGateways[].SubnetId' --output text 2>/dev/null | tr '\t' '\n' || true
)

if [ "${#NAT_SUBNETS[@]}" -eq 0 ]; then
  note "nat gateway:     none found (tagged hermes-${ENVIRONMENT}-nat-*)"
else
  NAT_ZONES=()
  while IFS= read -r zone; do
    [ -n "$zone" ] && NAT_ZONES+=("$zone")
  done < <(
    aws ec2 describe-subnets --region "$REGION" --subnet-ids "${NAT_SUBNETS[@]}" \
      --query 'Subnets[].AvailabilityZone' --output text | tr '\t' '\n' | sort -u
  )
  note "nat zones:       ${NAT_ZONES[*]}"
  for az in "${NAT_ZONES[@]}"; do
    if [ "$az" != "$EXPECTED" ]; then
      problem "A NAT gateway is in ${az}, nodes are in ${EXPECTED}. Either egress crosses an AZ
          boundary, or this gateway is billed hourly and routed through by nothing. Check
          vpc_single_nat_gateway in infra/terraform/environments/${ENVIRONMENT}.tfvars."
    fi
  done
fi

echo ""
if [ "$fail" -ne 0 ]; then
  echo "FAILED: ${CLUSTER} is not in a single availability zone." >&2
  echo "" >&2
  echo "Nothing is broken and nothing will report an error -- this costs money, not" >&2
  echo "availability. See ADR 0023 for the design and why the pin is not enforced" >&2
  echo "automatically across the Terraform/Crossplane boundary." >&2
  exit 1
fi

echo "OK: nodes, datastores and NAT are all in ${EXPECTED}."
