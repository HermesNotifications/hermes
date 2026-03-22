variable "environment" {
  description = "Environment name"
  type        = string
}

variable "cluster_version" {
  description = "Kubernetes version for the EKS cluster"
  type        = string
}

variable "vpc_id" {
  description = "ID of the VPC"
  type        = string
}

variable "private_subnet_ids" {
  description = "IDs of the private subnets for the EKS cluster"
  type        = list(string)
}

variable "node_instance_types" {
  description = "EC2 instance types for the EKS node group"
  type        = list(string)
}

variable "node_min_size" {
  description = "Minimum number of nodes"
  type        = number
}

variable "node_max_size" {
  description = "Maximum number of nodes"
  type        = number
}

variable "node_desired_size" {
  description = "Desired number of nodes"
  type        = number
}

variable "ecr_repository_arns" {
  description = "List of ECR repository ARNs for Kargo read access"
  type        = list(string)
  default     = []
}

variable "public_access_cidrs" {
  description = "CIDR blocks allowed to access the EKS API endpoint (default: unrestricted)"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}
