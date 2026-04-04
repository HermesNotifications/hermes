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

output "node_security_group_id" {
  description = "EKS node security group ID"
  value       = module.eks.node_security_group_id
}
