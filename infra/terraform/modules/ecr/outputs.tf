output "repository_urls" {
  description = "Map of service name to ECR repository URL"
  value       = { for k, v in aws_ecr_repository.services : k => v.repository_url }
}

output "registry_url" {
  description = "ECR registry URL (account/region prefix)"
  value       = "${data.aws_caller_identity.current.account_id}.dkr.ecr.${data.aws_region.current.name}.amazonaws.com"
}

output "repository_arns" {
  description = "Map of service name to ECR repository ARN"
  value       = { for k, v in aws_ecr_repository.services : k => v.arn }
}
