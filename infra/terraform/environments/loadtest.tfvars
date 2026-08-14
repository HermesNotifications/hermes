environment = "loadtest"
aws_region  = "us-east-1"

# Must not overlap staging (10.20.0.0/16) or production (10.30.0.0/16). The allocation
# register lives in infra/terraform/variables.tf — read it before changing this. Changing
# it after apply rebuilds the entire environment.
vpc_cidr = "10.40.0.0/16"

# eks_public_access_cidrs is REQUIRED and is deliberately absent from this file, exactly
# as in staging.tfvars and production.tfvars. Terraform will refuse to plan until you
# supply it. Setting it to [] does not block access; it means 0.0.0.0/0.
#
# Nothing here is retained: a load test's control-plane logs are interesting for the
# duration of the run and the triage after it, not for a quarter.
eks_cluster_log_retention_days = 7

# ------------------------------------------------------------------------------
# Single-AZ topology
# ------------------------------------------------------------------------------
#
# The reason this environment exists as its own tfvars file rather than as a copy of
# staging. Cross-AZ data transfer is $0.01/GB out plus $0.01/GB in, in-region, and a
# load test is nothing but data transfer: generators to the ingress, Send to NATS,
# Dispatch to Postgres and Redis, workers back onto NATS, Event Writer to Postgres. At
# 50k RPS with ~2 KB round trips that is on the order of 200 GB/hour crossing the wire,
# and a two-AZ spread puts roughly half of it across an AZ boundary in both directions.
#
# vpc_az_count is 2 rather than 1 because EKS requires control-plane subnets in at least
# two AZs and refuses the cluster otherwise. The second AZ's subnets are created and stay
# empty; an empty subnet is free. single_az_workloads is what actually pins the node
# groups, and it requires the single NAT gateway so that egress is same-AZ too.
#
# The trade: this environment has no availability story at all. Lose the AZ and the whole
# rig goes with it. That is correct for a load-test environment and would not be for
# staging, which is a rehearsal for production and should fail the way production fails.
vpc_az_count           = 2
vpc_single_nat_gateway = true
single_az_workloads    = true

# ------------------------------------------------------------------------------
# Capacity
# ------------------------------------------------------------------------------
#
# The system under test. Sized above staging because a rig that saturates before Hermes
# does measures the rig. Graviton throughout, per the project's ARM preference.
eks_node_instance_types = ["m7g.xlarge"]
eks_node_min_size       = 3
eks_node_max_size       = 12
eks_node_desired_size   = 3

# The load generators, on their own tainted pool so they never compete with the services
# they are measuring. min_size 0 means an idle loadtest environment runs no generator
# instances at all — scale it up for a run and it drains back to zero afterwards.
#
# Sizing per loadtest/k8s/node-pool.md: each k6 TestRun pod requests 2 vCPU / 4 GiB, so
# a c7g.2xlarge (8 vCPU / 16 GiB) holds three with headroom. max_size 12 covers a
# parallelism of roughly 30.
loadtest_generator_node_pool = {
  instance_types = ["c7g.2xlarge"]
  min_size       = 0
  max_size       = 12
  desired_size   = 0
}

github_org = "HermesNotifications"
