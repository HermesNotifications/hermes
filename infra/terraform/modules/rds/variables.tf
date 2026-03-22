variable "environment" {
  description = "Environment name"
  type        = string
}

variable "vpc_id" {
  description = "ID of the VPC"
  type        = string
}

variable "private_subnet_ids" {
  description = "IDs of the private subnets"
  type        = list(string)
}

variable "eks_security_group_id" {
  description = "Security group ID of the EKS nodes"
  type        = string
}

variable "instance_class" {
  description = "Aurora instance class"
  type        = string
}

variable "instance_count" {
  description = "Number of Aurora instances (1 for staging, 2+ for production)"
  type        = number
  default     = 1
}

variable "backup_retention_period" {
  description = "Number of days to retain backups"
  type        = number
  default     = 7
}
