# ------------------------------------------------------------------------------
# Random passwords for secrets not derived from other modules
#
# These only change when the resource is tainted or recreated — they are stable
# across normal applies, so the secret version only updates when infrastructure
# inputs (endpoints, credentials) actually change.
# ------------------------------------------------------------------------------

resource "random_password" "jwt_secret" {
  length  = 32
  special = false
}

resource "random_password" "centrifugo_token_secret" {
  length  = 32
  special = false
}

resource "random_password" "centrifugo_api_key" {
  length  = 32
  special = false
}

# ------------------------------------------------------------------------------
# Secrets Manager — infrastructure-derived values (Terraform-managed)
# ------------------------------------------------------------------------------

resource "aws_secretsmanager_secret" "hermes" {
  name                    = "hermes/${var.environment}"
  recovery_window_in_days = var.environment == "production" ? 30 : 0

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
    centrifugo_token_secret   = random_password.centrifugo_token_secret.result
    centrifugo_api_key        = random_password.centrifugo_api_key.result
    centrifugo_redis_address  = "${var.elasticache_endpoint}:6379"
    centrifugo_redis_password = var.elasticache_auth_token
  })
}

# ------------------------------------------------------------------------------
# SSM Parameter Store — operator-managed config (not secrets)
# ------------------------------------------------------------------------------

resource "aws_ssm_parameter" "email_webhook_url" {
  name  = "/hermes/${var.environment}/email_webhook_url"
  type  = "String"
  value = "https://REPLACE_ME/email"

  lifecycle {
    ignore_changes = [value]
  }

  tags = {
    Name = "hermes-${var.environment}-email-webhook-url"
  }
}

resource "aws_ssm_parameter" "sms_webhook_url" {
  name  = "/hermes/${var.environment}/sms_webhook_url"
  type  = "String"
  value = "https://REPLACE_ME/sms"

  lifecycle {
    ignore_changes = [value]
  }

  tags = {
    Name = "hermes-${var.environment}-sms-webhook-url"
  }
}
