# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  azs               = slice(data.aws_availability_zones.available.names, 0, var.az_count)
  nat_gateway_count = var.single_nat_gateway ? 1 : length(local.azs)
}

# ------------------------------------------------------------------------------
# Subnet layout
# ------------------------------------------------------------------------------
#
# Adjacent to finding 38 rather than part of it. The private subnets were
# cidrsubnet(vpc_cidr, 8, i + 10) — a /24, so 251 usable addresses per AZ. With the
# VPC CNI every *pod* consumes a VPC address, not just every node, so a /24 per AZ is a
# hard ceiling of roughly 250 pods in that AZ no matter how many nodes are added. Hitting
# it does not degrade gracefully: pods stop scheduling with InsufficientFreeAddresses and
# the only fix is new subnets, which means a new EKS cluster.
#
# aws_subnet.cidr_block is ForceNew, exactly like the VPC CIDR, and neither environment
# exists yet. This is the last moment it is free.
#
# Layout, for a /16 (enforced by the validation on var.vpc_cidr):
#
#   public[i]   cidrsubnet(cidr, 8, i)      /24    x.y.0.0/24  .. x.y.5.0/24
#   private[i]  cidrsubnet(cidr, 4, i + 1)  /20    x.y.16.0/20 .. x.y.96.0/20
#
# Public subnets stay /24 because they hold only NAT gateway and load balancer ENIs —
# a handful of addresses each. They all live inside x.y.0.0/20, which is private index 0
# and is deliberately never allocated, so public and private cannot collide for any
# az_count the module accepts (var.az_count is capped at 6; the public /24s would in fact
# stay inside that first /20 up to 16 AZs).
#
# Everything from x.y.112.0/20 upwards is unallocated and available for a later tier
# (isolated database subnets, a transit attachment) without disturbing what exists.
#
# Verified with `terraform console` against both allocated ranges for az_count 1..6:
# the two sets do not intersect. Not verified against AWS — nothing has been applied.

# ------------------------------------------------------------------------------
# VPC
# ------------------------------------------------------------------------------

resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name = "hermes-${var.environment}"
  }
}

# ------------------------------------------------------------------------------
# Subnets
# ------------------------------------------------------------------------------

resource "aws_subnet" "public" {
  count = length(local.azs)

  vpc_id                  = aws_vpc.main.id
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, count.index) # /24 — see "Subnet layout" above
  availability_zone       = local.azs[count.index]
  map_public_ip_on_launch = true

  tags = {
    Name                                        = "hermes-${var.environment}-public-${local.azs[count.index]}"
    "kubernetes.io/role/elb"                    = "1"
    "kubernetes.io/cluster/${var.cluster_name}" = "shared"
  }
}

resource "aws_subnet" "private" {
  count = length(local.azs)

  vpc_id            = aws_vpc.main.id
  cidr_block        = cidrsubnet(var.vpc_cidr, 4, count.index + 1) # /20 — see "Subnet layout" above
  availability_zone = local.azs[count.index]

  tags = {
    Name                                        = "hermes-${var.environment}-private-${local.azs[count.index]}"
    "kubernetes.io/role/internal-elb"           = "1"
    "kubernetes.io/cluster/${var.cluster_name}" = "shared"
  }
}

# ------------------------------------------------------------------------------
# Internet Gateway
# ------------------------------------------------------------------------------

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "hermes-${var.environment}"
  }
}

# ------------------------------------------------------------------------------
# NAT Gateway(s)
# ------------------------------------------------------------------------------

resource "aws_eip" "nat" {
  count  = local.nat_gateway_count
  domain = "vpc"

  tags = {
    Name = "hermes-${var.environment}-nat-${count.index}"
  }
}

resource "aws_nat_gateway" "main" {
  count = local.nat_gateway_count

  allocation_id = aws_eip.nat[count.index].id
  subnet_id     = aws_subnet.public[count.index].id

  tags = {
    Name = "hermes-${var.environment}-nat-${count.index}"
  }

  # Same reason the EKS preconditions live on the resource rather than in a `validation`
  # block: cross-variable validation needs Terraform >= 1.9 and versions.tf declares >= 1.5.
  lifecycle {
    precondition {
      condition     = !var.single_az_workloads || var.single_nat_gateway
      error_message = "single_az_workloads is true but single_nat_gateway is false. Workloads run only in the first AZ, so the gateways in every other AZ would be provisioned, billed hourly, and never routed through. Set single_nat_gateway = true; the one gateway lands in the public subnet of the same AZ the nodes are in, which keeps egress same-AZ as well."
    }
  }

  depends_on = [aws_internet_gateway.main]
}

# ------------------------------------------------------------------------------
# Route Tables
# ------------------------------------------------------------------------------

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = {
    Name = "hermes-${var.environment}-public"
  }
}

resource "aws_route_table_association" "public" {
  count = length(local.azs)

  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table" "private" {
  count = local.nat_gateway_count

  vpc_id = aws_vpc.main.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.main[count.index].id
  }

  tags = {
    Name = "hermes-${var.environment}-private-${count.index}"
  }
}

resource "aws_route_table_association" "private" {
  count = length(local.azs)

  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private[count.index % local.nat_gateway_count].id
}
