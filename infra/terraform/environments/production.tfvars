environment = "production"
aws_region  = "us-east-1"

# Finding 38. Must not overlap staging (10.20.0.0/16) or any future environment.
# The allocation register lives in infra/terraform/variables.tf — read it before changing
# this. Changing it after apply rebuilds the entire environment.
vpc_cidr = "10.30.0.0/16"


eks_node_instance_types = ["m7g.large"]
eks_node_min_size       = 3
eks_node_max_size       = 10
eks_node_desired_size   = 3
github_org              = "darylrobbins"
