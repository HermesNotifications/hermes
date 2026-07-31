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

variable "github_org" {
  description = "GitHub organization or user for OIDC trust"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository name for OIDC trust"
  type        = string
  default     = "hermes"
}
