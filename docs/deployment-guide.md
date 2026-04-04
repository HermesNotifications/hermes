# Hermes Deployment Guide

End-to-end guide for provisioning infrastructure and deploying Hermes to staging and production on AWS EKS.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Architecture Overview](#architecture-overview)
- [1. Bootstrap Terraform State](#1-bootstrap-terraform-state)
- [2. Provision Infrastructure](#2-provision-infrastructure)
- [3. Configure GitHub Actions](#3-configure-github-actions)
- [4. Bootstrap EKS Cluster](#4-bootstrap-eks-cluster)
- [5. Configure DNS](#5-configure-dns)
- [6. Update Placeholders](#6-update-placeholders)
- [7. Deploy with ArgoCD + Kargo](#7-deploy-with-argocd--kargo)
- [8. Verify Deployment](#8-verify-deployment)
- [9. Production Deployment](#9-production-deployment)
- [Day-2 Operations](#day-2-operations)

---

## Prerequisites

**Tools required:**

| Tool | Version | Purpose |
|------|---------|---------|
| AWS CLI | v2 | Cloud resource management |
| Terraform | >= 1.10 | Infrastructure provisioning (native S3 locking) |
| kubectl | >= 1.28 | Kubernetes management |
| Helm | >= 3.12 | Cluster component installation |
| jq | any | JSON processing |
| crossplane (optional) | >= 1.18 | Debugging Crossplane resources (`crossplane beta trace`, etc.) |

**AWS access:**
- An AWS account with permissions to create VPC, EKS, ECR, and IAM resources
- AWS CLI configured (`aws configure` or environment variables)

**GitHub:**
- Repository pushed to GitHub (needed for ArgoCD sync and Kargo git operations)
- Repository settings configured for GitHub Actions OIDC (Settings > Environments > production)

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│  AWS Account                                                    │
│                                                                 │
│  ┌──────────────── VPC (10.0.0.0/16) ───────────────────────┐  │
│  │                                                           │  │
│  │  Public Subnets          Private Subnets                  │  │
│  │  ┌─────────────┐        ┌────────────────────────────┐   │  │
│  │  │ NAT Gateway │        │  EKS Cluster               │   │  │
│  │  │ NLB (nginx) │───────>│  ┌──────────────────────┐  │   │  │
│  │  └─────────────┘        │  │ hermes namespace     │  │   │  │
│  │                         │  │  8 services + NATS   │  │   │  │
│  │                         │  │  + Centrifugo        │  │   │  │
│  │                         │  └──────┬───────┬───────┘  │   │  │
│  │                         └─────────┼───────┼──────────┘   │  │
│  │                                   │       │              │  │
│  │                         ┌─────────┘       └─────────┐    │  │
│  │                         │                           │    │  │
│  │                    ┌────▼────┐              ┌───────▼┐   │  │
│  │                    │ Aurora  │              │Valkey  │   │  │
│  │                    │Postgres │              │(ElastiC│   │  │
│  │                    │  16     │              │ache)   │   │  │
│  │                    └─────────┘              └────────┘   │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌─────────────┐  ┌─────────────────┐                          │
│  │     ECR     │  │    S3 bucket    │                          │
│  │  9 repos    │  │  TF state       │                          │
│  └─────────────┘  └─────────────────┘                          │
└─────────────────────────────────────────────────────────────────┘

Aurora PostgreSQL, ElastiCache Valkey, and Secrets Manager are managed
by Crossplane (in-cluster) rather than Terraform. Terraform only manages
VPC, EKS, ECR, and the CICD OIDC role.

Deployment Pipeline:
  git push → CI (build) → CD (push to ECR) → Kargo (promote) → ArgoCD (sync)
```

**Key infrastructure decisions:**
- **Graviton (ARM) instances** throughout — EKS nodes, Aurora, ElastiCache — for better price/performance
- **Aurora PostgreSQL** — auto-scaling storage, automatic failover with read replicas
- **Valkey** (not Redis) — BSD-licensed, wire-compatible, supported natively by ElastiCache
- **NATS runs in-cluster** as a StatefulSet (no AWS-managed equivalent)
- **Separate EKS clusters** per environment (not shared)
- **Crossplane manages data services** — Aurora, ElastiCache, and Secrets Manager are declared as Crossplane claims inside the cluster, enabling GitOps for infrastructure

---

## 1. Bootstrap Terraform State

Create the S3 bucket that Terraform uses for remote state. Terraform 1.10+ handles locking natively via S3 — no DynamoDB table needed. This is a one-time operation.

```bash
make tf-bootstrap REGION=us-east-1
```

This creates an S3 bucket `hermes-terraform-state-471524413120` (versioned, encrypted, public access blocked).

---

## 2. Provision Infrastructure

Start with staging. The `tfenv.sh` wrapper handles backend init, secret reconciliation, and environment selection automatically.

```bash
make tf-plan ENV=staging
make tf-apply ENV=staging    # requires manual approval
```

### Capture outputs

After apply completes, save key outputs for subsequent steps:

```bash
cd infra/terraform
terraform output -raw eks_cluster_name          # e.g. hermes-staging
terraform output -raw external_secrets_role_arn  # for bootstrap-cluster.sh
terraform output -raw crossplane_role_arn       # for bootstrap-cluster.sh
terraform output -raw vpc_id                    # used by Crossplane EnvironmentConfig
terraform output -raw private_subnet_ids        # used by Crossplane EnvironmentConfig
terraform output -raw node_security_group_id    # used by Crossplane EnvironmentConfig
```

### What Terraform creates

| Resource | Staging | Production |
|----------|---------|------------|
| VPC | 2 AZs, 1 NAT GW | 3 AZs, 3 NAT GWs (HA) |
| EKS | `t4g.medium` nodes, 2-4 count | `m7g.large` nodes, 3-10 count |
| ECR | 9 repositories (AES256 encrypted) | Shared (apply once) |
| IAM (CICD) | GitHub Actions OIDC role | Shared (apply once) |
| IAM (Crossplane) | IRSA role for Crossplane AWS provider | Per-cluster |

> **Note:** Aurora PostgreSQL, ElastiCache Valkey, and Secrets Manager are now managed by Crossplane compositions inside the EKS cluster. See [Deploy with ArgoCD](#7-deploy-with-argocd--kargo) for details.

---

## 3. Configure GitHub Actions

Set your AWS account ID as a repository secret (keeps it out of the codebase):

```bash
gh secret set AWS_ACCOUNT_ID --body "$(aws sts get-caller-identity --query Account --output text)"
```

The CD workflow derives the ECR registry URL and IAM role ARN at runtime — no hardcoded account IDs.

---

## 4. Bootstrap EKS Cluster

Install platform components (ingress, cert-manager, External Secrets Operator, ArgoCD, Kargo) onto the EKS cluster.

```bash
ESO_ROLE_ARN=$(cd infra/terraform && terraform output -raw external_secrets_role_arn)
KARGO_ROLE_ARN=$(cd infra/terraform && terraform output -raw kargo_controller_role_arn)
CROSSPLANE_ROLE_ARN=$(cd infra/terraform && terraform output -raw crossplane_role_arn)

./infra/scripts/bootstrap-cluster.sh hermes-staging us-east-1 "$ESO_ROLE_ARN" "$KARGO_ROLE_ARN" "$CROSSPLANE_ROLE_ARN"
```

The 5th parameter (`crossplane-role-arn`) tells the bootstrap script to install Crossplane with the AWS provider configured for IRSA authentication. The script also creates an `EnvironmentConfig` from Terraform outputs (`vpc_id`, `private_subnet_ids`, `node_security_group_id`) so that Crossplane compositions can place resources in the correct VPC and subnets.

The script installs:

| Component | Namespace | Purpose |
|-----------|-----------|---------|
| NGINX Ingress Controller | `ingress-nginx` | Internet-facing NLB, routes traffic to services |
| cert-manager | `cert-manager` | Automatic TLS certificates via Let's Encrypt |
| External Secrets Operator | `external-secrets` | Syncs AWS Secrets Manager → K8s Secrets |
| Crossplane + AWS Provider | `crossplane-system` | Manages Aurora, ElastiCache, and Secrets Manager as in-cluster resources |
| ArgoCD | `argocd` | GitOps — syncs K8s manifests from git |
| Kargo | `kargo` | Promotion pipeline — manages staging → production flow |

After bootstrap, note the ArgoCD and Kargo admin passwords printed to stdout. Access them:

```bash
kubectl port-forward svc/argocd-server -n argocd 8080:443
# Open https://localhost:8080, login with admin / <password from output>

kubectl port-forward svc/kargo-api -n kargo 8443:443
# Open https://localhost:8443
```

---

## 5. Configure DNS

Get the NLB hostname created by the NGINX ingress controller:

```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller \
  -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
```

Create a DNS record pointing your domain to this NLB:

| Record | Type | Value |
|--------|------|-------|
| `staging.hermes.example.com` | CNAME | `<NLB hostname from above>` |

For production, use `hermes.example.com` → production NLB.

> **Note:** If using Route53, you can create an Alias record (A record) instead of CNAME, which avoids the extra DNS lookup.

---

## 6. Update Placeholders

### ECR registry URL

Replace `ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com` in:

| File | What to replace |
|------|-----------------|
| `deploy/kargo/warehouse.yaml` | `471524413120` in all image repoURLs |
| `deploy/k8s/overlays/staging/kustomization.yaml` | `REGISTRY/hermes-*` image newName values |
| `deploy/k8s/overlays/production/kustomization.yaml` | `REGISTRY/hermes-*` image newName values |

Get the actual ECR URL:
```bash
cd infra/terraform && terraform output -raw ecr_registry_url
```

> **Note:** `.github/workflows/cd.yml` does **not** need updating — it derives the ECR registry at runtime from the ECR login step.

### GitHub repository URL

Replace `OWNER` in:

| File | What to replace |
|------|-----------------|
| `deploy/argocd/staging.yaml` | `repoURL` |
| `deploy/argocd/production.yaml` | `repoURL` |
| `deploy/kargo/stages/staging.yaml` | `repoURL` in git-clone and argocd-update steps |
| `deploy/kargo/stages/production.yaml` | `repoURL` in git-clone and argocd-update steps |

### Domain names

If your domain isn't `hermes.example.com`, update:
- `deploy/k8s/overlays/staging/patches/ingress.yaml`
- `deploy/k8s/overlays/production/patches/ingress.yaml`

### Webhook URLs

After deploying, update the webhook URLs in SSM Parameter Store:
```bash
aws ssm put-parameter --name "/hermes/staging/email_webhook_url" \
  --value "https://your-email-provider.com/send" --overwrite

aws ssm put-parameter --name "/hermes/staging/sms_webhook_url" \
  --value "https://your-sms-provider.com/send" --overwrite
```

### Let's Encrypt email

Update the ACME email in `infra/scripts/bootstrap-cluster.sh` (line with `email: ops@example.com`) before running bootstrap.

### Commit and push

```bash
git add -A
git commit -m "chore(infra): configure deployment placeholders for staging"
git push
```

---

## 7. Deploy with ArgoCD + Kargo

### Apply ArgoCD Applications

```bash
# Crossplane XRDs, compositions, and functions (shared across environments)
kubectl apply -f deploy/argocd/crossplane-infra.yaml

# Crossplane resource claims for staging (Aurora, ElastiCache, Secrets Manager)
kubectl apply -f deploy/argocd/crossplane-claims-staging.yaml

# Hermes application for staging
kubectl apply -f deploy/argocd/staging.yaml
```

> **Important:** Apply `crossplane-infra` first — it installs the XRDs and compositions that the claims depend on. Wait for it to sync before applying the claims app. The claims app provisions the actual AWS resources (Aurora cluster, ElastiCache, Secrets Manager secret), which takes 10-15 minutes for the initial creation.

ArgoCD will immediately sync the staging overlay. Watch the sync:

```bash
kubectl get applications -n argocd
# Or via the ArgoCD UI at https://localhost:8080
```

### Apply Kargo resources

```bash
kubectl apply -f deploy/kargo/project.yaml
kubectl apply -f deploy/kargo/warehouse.yaml
kubectl apply -f deploy/kargo/analysis/health-check.yaml
kubectl apply -f deploy/kargo/stages/staging.yaml
kubectl apply -f deploy/kargo/stages/production.yaml
```

### Trigger first build

Push any commit to `main` to trigger the CD pipeline. This builds all 9 images and pushes them to ECR. Kargo then detects the new images and promotes to staging.

```bash
gh run watch
```

---

## 8. Verify Deployment

### Check pods

```bash
kubectl get pods -n hermes
```

All 8 service deployments + NATS StatefulSet + Centrifugo should be Running.

### Check health endpoints

```bash
DOMAIN="staging.hermes.example.com"
curl -s https://$DOMAIN/v1/send     # Should return 401 (no API key)
curl -s https://$DOMAIN/v1/inbox    # Should return 401 (no JWT)
```

### Check ArgoCD sync status

```bash
kubectl get applications -n argocd hermes-staging \
  -o jsonpath='{.status.sync.status}'
# Should print: Synced
```

### Check Kargo stages

```bash
kubectl get stages -n hermes
# staging stage should show "Healthy" after first successful promotion
```

---

## 9. Production Deployment

Repeat steps 2-8 with production values.

### Provision production infrastructure

```bash
make tf-plan ENV=production
make tf-apply ENV=production    # requires manual approval
```

### Bootstrap production cluster

```bash
ESO_ROLE_ARN=$(cd infra/terraform && terraform output -raw external_secrets_role_arn)
KARGO_ROLE_ARN=$(cd infra/terraform && terraform output -raw kargo_controller_role_arn)
CROSSPLANE_ROLE_ARN=$(cd infra/terraform && terraform output -raw crossplane_role_arn)
./infra/scripts/bootstrap-cluster.sh hermes-production us-east-1 "$ESO_ROLE_ARN" "$KARGO_ROLE_ARN" "$CROSSPLANE_ROLE_ARN"
```

### Apply ArgoCD + Kargo

```bash
kubectl apply -f deploy/argocd/crossplane-infra.yaml
kubectl apply -f deploy/argocd/crossplane-claims-production.yaml
kubectl apply -f deploy/argocd/production.yaml
kubectl apply -f deploy/kargo/  # Kargo resources are shared if on same cluster,
                                 # or re-apply if separate clusters
```

### First production promotion

In the Kargo UI, the production stage will show a "Promote" button once the staging stage has a verified freight. Click it to deploy.

---

## Day-2 Operations

### Promoting to Production

The normal deployment flow:

1. Merge PR to `main`
2. CI runs tests, CD builds and pushes images to ECR
3. Kargo auto-promotes to staging, runs health checks
4. Once staging is verified, Kargo shows production promotion as available
5. An operator approves the promotion in the Kargo UI
6. Kargo updates the production overlay in git
7. ArgoCD syncs the production cluster

To check what's currently deployed:

```bash
kubectl get deployments -n hermes -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.template.spec.containers[0].image}{"\n"}{end}'
kubectl get freight -n hermes
```

### Scaling

**Horizontal (EKS node count):** Update `eks_node_min_size` / `eks_node_max_size` in the appropriate tfvars file:
```bash
make tf-plan ENV=production
make tf-apply ENV=production
```

**Horizontal (pod count):** Production services use HPA. To adjust:
- Edit `deploy/k8s/overlays/production/hpa/*.yaml`
- Commit and push — ArgoCD auto-syncs

**Vertical (EKS node size):** Update instance types in tfvars and re-apply. This triggers a rolling replacement of EKS nodes.

**Aurora / ElastiCache scaling:** Edit the Crossplane claims in git and push. ArgoCD syncs the change, and Crossplane applies it to AWS:
```bash
# Example: scale Aurora instance class or add a read replica
vim infra/crossplane/claims/production/database.yaml   # change instanceClass or replicaCount
# Example: scale ElastiCache node type or replica count
vim infra/crossplane/claims/production/cache.yaml       # change nodeType or nodeCount
git add -A && git commit -m "chore(infra): scale Aurora to r7g.xlarge" && git push
```
Crossplane applies changes with the same maintenance-window behavior as the AWS console. Expect brief downtime for instance class changes.

### Secrets Rotation

Secrets are stored in AWS Secrets Manager and synced to K8s by External Secrets Operator (1h refresh interval). The Secrets Manager secret itself is managed by the `HermesSecretsBundle` Crossplane composition, which assembles connection details from the Aurora and ElastiCache claims.

**Rotate database password:**
```bash
NEW_PASS=$(openssl rand -base64 32)

# Update Aurora
aws rds modify-db-cluster \
  --db-cluster-identifier hermes-production \
  --master-user-password "$NEW_PASS" \
  --apply-immediately

# Update Secrets Manager (Crossplane manages the secret resource, but you can update the value)
aws secretsmanager put-secret-value \
  --secret-id hermes/production \
  --secret-string "$(
    aws secretsmanager get-secret-value --secret-id hermes/production \
      --query SecretString --output text | \
    jq --arg pw "$NEW_PASS" '.database_url = "postgres://hermes:\($pw)@" + (.database_url | split("@")[1])'
  )"

# Force ESO to resync (or wait up to 1 hour)
kubectl annotate externalsecret hermes-secrets -n hermes force-sync=$(date +%s) --overwrite

# Restart pods to pick up new secret
kubectl rollout restart deployment -n hermes
```

**Rotate JWT secret:** Same pattern — update Secrets Manager, force ESO resync, restart pods. Existing JWTs will be invalidated.

**Rotate Valkey auth token:** Update ElastiCache auth token, update Secrets Manager, force resync, restart.

### Database Migrations

Migrations run automatically as a K8s Job during each ArgoCD sync (defined in `deploy/k8s/base/migration-job.yaml`). The job runs `cmd/migrate` with the database URL from Secrets Manager.

**Run migrations manually:**
```bash
kubectl delete job hermes-migration -n hermes --ignore-not-found
kubectl apply -f deploy/k8s/base/migration-job.yaml
```

**Rollback a migration:**
```bash
kubectl run migrate-down --rm -it \
  --image=$(kubectl get deploy hermes-admin -n hermes -o jsonpath='{.spec.template.spec.containers[0].image}') \
  -- /migrate -database-url="$DATABASE_URL" -migrations-path=/migrations down 1
```

### Upgrading EKS

1. Update `eks_cluster_version` in the tfvars file
2. Review the plan:
   ```bash
   make tf-plan ENV=production
   ```
3. Apply — the cluster control plane upgrades first (~15 min), then the node group rolls (~15 min per node):
   ```bash
   make tf-apply ENV=production
   ```
4. Verify: `kubectl get nodes` — all nodes should show the new version

> **Important:** Only upgrade one minor version at a time (e.g., 1.31 → 1.32). Check the [EKS release calendar](https://docs.aws.amazon.com/eks/latest/userguide/kubernetes-versions.html) for deprecations.

### Monitoring and Debugging

**View logs:**
```bash
# Specific service
kubectl logs -n hermes -l app.kubernetes.io/name=hermes-admin --tail=100 -f

# All services
kubectl logs -n hermes -l app.kubernetes.io/part-of=hermes --tail=50

# NATS
kubectl logs -n hermes nats-0 --tail=100
```

**Check resource usage:**
```bash
kubectl top pods -n hermes
kubectl top nodes
```

**Debug a failing pod:**
```bash
kubectl describe pod <pod-name> -n hermes
kubectl logs <pod-name> -n hermes --previous  # logs from crashed container
```

**NATS JetStream status:**
```bash
kubectl exec -n hermes nats-0 -- nats stream ls
kubectl exec -n hermes nats-0 -- nats stream info NOTIFICATIONS
kubectl exec -n hermes nats-0 -- nats consumer ls DELIVERY
```

**Connect to database:**
```bash
# Get connection string from secret
kubectl get secret hermes-secrets -n hermes -o jsonpath='{.data.HERMES_DATABASE_URL}' | base64 -d
```

### Disaster Recovery

**Aurora PostgreSQL:**
- Automated continuous backups (7 days staging, 30 days production)
- Point-in-time recovery available within the retention window
- Production runs 2 instances across AZs with automatic failover

**NATS JetStream:**
- Data stored on EBS PVCs
- Stream retention is 7 days
- If all NATS pods are lost, streams are recreated automatically by services on startup
- Unacknowledged messages are redelivered via durable consumers with explicit ack

**Valkey:**
- Production has 2 replicas across AZs with automatic failover
- Daily snapshots (7 days retention)
- Cache data is reconstructable — Hermes uses Valkey as a cache layer, not as primary storage

**Full cluster recovery:**
1. `make tf-apply ENV=<environment>` recreates VPC, EKS, ECR, and IAM from state
2. `./infra/scripts/bootstrap-cluster.sh` reinstalls platform components including Crossplane, the AWS provider, and the EnvironmentConfig from Terraform outputs — this must complete before ArgoCD can reconcile Crossplane claims
3. `kubectl apply -f deploy/argocd/` + `kubectl apply -f deploy/kargo/` restores GitOps
4. ArgoCD syncs `crossplane-infra` (XRDs/compositions) then `crossplane-claims-<env>` (data services) — Aurora and ElastiCache are recreated from the claim specs
5. ArgoCD auto-syncs the application — all K8s resources are defined in git
6. Migration job runs automatically, seed data may need to be re-applied

---

## Quick Reference

### File locations

| What | Where |
|------|-------|
| Terraform modules | `infra/terraform/modules/` |
| Environment configs | `infra/terraform/environments/*.tfvars` |
| Crossplane XRDs | `infra/crossplane/xrds/` — Cloud-agnostic resource definitions |
| Crossplane compositions | `infra/crossplane/compositions/aws/` — AWS-specific implementations |
| Crossplane claims | `infra/crossplane/claims/{env}/` — Per-environment resource claims |
| Crossplane provider config | `infra/crossplane/provider/` — AWS provider and auth config |
| K8s base manifests | `deploy/k8s/base/` |
| Staging overlay | `deploy/k8s/overlays/staging/` |
| Production overlay | `deploy/k8s/overlays/production/` |
| ArgoCD apps | `deploy/argocd/` |
| Kargo pipeline | `deploy/kargo/` |
| CI pipeline | `.github/workflows/ci.yml` |
| CD pipeline | `.github/workflows/cd.yml` |
| Bootstrap scripts | `infra/scripts/`, `infra/terraform/scripts/` |
| Database migrations | `migrations/` |

### Useful commands

```bash
# Terraform — plan/apply/destroy via wrapper
make tf-plan ENV=staging
make tf-apply ENV=staging
make tf-destroy ENV=staging

# View what's deployed
kubectl get all -n hermes

# Render Kustomize overlay without applying
kubectl kustomize deploy/k8s/overlays/staging

# Switch kubectl context between clusters
aws eks update-kubeconfig --name hermes-staging --region us-east-1
aws eks update-kubeconfig --name hermes-production --region us-east-1
```
