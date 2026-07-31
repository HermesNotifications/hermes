module "vpc" {
  source = "./modules/vpc"

  environment        = var.environment
  vpc_cidr           = var.vpc_cidr
  cluster_name       = "hermes-${var.environment}"
  single_nat_gateway = var.environment == "staging"
  az_count           = var.environment == "production" ? 3 : 2
}

module "eks" {
  source = "./modules/eks"

  environment         = var.environment
  cluster_version     = var.eks_cluster_version
  vpc_id              = module.vpc.vpc_id
  private_subnet_ids  = module.vpc.private_subnet_ids
  node_instance_types = var.eks_node_instance_types
  node_min_size       = var.eks_node_min_size
  node_max_size       = var.eks_node_max_size
  node_desired_size   = var.eks_node_desired_size
  ecr_repository_arns = values(module.ecr.repository_arns)

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
