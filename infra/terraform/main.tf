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
}

module "rds" {
  source = "./modules/rds"

  environment             = var.environment
  vpc_id                  = module.vpc.vpc_id
  private_subnet_ids      = module.vpc.private_subnet_ids
  eks_security_group_id   = module.eks.node_security_group_id
  instance_class          = var.rds_instance_class
  multi_az                = var.rds_multi_az
  allocated_storage       = var.rds_allocated_storage
  backup_retention_period = var.rds_backup_retention_period
}

module "elasticache" {
  source = "./modules/elasticache"

  environment           = var.environment
  vpc_id                = module.vpc.vpc_id
  private_subnet_ids    = module.vpc.private_subnet_ids
  eks_security_group_id = module.eks.node_security_group_id
  node_type             = var.elasticache_node_type
  num_cache_nodes       = var.elasticache_num_cache_nodes
}

module "ecr" {
  source = "./modules/ecr"

  environment = var.environment
}

module "secrets" {
  source = "./modules/secrets"

  environment            = var.environment
  rds_endpoint           = module.rds.endpoint
  rds_username           = module.rds.master_username
  rds_password           = module.rds.master_password
  elasticache_endpoint   = module.elasticache.primary_endpoint
  elasticache_auth_token = module.elasticache.auth_token
}

module "cicd" {
  source = "./modules/cicd"

  ecr_repository_arns = values(module.ecr.repository_arns)
  github_org          = var.github_org
  github_repo         = var.github_repo
}
