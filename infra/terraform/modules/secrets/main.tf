# ------------------------------------------------------------------------------
# Random passwords for secrets not derived from other modules
# ------------------------------------------------------------------------------

resource "random_password" "jwt_secret" {
  length  = 32
  special = false
}

resource "random_password" "centrifugo_api_key" {
  length  = 32
  special = false
}

# ------------------------------------------------------------------------------
# Secrets Manager
# ------------------------------------------------------------------------------

resource "aws_secretsmanager_secret" "hermes" {
  name = "hermes/${var.environment}"

  tags = {
    Name = "hermes-${var.environment}"
  }
}

resource "aws_secretsmanager_secret_version" "hermes" {
  secret_id = aws_secretsmanager_secret.hermes.id

  secret_string = jsonencode({
    database_url              = "postgres://${var.rds_username}:${var.rds_password}@${var.rds_endpoint}:5432/hermes?sslmode=require"
    redis_url                 = "rediss://default:${var.elasticache_auth_token}@${var.elasticache_endpoint}:6379"
    jwt_secret                = random_password.jwt_secret.result
    centrifugo_token_secret   = random_password.jwt_secret.result
    centrifugo_api_key        = random_password.centrifugo_api_key.result
    centrifugo_redis_address  = "${var.elasticache_endpoint}:6379"
    centrifugo_redis_password = var.elasticache_auth_token
    email_webhook_url         = "https://REPLACE_ME/email"
    sms_webhook_url           = "https://REPLACE_ME/sms"
  })

  lifecycle {
    ignore_changes = [secret_string]
  }
}
