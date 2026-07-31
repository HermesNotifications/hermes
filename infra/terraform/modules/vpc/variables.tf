variable "environment" {
  description = "Environment name"
  type        = string
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC. Must be a /16 — the subnet layout in main.tf assumes one."
  type        = string

  # Repeated from the root module deliberately. This module is callable on its own, and
  # the subnet arithmetic silently produces the wrong sizes for any other prefix length
  # rather than failing.
  validation {
    condition     = can(cidrhost(var.vpc_cidr, 0)) && can(regex("/16$", var.vpc_cidr))
    error_message = "vpc_cidr must be a valid /16 CIDR block. See the subnet layout comment in modules/vpc/main.tf."
  }
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
  description = "Number of availability zones to use (2 for staging, 3 for production). Capped at 6 by the subnet layout."
  type        = number
  default     = 3

  # The layout in main.tf allocates private /20s at indices 1..az_count. Above 6 the
  # allocation is still arithmetically valid but stops being the layout that comment
  # describes, and an unreviewed change to the top of the range is how subnets start
  # overlapping. Raise the cap deliberately, with the comment, or not at all.
  validation {
    condition     = var.az_count >= 2 && var.az_count <= 6
    error_message = "az_count must be between 2 and 6. Two is the minimum for a highly available EKS control plane; above six, revisit the subnet layout in modules/vpc/main.tf first."
  }
}
