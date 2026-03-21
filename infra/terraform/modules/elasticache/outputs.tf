output "primary_endpoint" {
  description = "Primary endpoint address of the replication group"
  value       = aws_elasticache_replication_group.main.primary_endpoint_address
}

output "port" {
  description = "Port of the ElastiCache cluster"
  value       = aws_elasticache_replication_group.main.port
}

output "auth_token" {
  description = "Auth token for the ElastiCache cluster"
  value       = random_password.auth_token.result
  sensitive   = true
}
