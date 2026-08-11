# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

variable "environment" {
  description = "Environment name (staging or production)"
  type        = string

  validation {
    condition     = contains(["staging", "production"], var.environment)
    error_message = "Environment must be 'staging' or 'production'."
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
#   10.40.0.0/16+ Unallocated.
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

variable "github_org" {
  description = "GitHub organization or user for OIDC trust"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository name for OIDC trust"
  type        = string
  default     = "hermes"
}
