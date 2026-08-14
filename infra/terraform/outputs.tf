# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

output "ecr_registry_url" {
  description = "ECR registry URL (account/region prefix)"
  value       = module.ecr.registry_url
}

output "ecr_repository_urls" {
  description = "Map of service name to ECR repository URL"
  value       = module.ecr.repository_urls
}

output "eks_cluster_name" {
  description = "Name of the EKS cluster"
  value       = module.eks.cluster_name
}

output "external_secrets_role_arn" {
  description = "IAM role ARN for External Secrets Operator (IRSA)"
  value       = module.eks.external_secrets_role_arn
}

output "github_actions_role_arn" {
  description = "IAM role ARN for GitHub Actions OIDC"
  value       = module.cicd.github_actions_role_arn
}

output "kargo_controller_role_arn" {
  description = "IAM role ARN for Kargo controller (IRSA)"
  value       = module.eks.kargo_controller_role_arn
}

output "crossplane_role_arn" {
  description = "IAM role ARN for Crossplane AWS provider (IRSA)"
  value       = module.eks.crossplane_role_arn
}

output "vpc_id" {
  description = "VPC ID"
  value       = module.vpc.vpc_id
}

output "private_subnet_ids" {
  description = "Private subnet IDs"
  value       = module.vpc.private_subnet_ids
}

output "workload_subnet_ids" {
  description = "Private subnets the node groups schedule into. A single entry when single_az_workloads is set, otherwise all of them."
  value       = local.workload_subnet_ids
}

# Empty string, not null, for multi-AZ environments: the bootstrap script interpolates
# this straight into the EnvironmentConfig, and `terraform output -raw` on a null prints
# the word "null".
#
# When it is non-empty, it is the AZ that infra/crossplane/claims/loadtest/*.yaml must
# name in `availabilityZone`. Terraform cannot set that itself — Aurora and ElastiCache
# are Crossplane's — so this output is how the two stay in agreement. Check it with:
#
#   ./scripts/tfenv.sh loadtest output -raw workload_availability_zone
output "workload_availability_zone" {
  description = "AZ that workloads are pinned to, or \"\" when they are spread across all AZs. Crossplane claims for a single-AZ environment must match this."
  value       = var.single_az_workloads ? module.vpc.first_availability_zone : ""
}

output "availability_zones" {
  description = "Availability zones the VPC spans, in subnet index order"
  value       = module.vpc.availability_zones
}

output "loadtest_generator_node_group_name" {
  description = "Name of the k6 load generator node group, or null when the environment has no pool"
  value       = module.eks.loadtest_generator_node_group_name
}

output "node_security_group_id" {
  description = "EKS node security group ID"
  value       = module.eks.node_security_group_id
}
