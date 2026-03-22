# ------------------------------------------------------------------------------
# Subnet Group
# ------------------------------------------------------------------------------

resource "aws_elasticache_subnet_group" "main" {
  name       = "hermes-${var.environment}"
  subnet_ids = var.private_subnet_ids

  tags = {
    Name = "hermes-${var.environment}"
  }
}

# ------------------------------------------------------------------------------
# Security Group
# ------------------------------------------------------------------------------

resource "aws_security_group" "elasticache" {
  name_prefix = "hermes-${var.environment}-elasticache-"
  description = "Security group for Hermes ElastiCache"
  vpc_id      = var.vpc_id

  ingress {
    description     = "Valkey/Redis from EKS nodes"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [var.eks_security_group_id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "hermes-${var.environment}-elasticache"
  }

  lifecycle {
    create_before_destroy = true
  }
}

# ------------------------------------------------------------------------------
# Auth Token
# ------------------------------------------------------------------------------

resource "random_password" "auth_token" {
  length  = 32
  special = false
}

# ------------------------------------------------------------------------------
# Replication Group
# ------------------------------------------------------------------------------

resource "aws_elasticache_replication_group" "main" {
  replication_group_id = "hermes-${var.environment}"
  description          = "Hermes ${var.environment} Valkey cluster"

  engine         = "valkey"
  engine_version = "7.2"
  node_type      = var.node_type

  num_cache_clusters         = var.num_cache_nodes
  automatic_failover_enabled = var.num_cache_nodes > 1

  subnet_group_name  = aws_elasticache_subnet_group.main.name
  security_group_ids = [aws_security_group.elasticache.id]

  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  auth_token                 = random_password.auth_token.result
  auth_token_update_strategy = "ROTATE"

  snapshot_retention_limit = var.environment == "production" ? 7 : 1

  tags = {
    Name = "hermes-${var.environment}"
  }
}
