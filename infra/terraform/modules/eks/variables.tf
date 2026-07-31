variable "environment" {
  description = "Environment name"
  type        = string
}

variable "cluster_version" {
  description = "Kubernetes version for the EKS cluster"
  type        = string
}

variable "vpc_id" {
  description = "ID of the VPC"
  type        = string
}

variable "private_subnet_ids" {
  description = "IDs of the private subnets for the EKS cluster"
  type        = list(string)
}

variable "node_instance_types" {
  description = "EC2 instance types for the EKS node group"
  type        = list(string)
}

variable "node_min_size" {
  description = "Minimum number of nodes"
  type        = number
}

variable "node_max_size" {
  description = "Maximum number of nodes"
  type        = number
}

variable "node_desired_size" {
  description = "Desired number of nodes"
  type        = number
}

variable "ecr_repository_arns" {
  description = "List of ECR repository ARNs for Kargo read access"
  type        = list(string)
  default     = []
}

# Finding 5. This defaulted to ["0.0.0.0/0"] and the root module never overrode it, so
# both environments would have exposed the Kubernetes API server to the entire internet.
#
# There is deliberately no default now. Who may reach the API server is an operational
# decision that cannot be derived from this repository — ArgoCD, Kargo and the Crossplane
# providers all run *inside* the cluster and use the in-cluster service, so the only
# consumers of the public endpoint are a human operator and possibly CI, and
# GitHub-hosted runner egress is not a list anyone can sensibly allowlist. Rather than
# invent a plausible-looking range, this refuses to plan until someone decides.
#
# DO NOT "harden" this by setting it to []. Verified from the provider source at
# v5.100.0 (internal/service/eks/cluster.go, expandVpcConfigRequest and updateVPCConfig
# both guard on `v.Len() > 0`): an empty set sends nothing to the EKS API, and the API
# then defaults the endpoint to 0.0.0.0/0. Setting [] reads as "allow nothing" and means
# "allow everything", with no diff to warn you. The precondition in main.tf rejects it.
#
# To genuinely close the public endpoint, set endpoint_public_access = false. That is the
# only expression of "no public access" this resource has.
variable "public_access_cidrs" {
  description = "CIDR blocks allowed to reach the EKS public API endpoint. Required when endpoint_public_access is true. Must be network addresses (10.0.0.0/24, not 10.0.0.1/24)."
  type        = list(string)
}

variable "endpoint_public_access" {
  description = "Whether the EKS API server has a public endpoint at all. Setting this false is the only way to allow no public access; an empty public_access_cidrs means unrestricted, not blocked."
  type        = bool
  default     = true
}

variable "allow_public_access_from_anywhere" {
  description = "Escape hatch permitting 0.0.0.0/0 in public_access_cidrs. Off by default so that exposing the API server to the internet is an explicit, greppable, reviewable choice rather than an unnoticed default."
  type        = bool
  default     = false
}

variable "enabled_cluster_log_types" {
  description = "EKS control plane log types to ship to CloudWatch. Empty disables control plane logging entirely."
  type        = list(string)
  # Finding 5: the cluster set none of these, so there was no audit trail of who did what
  # to the API server — and no way to reconstruct one after the fact.
  default = ["api", "audit", "authenticator", "controllerManager", "scheduler"]

  validation {
    condition = length(setsubtract(var.enabled_cluster_log_types,
    ["api", "audit", "authenticator", "controllerManager", "scheduler"])) == 0
    error_message = "enabled_cluster_log_types accepts only: api, audit, authenticator, controllerManager, scheduler. The values are case-sensitive and controllerManager is camelCase."
  }
}

variable "cluster_log_retention_days" {
  description = "CloudWatch retention for the EKS control plane log group. Audit logs are the record you reach for after an incident, so this wants to outlive a slow discovery."
  type        = number
  default     = 90
}
