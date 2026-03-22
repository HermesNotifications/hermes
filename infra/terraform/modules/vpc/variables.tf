variable "environment" {
  description = "Environment name"
  type        = string
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
}

variable "cluster_name" {
  description = "Name of the EKS cluster (for subnet tagging)"
  type        = string
}

variable "single_nat_gateway" {
  description = "Use a single NAT gateway (true for staging, false for production)"
  type        = bool
  default     = true
}

variable "az_count" {
  description = "Number of availability zones to use (2 for staging, 3 for production)"
  type        = number
  default     = 3
}
