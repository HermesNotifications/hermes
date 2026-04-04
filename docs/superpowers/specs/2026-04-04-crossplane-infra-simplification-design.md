# Crossplane Infrastructure Simplification

## Context

Hermes currently uses Terraform to provision all AWS infrastructure: VPC, EKS, RDS (Aurora), ElastiCache (Valkey), ECR, Secrets Manager, and CICD OIDC roles — 7 modules total. This works but creates friction:

- **Multi-cloud ambition:** Every new cloud requires rewriting all Terraform modules, including data services. We want to minimize cloud-specific IaC so adding GCP/Azure means only a thin "give me a cluster" layer.
- **Terraform surface area:** RDS, ElastiCache, and Secrets modules are the most complex and change most often. Managing them outside the cluster creates a two-tool workflow (Terraform + kubectl).
- **Operational preference:** The team wants K8s as the single control plane for everything running in or supporting the cluster.

**Solution:** Strip Terraform down to bare infrastructure (VPC + EKS + ECR + CICD) and move cloud-managed data services into Crossplane compositions with portable XRD abstractions.

## Infrastructure Split

| Layer | Tool | Resources | Lifecycle |
|-------|------|-----------|-----------|
| Base infra | Terraform | VPC, EKS, ECR, CICD OIDC role | Rarely changes; one module set per cloud |
| Cloud resources | Crossplane | Aurora, ElastiCache, Secrets Manager, SSM | K8s manifests, GitOps'd via ArgoCD |

## Directory Structure

```
infra/
  terraform/                    # Minimal — 4 modules only
    main.tf                     # VPC + EKS + ECR + CICD
    modules/
      vpc/                      # Unchanged
      eks/                      # + IRSA role for Crossplane
      ecr/                      # Unchanged
      cicd/                     # Unchanged
    environments/
      staging.tfvars            # Remove RDS/ElastiCache vars
      production.tfvars
  crossplane/
    provider/
      aws-provider.yaml         # AWS provider + ProviderConfig (IRSA)
    xrds/
      hermes-database.yaml      # CompositeResourceDefinition
      hermes-cache.yaml
      hermes-secrets.yaml
    compositions/
      aws/
        database.yaml           # Aurora PostgreSQL composition
        cache.yaml              # ElastiCache Valkey composition
        secrets.yaml            # Secrets Manager + SSM composition
    claims/
      staging/
        database.yaml           # HermesDatabase claim
        cache.yaml              # HermesCache claim
        secrets.yaml            # HermesSecrets claim
      production/
        database.yaml
        cache.yaml
        secrets.yaml
```

## Crossplane XRDs

### HermesDatabase

Wraps Aurora (AWS), Cloud SQL (GCP later).

**Exposed parameters:**
- `engineVersion` (string, default `"16.4"`)
- `instanceClass` (string, e.g. `"db.r7g.large"`)
- `instanceCount` (number, default `1`)
- `backupRetentionDays` (number, default `7`)
- `storageEncrypted` (bool, default `true`)
- `deletionProtection` (bool, default `false`)

**AWS composition creates:**
- `aws_db_subnet_group` — uses private subnets from VPC
- `aws_security_group` — allows 5432 from EKS node SG
- `aws_rds_cluster_parameter_group` — aurora-postgresql16, slow query logging
- `aws_rds_cluster` — Aurora PostgreSQL with encryption, backup window
- `aws_rds_cluster_instance` (N) — with Performance Insights enabled

**Connection secret keys:** `endpoint`, `username`, `password`, `port`

### HermesCache

Wraps ElastiCache Valkey (AWS), Memorystore (GCP later).

**Exposed parameters:**
- `engineVersion` (string, default `"7.2"`)
- `nodeType` (string, e.g. `"cache.r7g.large"`)
- `nodeCount` (number, default `1`)
- `transitEncryption` (bool, default `true`)
- `snapshotRetentionDays` (number, default `1`)

**AWS composition creates:**
- `aws_elasticache_subnet_group` — uses private subnets from VPC
- `aws_security_group` — allows 6379 from EKS node SG
- `aws_elasticache_replication_group` — Valkey with auth token, at-rest + transit encryption, automatic failover when nodeCount > 1

**Connection secret keys:** `endpoint`, `auth_token`, `port`

### HermesSecrets

Wraps Secrets Manager + SSM (AWS), Secret Manager (GCP later).

**Inputs:** References to HermesDatabase and HermesCache connection secrets.

**Creates:**
- Application secrets (JWT secret, Centrifugo token secret, Centrifugo API key) via random password generation
- `aws_secretsmanager_secret` + version assembling the full secret bundle: `database_url`, `redis_url`, `jwt_secret`, `centrifugo_token_secret`, `centrifugo_api_key`, `centrifugo_redis_address`, `centrifugo_redis_password`
- `aws_ssm_parameter` entries for operator-managed config (webhook URLs) with `ignore_changes` equivalent

