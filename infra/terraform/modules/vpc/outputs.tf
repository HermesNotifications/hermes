# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

output "vpc_id" {
  description = "ID of the VPC"
  value       = aws_vpc.main.id
}

output "public_subnet_ids" {
  description = "IDs of the public subnets"
  value       = aws_subnet.public[*].id
}

output "private_subnet_ids" {
  description = "IDs of the private subnets"
  value       = aws_subnet.private[*].id
}

output "vpc_cidr_block" {
  description = "CIDR block of the VPC"
  value       = aws_vpc.main.cidr_block
}

output "availability_zones" {
  description = "Availability zones the VPC spans, in subnet index order"
  value       = local.azs
}

# The AZ of private_subnet_ids[0]. Callers that pin workloads pin them to this subnet,
# and anything outside Terraform that must land in the same AZ — the Aurora cluster
# instance and the ElastiCache node, both provisioned by Crossplane — needs the AZ name,
# not the subnet ID. Exported so that value has one source rather than being retyped
# into a claim from memory.
output "first_availability_zone" {
  description = "AZ of the first private subnet — the one workloads are pinned to when single_az_workloads is set"
  value       = local.azs[0]
}
