# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

variable "environment" {
  description = "Environment name (staging, production or loadtest)"
  type        = string

  validation {
    condition     = contains(["staging", "production", "loadtest"], var.environment)
    error_message = "Environment must be 'staging', 'production' or 'loadtest'."
  }
}

variable "aws_region" {
  description = "AWS region to deploy into"
  type        = string
  default     = "us-east-1"
}

# Finding 38. This variable used to default to 10.0.0.0/16 and neither tfvars file
# overrode it, so staging and production would have been built on identical, overlapping
# ranges and could never have been peered. `aws_vpc.cidr_block` is ForceNew: today that is
# one line per environment, after deployment it is a rebuild that takes the VPC, subnets,
# NAT gateways, EKS cluster, node groups and every attached Aurora and ElastiCache with it.
#
# There is deliberately NO default now. A missing override must be a hard error at plan
# time, not a silently shared range discovered when someone tries to peer.
#
# Allocation register — a /16 per environment, from 10.0.0.0/8. Add a row before you
# invent a fourth environment; do not reuse a range.
#
#   10.0.0.0/16   RESERVED, DO NOT ALLOCATE. It is the default in most Terraform module
#                 examples, in the AWS console's VPC wizard, and in a great many
#                 third-party VPCs. Leaving it empty means a future peer or a vendor VPC
#                 that took the default still does not collide with us.
#   10.10.0.0/16  Reserved for a future dev/sandbox environment.
#   10.20.0.0/16  staging      (infra/terraform/environments/staging.tfvars)
#   10.30.0.0/16  production   (infra/terraform/environments/production.tfvars)
#   10.40.0.0/16  loadtest     (infra/terraform/environments/loadtest.tfvars)
#   10.50.0.0/16+ Unallocated.
#
# A /16 is 65,536 addresses. See modules/vpc/variables.tf for how it is carved up and why
# the private subnets are /20 rather than /24.
variable "vpc_cidr" {
  description = "CIDR block for the VPC. Must be a /16 and must not overlap any other Hermes environment — see the allocation register in this file."
  type        = string

  validation {
    condition     = can(cidrhost(var.vpc_cidr, 0)) && can(regex("/16$", var.vpc_cidr))
    error_message = "vpc_cidr must be a valid /16 CIDR block (the subnet layout in modules/vpc assumes a /16). See the allocation register in infra/terraform/variables.tf."
  }
}

# ------------------------------------------------------------------------------
# Network topology
# ------------------------------------------------------------------------------
#
# These two used to be expressions on the environment NAME in main.tf:
#
#   single_nat_gateway = var.environment == "staging"
#   az_count           = var.environment == "production" ? 3 : 2
#
# That reads fine with two environments and silently mis-sizes the third: `loadtest`
# is not "staging", so it would have inherited production's per-AZ NAT gateways —
# three of them, for an environment whose entire point is to not pay for cross-AZ
# anything. Deciding topology from a string comparison also means the only way to see
# what an environment actually builds is to evaluate a conditional in your head.
#
# No defaults, deliberately, matching the treatment of vpc_cidr above: both are
# ForceNew-adjacent (lowering az_count destroys subnets, and a subnet cannot be
# destroyed while EKS or an RDS subnet group holds an ENI in it), so a silently
# inherited default is the failure mode worth ruling out. Every tfvars file sets both.
variable "vpc_az_count" {
  description = "Number of availability zones the VPC spans. Two is the floor — EKS requires control-plane subnets in at least two AZs, including for single-AZ workload environments. See single_az_workloads."
  type        = number

  validation {
    condition     = var.vpc_az_count >= 2 && var.vpc_az_count <= 6
    error_message = "vpc_az_count must be between 2 and 6. Two is EKS's minimum for the control plane; above six, revisit the subnet layout in modules/vpc/main.tf first."
  }
}

variable "vpc_single_nat_gateway" {
  description = "Route every private subnet through one NAT gateway instead of one per AZ. Cheaper and not highly available: losing that AZ takes egress with it."
  type        = bool
}

# Cross-AZ data transfer is billed per gigabyte in BOTH directions, on traffic that
# never leaves the region and never appears on a bandwidth bill anyone is watching.
# For the loadtest environment that traffic is the entire workload — generators to
# services, services to Aurora, services to ElastiCache, NATS between pods — and a
# multi-AZ spread would mean paying for roughly half of it twice, for an environment
# whose availability requirement is nil.
#
# What this flag does and does not do:
#
#   DOES  pin the EKS node group (and the load generator pool) to the FIRST private
#         subnet, so every pod, and therefore every pod-to-pod hop, lands in one AZ.
#   DOES  require a single NAT gateway, which lives in the public subnet of that same
#         AZ, so egress is same-AZ too.
#   DOES NOT collapse the VPC to one AZ. EKS rejects a cluster whose vpc_config names
#         subnets in fewer than two AZs, and an RDS DB subnet group needs two as well.
#         The second AZ's subnets exist and stay empty; empty subnets cost nothing.
#   DOES NOT pin Aurora or ElastiCache. Those are Crossplane's, not Terraform's — the
#         claim sets `availabilityZone` and it must match the AZ this outputs as
#         `workload_availability_zone`. See infra/crossplane/claims/loadtest/.
#
# The honest cost of turning it on: the environment dies with its AZ. That is the
# correct trade for a load-test rig and the wrong one for anything serving users.
variable "single_az_workloads" {
  description = "Pin the node groups and NAT gateway to a single AZ to avoid cross-AZ data transfer charges. Trades availability for cost — the whole environment fails with that AZ. Requires vpc_single_nat_gateway."
  type        = bool
  default     = false
}

