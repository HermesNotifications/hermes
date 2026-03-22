output "secret_arn" {
  description = "ARN of the infrastructure secrets"
  value       = aws_secretsmanager_secret.hermes.arn
}