**Connection secret keys:** Full secret bundle matching current shape consumed by services.

## Cross-Layer References

Crossplane compositions need VPC/EKS context (subnet IDs, node security group ID) to create subnet groups and security groups. Options:

- **Crossplane `EnvironmentConfig`:** Bootstrap script queries Terraform outputs and creates an `EnvironmentConfig` CR with VPC/subnet/SG IDs. Compositions reference these via `environment.patches`.
- **Direct claim parameters:** Pass subnet IDs and SG ID as claim parameters. Simpler but couples claims to cloud-specific IDs.

**Recommendation:** Use `EnvironmentConfig` — keeps claims cloud-agnostic while giving compositions the cloud-specific context they need. The bootstrap script already has access to Terraform outputs and can populate this.

## Crossplane Auth (IRSA)

- Terraform's EKS module creates an IAM role for Crossplane with permissions: `rds:*`, `elasticache:*`, `secretsmanager:*`, `ssm:*`, `ec2:CreateSecurityGroup`, `ec2:*SecurityGroupIngress/Egress`, `ec2:DeleteSecurityGroup`, `ec2:Describe*`
- Role is annotated on the Crossplane AWS provider's ServiceAccount
- ProviderConfig references the role ARN via `spec.credentials.source: IRSA`

## Bootstrap Changes

`infra/scripts/bootstrap-cluster.sh` adds after existing platform components:

1. Install Crossplane via Helm (`crossplane-stable/crossplane`)
2. Install `provider-aws` (Upbound official provider family)
3. Apply `ProviderConfig` with IRSA role ARN
4. Apply XRDs and compositions
5. Apply environment-specific claims

## GitOps Integration

- XRDs and compositions: managed by a new ArgoCD Application pointing at `infra/crossplane/` (shared across environments)
- Claims: either folded into existing staging/production ArgoCD Applications or a separate Application per environment pointing at `infra/crossplane/claims/{env}/`
- Kargo pipeline unchanged — it manages app image promotions, not infrastructure

## What Changes

| Area | Change |
|------|--------|
| `infra/terraform/modules/` | Delete `rds/`, `elasticache/`, `secrets/` |
| `infra/terraform/main.tf` | Remove RDS, ElastiCache, Secrets module blocks |
| `infra/terraform/variables.tf` | Remove `rds_*`, `elasticache_*` variables |
| `infra/terraform/outputs.tf` | Remove RDS/ElastiCache/Secrets outputs |
| `infra/terraform/environments/*.tfvars` | Remove RDS/ElastiCache values |
| `infra/terraform/modules/eks/` | Add IRSA role + policy for Crossplane |
| `infra/crossplane/` | New — all XRDs, compositions, provider config, claims |
| `infra/scripts/bootstrap-cluster.sh` | Add Crossplane + provider installation |
| `deploy/argocd/` | Add Application(s) for Crossplane resources |
| `docs/deployment-guide.md` | Update to reflect Crossplane workflow |

## What Stays the Same

- All service code, Dockerfiles, CI/CD pipelines
- Kustomize overlays for app deployments
- ArgoCD + Kargo promotion pipeline
- NATS and Centrifugo as in-cluster workloads
- Local dev (Docker Compose, k3d/Tilt)
- External Secrets Operator (still bridges secrets into pods)
- VPC, EKS, ECR, CICD Terraform modules

## Multi-Cloud Story

To add a second cloud (e.g., GCP):

1. Write new Terraform modules: GCP VPC + GKE + Artifact Registry (parallel to AWS)
2. Write new Crossplane compositions under `infra/crossplane/compositions/gcp/` implementing the same XRDs against Cloud SQL, Memorystore, Secret Manager
3. Claims and app manifests stay identical — composition selection determines which cloud resources are created

## Verification

1. **Terraform:** `terraform plan` on staging shows only VPC + EKS + ECR + CICD — no RDS/ElastiCache/Secrets
2. **Crossplane XRDs:** `kubectl get xrd` shows all three definitions healthy
3. **Compositions:** `kubectl get composition` shows AWS compositions ready
4. **Claims:** `kubectl get hermes-database,hermes-cache,hermes-secrets` shows claims bound
5. **AWS resources:** Verify Aurora cluster, ElastiCache replication group, and Secrets Manager secret exist in AWS console
6. **Secrets:** `kubectl get secret` confirms connection secrets populated with correct keys
7. **Services:** Deploy Hermes services and confirm they connect to Aurora and Valkey using the Crossplane-provisioned credentials
8. **Kustomize:** `make ci-kustomize` (or equivalent) validates all overlays still render
