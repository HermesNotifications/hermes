# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

locals {
  # The EKS CLUSTER always gets every private subnet: its vpc_config must name subnets
  # in at least two AZs or the API rejects it, so a single-AZ environment is still a
  # two-AZ VPC. What single_az_workloads pins is everything that moves bytes — the node
  # groups below, and (via vpc_single_nat_gateway) the NAT gateway, which the vpc module
  # places in the public subnet of this same first AZ. Nothing schedules into the second
  # AZ, so nothing is billed for crossing between them.
  #
  # slice() rather than [0] to keep this a list(string) for both branches.
  workload_subnet_ids = var.single_az_workloads ? slice(module.vpc.private_subnet_ids, 0, 1) : module.vpc.private_subnet_ids
}

module "vpc" {
  source = "./modules/vpc"

  environment         = var.environment
  vpc_cidr            = var.vpc_cidr
  cluster_name        = "hermes-${var.environment}"
  single_nat_gateway  = var.vpc_single_nat_gateway
  az_count            = var.vpc_az_count
  single_az_workloads = var.single_az_workloads
}

module "eks" {
  source = "./modules/eks"

  environment         = var.environment
  cluster_version     = var.eks_cluster_version
  vpc_id              = module.vpc.vpc_id
  private_subnet_ids  = module.vpc.private_subnet_ids
  node_subnet_ids     = local.workload_subnet_ids
  node_instance_types = var.eks_node_instance_types
  node_min_size       = var.eks_node_min_size
  node_max_size       = var.eks_node_max_size
  node_desired_size   = var.eks_node_desired_size
  ecr_repository_arns = values(module.ecr.repository_arns)

  loadtest_generator_node_pool = var.loadtest_generator_node_pool

  # Finding 5. The root module previously passed none of these, so the eks module's
  # public_access_cidrs default of ["0.0.0.0/0"] applied to both environments.
  endpoint_public_access            = var.eks_endpoint_public_access
  public_access_cidrs               = var.eks_public_access_cidrs
  allow_public_access_from_anywhere = var.eks_allow_public_access_from_anywhere
  cluster_log_retention_days        = var.eks_cluster_log_retention_days
}

module "ecr" {
  source = "./modules/ecr"

  environment = var.environment
}

module "cicd" {
  source = "./modules/cicd"

  ecr_repository_arns = values(module.ecr.repository_arns)
  github_org          = var.github_org
  github_repo         = var.github_repo
}