variable "eks_cluster_version" {
  description = "Kubernetes version for the EKS cluster"
  type        = string
  default     = "1.35"
}

variable "eks_node_instance_types" {
  description = "EC2 instance types for EKS node group"
  type        = list(string)
}

variable "eks_node_min_size" {
  description = "Minimum number of nodes in the EKS node group"
  type        = number
}

variable "eks_node_max_size" {
  description = "Maximum number of nodes in the EKS node group"
  type        = number
}

variable "eks_node_desired_size" {
  description = "Desired number of nodes in the EKS node group"
  type        = number
}

# Finding 5. Who may reach the Kubernetes API server.
#
# NO DEFAULT, AND DELIBERATELY NOT SET IN EITHER tfvars FILE. Terraform will refuse to
# plan until this is supplied. That is the point: the previous default was 0.0.0.0/0, and
# a value invented here to make the plan run would be the same defect wearing a number.
#
# Nothing in this repository determines the answer. ArgoCD, Kargo and the Crossplane
# providers all run inside the cluster and talk to kubernetes.default.svc, so they never
# touch the public endpoint. That leaves a human operator running
# infra/scripts/bootstrap-cluster.sh, and CI if it ever runs kubectl — and GitHub-hosted
# runner egress is a large, frequently-changing published list that is not worth
# allowlisting. Supply it deliberately, per environment:
#
#   TF_VAR_eks_public_access_cidrs='["203.0.113.10/32"]' terraform plan ...
#   terraform plan -var-file=environments/staging.tfvars -var 'eks_public_access_cidrs=["203.0.113.10/32"]'
#   or an untracked *.auto.tfvars alongside the checked-in ones
#
# If the answer is genuinely "nobody, over the public internet", set
# eks_endpoint_public_access = false and reach the API through a bastion or VPN in the
# VPC. Note that this repository provides neither, so bootstrap would need one first.
variable "eks_public_access_cidrs" {
  description = "CIDR blocks allowed to reach the EKS public API endpoint. Required unless eks_endpoint_public_access is false. Must be network addresses."
  type        = list(string)
}

variable "eks_endpoint_public_access" {
  description = "Whether the EKS API server has a public endpoint. False is the only way to allow no public access — an empty CIDR list means unrestricted, not blocked."
  type        = bool
  default     = true
}

variable "eks_allow_public_access_from_anywhere" {
  description = "Escape hatch permitting 0.0.0.0/0 in eks_public_access_cidrs. Off by default so internet exposure is an explicit, reviewable choice."
  type        = bool
  default     = false
}

variable "eks_cluster_log_retention_days" {
  description = "CloudWatch retention for EKS control plane logs, audit logs included."
  type        = number
  default     = 90
}

# The pool contract in loadtest/k8s/node-pool.md — name, taint and label — is what every
# manifest under loadtest/k8s/ already tolerates and selects on. It said "the actual pool
# is created by Terraform / Crossplane under infra/", and until now nothing did. This is
# that pool.
#
# Null means no pool, which is the right answer for staging and production: generators
# hammering the same nodes as the services under test measures the noisy neighbour, not
# the system. min_size 0 is deliberate — the pool scales to nothing between runs, so an
# idle loadtest environment carries no generator spend at all.
variable "loadtest_generator_node_pool" {
  description = "Dedicated tainted node group for k6 load generators, per loadtest/k8s/node-pool.md. Null (the default) creates no pool."
  type = object({
    instance_types = list(string)
    min_size       = number
    max_size       = number
    desired_size   = number
  })
  default = null

  validation {
    condition = var.loadtest_generator_node_pool == null || try(
      var.loadtest_generator_node_pool.min_size >= 0 &&
      var.loadtest_generator_node_pool.desired_size >= var.loadtest_generator_node_pool.min_size &&
      var.loadtest_generator_node_pool.max_size >= var.loadtest_generator_node_pool.desired_size &&
      var.loadtest_generator_node_pool.max_size > 0 &&
      length(var.loadtest_generator_node_pool.instance_types) > 0,
      false
    )
    error_message = "loadtest_generator_node_pool needs 0 <= min_size <= desired_size <= max_size, max_size > 0, and at least one instance type."
  }
}

variable "github_org" {
  description = "GitHub organization or user for OIDC trust"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository name for OIDC trust"
  type        = string
  default     = "hermes"
}
