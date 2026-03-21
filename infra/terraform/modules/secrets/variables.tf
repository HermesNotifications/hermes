variable "environment" {
  description = "Environment name"
  type        = string
}

variable "rds_endpoint" {
  description = "RDS instance endpoint (hostname)"
  type        = string
}

variable "rds_username" {
  description = "RDS master username"
  type        = string
}

variable "rds_password" {
  description = "RDS master password"
  type        = string
  sensitive   = true
}

variable "elasticache_endpoint" {
  description = "ElastiCache primary endpoint"
  type        = string
}

variable "elasticache_auth_token" {
  description = "ElastiCache auth token"
  type        = string
  sensitive   = true
}
