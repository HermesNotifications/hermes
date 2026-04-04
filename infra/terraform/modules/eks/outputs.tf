output "cluster_name" {
  description = "Name of the EKS cluster"
  value       = aws_eks_cluster.main.name
}

output "cluster_endpoint" {
  description = "Endpoint for the EKS cluster API server"
  value       = aws_eks_cluster.main.endpoint
}

output "cluster_ca_certificate" {
  description = "Base64-encoded CA certificate for the EKS cluster"
  value       = aws_eks_cluster.main.certificate_authority[0].data
}

output "node_security_group_id" {
  description = "Security group ID shared by EKS cluster and nodes"
  value       = aws_eks_cluster.main.vpc_config[0].cluster_security_group_id
}

output "cluster_security_group_id" {
  description = "EKS-managed cluster security group ID (attached to both control plane and nodes)"
  value       = aws_eks_cluster.main.vpc_config[0].cluster_security_group_id
}

output "oidc_provider_arn" {
  description = "ARN of the OIDC provider for IRSA"
  value       = aws_iam_openid_connect_provider.eks.arn
}

output "oidc_provider_url" {
  description = "URL of the OIDC provider"
  value       = aws_iam_openid_connect_provider.eks.url
}

output "external_secrets_role_arn" {
  description = "IAM role ARN for External Secrets Operator"
  value       = aws_iam_role.external_secrets.arn
}

output "kargo_controller_role_arn" {
  description = "IAM role ARN for Kargo controller (IRSA)"
  value       = aws_iam_role.kargo_controller.arn
}

output "crossplane_role_arn" {
  description = "IAM role ARN for Crossplane AWS provider (IRSA)"
  value       = aws_iam_role.crossplane.arn
}
