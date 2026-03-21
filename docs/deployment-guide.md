# Hermes Deployment Guide

End-to-end guide for provisioning infrastructure and deploying Hermes to staging and production on AWS EKS.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Architecture Overview](#architecture-overview)
- [1. Bootstrap Terraform State](#1-bootstrap-terraform-state)
- [2. Provision Infrastructure](#2-provision-infrastructure)
- [3. Bootstrap EKS Cluster](#3-bootstrap-eks-cluster)
- [4. Configure DNS](#4-configure-dns)
- [5. Update Placeholders](#5-update-placeholders)
- [6. Deploy with ArgoCD + Kargo](#6-deploy-with-argocd--kargo)
- [7. Verify Deployment](#7-verify-deployment)
- [8. Production Deployment](#8-production-deployment)
- [Day-2 Operations](#day-2-operations)
  - [Promoting to Production](#promoting-to-production)
  - [Scaling](#scaling)
  - [Secrets Rotation](#secrets-rotation)
  - [Database Migrations](#database-migrations)
  - [Upgrading EKS](#upgrading-eks)
  - [Monitoring and Debugging](#monitoring-and-debugging)
  - [Disaster Recovery](#disaster-recovery)

---

## Prerequisites

**Tools required:**

| Tool | Version | Purpose |
|------|---------|---------|
| AWS CLI | v2 | Cloud resource management |
| Terraform | >= 1.5 | Infrastructure provisioning |
| kubectl | >= 1.28 | Kubernetes management |
| Helm | >= 3.12 | Cluster component installation |
| kustomize | >= 5.0 | K8s manifest rendering (bundled with kubectl) |

**AWS access:**
- An AWS account with permissions to create VPC, EKS, RDS, ElastiCache, ECR, Secrets Manager, and IAM resources
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
│  │                    │   RDS   │              │Valkey  │   │  │
│  │                    │Postgres │              │(ElastiC│   │  │
│  │                    │  16     │              │ache)   │   │  │
│  │                    └─────────┘              └────────┘   │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌─────────────┐  ┌──────────────────┐  ┌─────────────────┐    │
│  │     ECR     │  │ Secrets Manager  │  │  S3 + DynamoDB  │    │
│  │  9 repos    │  │ hermes/staging   │  │  TF state       │    │
│  │             │  │ hermes/production│  │                  │    │
│  └─────────────┘  └──────────────────┘  └─────────────────┘    │
└─────────────────────────────────────────────────────────────────┘

Deployment Pipeline:
  git push → CI (build) → CD (push to ECR) → Kargo (promote) → ArgoCD (sync)
```

**Key infrastructure decisions:**
- **Graviton (ARM) instances** throughout — EKS nodes, RDS, ElastiCache — for better price/performance
- **Valkey** (not Redis) — BSD-licensed, wire-compatible, supported natively by ElastiCache
- **NATS runs in-cluster** as a StatefulSet (no AWS-managed equivalent)
- **Separate EKS clusters** per environment (not shared)

---

## 1. Bootstrap Terraform State

Create the S3 bucket and DynamoDB table that Terraform uses for remote state and locking. This is a one-time operation.

```bash
chmod +x infra/terraform/scripts/bootstrap-backend.sh
./infra/terraform/scripts/bootstrap-backend.sh us-east-1
```

This creates:
- S3 bucket `hermes-terraform-state` (versioned, encrypted, public access blocked)
- DynamoDB table `hermes-terraform-locks` (for state locking)

---

## 2. Provision Infrastructure

Start with staging. Each environment gets its own Terraform state file.

### Initialize Terraform

```bash
cd infra/terraform

terraform init \
  -backend-config="bucket=hermes-terraform-state" \
  -backend-config="key=hermes/staging/terraform.tfstate" \
  -backend-config="region=us-east-1" \
  -backend-config="dynamodb_table=hermes-terraform-locks" \
  -backend-config="encrypt=true"
```

### Review and apply

```bash
terraform plan -var-file=environments/staging.tfvars \
  -var="github_org=YOUR_GITHUB_ORG"

terraform apply -var-file=environments/staging.tfvars \
  -var="github_org=YOUR_GITHUB_ORG"
```

### Capture outputs

After apply completes, save the outputs — you'll need them for subsequent steps:

```bash
terraform output -json > ../../staging-outputs.json

# Key values you'll need:
terraform output ecr_registry_url          # e.g. 123456789012.dkr.ecr.us-east-1.amazonaws.com
terraform output eks_cluster_name           # e.g. hermes-staging
terraform output external_secrets_role_arn  # for bootstrap-cluster.sh
terraform output github_actions_role_arn    # for .github/workflows/cd.yml
```

### What Terraform creates

| Resource | Staging | Production |
|----------|---------|------------|
| VPC | 2 public + 2 private subnets, 1 NAT GW | Same but 2 NAT GWs (HA) |
| EKS | `t4g.medium` nodes, 2-4 count | `m7g.large` nodes, 3-10 count |
| RDS PostgreSQL 16 | `db.t4g.medium`, single-AZ, 20GB | `db.r7g.large`, multi-AZ, 100GB |
| ElastiCache Valkey 7.2 | `cache.t4g.micro`, 1 node | `cache.r7g.large`, 2 replicas |
| ECR | 9 repositories | Shared (apply once) |
| Secrets Manager | `hermes/staging` secret | `hermes/production` secret |
| IAM (CICD) | GitHub Actions OIDC role | Shared (apply once) |

---

## 3. Bootstrap EKS Cluster

Install the platform components (ingress, cert-manager, External Secrets Operator, ArgoCD, Kargo) onto the EKS cluster.

```bash
ESO_ROLE_ARN=$(cd infra/terraform && terraform output -raw external_secrets_role_arn)

chmod +x infra/scripts/bootstrap-cluster.sh
./infra/scripts/bootstrap-cluster.sh hermes-staging us-east-1 "$ESO_ROLE_ARN"
```

The script installs:

| Component | Namespace | Purpose |
|-----------|-----------|---------|
| NGINX Ingress Controller | `ingress-nginx` | Internet-facing NLB, routes traffic to services |
| cert-manager | `cert-manager` | Automatic TLS certificates via Let's Encrypt |
| External Secrets Operator | `external-secrets` | Syncs AWS Secrets Manager → K8s Secrets |
| ArgoCD | `argocd` | GitOps — syncs K8s manifests from git |
| Kargo | `kargo` | Promotion pipeline — manages staging → production flow |

After bootstrap, note the ArgoCD admin password printed to stdout. Access ArgoCD:

```bash
kubectl port-forward svc/argocd-server -n argocd 8080:443
# Open https://localhost:8080, login with admin / <password from output>
```

---

## 4. Configure DNS

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

## 5. Update Placeholders

Several files contain placeholder values that need to be replaced with your actual Terraform outputs.

### ECR registry URL

Replace `ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com` in:

| File | What to replace |
|------|-----------------|
| `.github/workflows/cd.yml` | `ECR_REGISTRY` env var and `role-to-assume` ARN |
| `deploy/kargo/warehouse.yaml` | `<ACCOUNT_ID>` in all image repoURLs |
| `deploy/k8s/overlays/staging/kustomization.yaml` | `REGISTRY/hermes-*` image newName values |
| `deploy/k8s/overlays/production/kustomization.yaml` | `REGISTRY/hermes-*` image newName values |

Use the actual ECR URL from `terraform output ecr_registry_url`.

### GitHub repository URL

Replace `OWNER` in:

| File | What to replace |
|------|-----------------|
| `deploy/argocd/staging.yaml` | `repoURL` |
| `deploy/argocd/production.yaml` | `repoURL` |
| `deploy/kargo/stages/staging.yaml` | `repoURL` in git-clone and argocd-update steps |
| `deploy/kargo/stages/production.yaml` | `repoURL` in git-clone and argocd-update steps |

### GitHub Actions IAM role

Update `.github/workflows/cd.yml`:
```yaml
role-to-assume: <terraform output github_actions_role_arn>
```

### Domain names

If your domain isn't `hermes.example.com`, update:
- `deploy/k8s/overlays/staging/patches/ingress.yaml`
- `deploy/k8s/overlays/production/patches/ingress.yaml`

### Webhook URLs

After deploying, update the webhook URLs in AWS Secrets Manager:
```bash
aws secretsmanager put-secret-value \
  --secret-id hermes/staging \
  --secret-string "$(
    aws secretsmanager get-secret-value --secret-id hermes/staging \
      --query SecretString --output text | \
    jq '.email_webhook_url = "https://your-email-provider.com/send" |
        .sms_webhook_url = "https://your-sms-provider.com/send"'
  )"
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

## 6. Deploy with ArgoCD + Kargo

### Apply ArgoCD Applications

```bash
kubectl apply -f deploy/argocd/staging.yaml
```

ArgoCD will immediately sync the staging overlay. Watch the sync:

```bash
# Via CLI
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
# Watch the CD pipeline
gh run watch

# Watch Kargo promotion
kubectl port-forward svc/kargo-api -n kargo 8443:443
# Open https://localhost:8443
```

---

## 7. Verify Deployment

### Check pods

```bash
kubectl get pods -n hermes
```

All 8 service deployments + NATS StatefulSet + Centrifugo should be Running:

```
NAME                              READY   STATUS    RESTARTS
hermes-admin-xxx                  1/1     Running   0
hermes-router-xxx                 1/1     Running   0
hermes-worker-events-xxx          1/1     Running   0
hermes-worker-email-xxx           1/1     Running   0
hermes-worker-sms-xxx             1/1     Running   0
hermes-worker-inbox-xxx           1/1     Running   0
hermes-inbox-xxx                  1/1     Running   0
hermes-user-xxx                   1/1     Running   0
centrifugo-xxx                    1/1     Running   0
nats-0                            1/1     Running   0
```

### Check health endpoints

```bash
DOMAIN="staging.hermes.example.com"

curl -s https://$DOMAIN/v1/send     # Should return 401 (no API key)
curl -s https://$DOMAIN/v1/inbox    # Should return 401 (no JWT)
```

### Run seed (first time only)

Create an initial tenant and API key for testing:

```bash
# Port-forward the admin service
kubectl port-forward svc/hermes-admin -n hermes 8080:8080

# Use the existing seed script or create a tenant via API
curl -X POST http://localhost:8080/v1/auth/tenants \
  -H "Content-Type: application/json" \
  -d '{"name": "test-tenant"}'
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

## 8. Production Deployment

Repeat steps 2-7 with production-specific values.

### Re-initialize Terraform for production

```bash
cd infra/terraform

# Switch to production state file
terraform init -reconfigure \
  -backend-config="bucket=hermes-terraform-state" \
  -backend-config="key=hermes/production/terraform.tfstate" \
  -backend-config="region=us-east-1" \
  -backend-config="dynamodb_table=hermes-terraform-locks" \
  -backend-config="encrypt=true"

terraform apply -var-file=environments/production.tfvars \
  -var="github_org=YOUR_GITHUB_ORG"
```

### Bootstrap production cluster

```bash
ESO_ROLE_ARN=$(terraform output -raw external_secrets_role_arn)
./infra/scripts/bootstrap-cluster.sh hermes-production us-east-1 "$ESO_ROLE_ARN"
```

### Apply ArgoCD + Kargo

```bash
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
5. An operator approves the promotion in the Kargo UI (https://localhost:8443)
6. Kargo updates the production overlay in git
7. ArgoCD syncs the production cluster

To check what's currently deployed:

```bash
# Staging image tags
kubectl get deployments -n hermes -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.template.spec.containers[0].image}{"\n"}{end}'

# Kargo freight status
kubectl get freight -n hermes
```

### Scaling

**Horizontal (node count):** Update `eks_node_min_size` / `eks_node_max_size` in the appropriate tfvars file and re-apply Terraform.

**Horizontal (pod count):** Production services use HPA (Horizontal Pod Autoscaler). To adjust:
- Edit `deploy/k8s/overlays/production/hpa/*.yaml`
- Commit and push — ArgoCD auto-syncs

**Vertical (instance size):** Update instance types in tfvars and re-apply. For EKS nodes, this triggers a rolling replacement. For RDS/ElastiCache, expect brief downtime during the maintenance window.

### Secrets Rotation

Secrets are stored in AWS Secrets Manager and synced to K8s by External Secrets Operator (1h refresh interval).

**Rotate database password:**
```bash
# Generate new password
NEW_PASS=$(openssl rand -base64 32)

# Update RDS
aws rds modify-db-instance \
  --db-instance-identifier hermes-production \
  --master-user-password "$NEW_PASS"

# Update Secrets Manager
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
# Port-forward to RDS via a pod
kubectl run pg-client --rm -it --image=postgres:16-alpine \
  --env="PGPASSWORD=<password>" \
  -- psql -h <rds-endpoint> -U hermes -d hermes

# Or trigger the migration job
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
2. Run `terraform plan` to review — the cluster upgrade is in-place, node group replacement is rolling
3. Apply: `terraform apply -var-file=environments/<env>.tfvars -var="github_org=..."`
4. The cluster control plane upgrades first (~15 min), then the node group rolls (~15 min per node)
5. Verify: `kubectl get nodes` — all nodes should show the new version

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

**ArgoCD sync issues:**
```bash
kubectl get applications -n argocd hermes-staging -o yaml | grep -A 20 'status:'

# Force a re-sync
kubectl patch application hermes-staging -n argocd --type merge \
  -p '{"operation": {"sync": {"revision": "HEAD"}}}'
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

# Port-forward to RDS (from a pod, since RDS is in private subnet)
kubectl run pg-client --rm -it --image=postgres:16-alpine \
  --env="PGPASSWORD=<password>" \
  -- psql -h <rds-endpoint> -U hermes -d hermes
```

### Disaster Recovery

**RDS:**
- Automated daily backups (7 days staging, 30 days production)
- Point-in-time recovery available within the retention window
- Multi-AZ failover is automatic in production

```bash
# Restore from snapshot
aws rds restore-db-instance-from-db-snapshot \
  --db-instance-identifier hermes-production-restored \
  --db-snapshot-identifier <snapshot-id>
```

**NATS JetStream:**
- Data stored on EBS PVCs (5Gi per replica)
- Stream retention is 7 days
- If all NATS pods are lost, streams are recreated automatically by services on startup (empty)
- In-flight messages would be lost — services use durable consumers with explicit ack, so unacknowledged messages are redelivered

**Valkey:**
- Production has 2 replicas across AZs with automatic failover
- Daily snapshots (7 days retention)
- Cache data is reconstructable — Hermes uses Redis as a cache layer, not as primary storage

**Full cluster recovery:**
1. `terraform apply` recreates cloud infrastructure from state
2. `./infra/scripts/bootstrap-cluster.sh` reinstalls platform components
3. `kubectl apply -f deploy/argocd/` + `kubectl apply -f deploy/kargo/` restores GitOps
4. ArgoCD auto-syncs the application — all K8s resources are defined in git
5. Migration job runs automatically, seed data may need to be re-applied

---

## Quick Reference

### File locations

| What | Where |
|------|-------|
| Terraform modules | `infra/terraform/modules/` |
| Environment configs | `infra/terraform/environments/*.tfvars` |
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
# View what's deployed
kubectl get all -n hermes

# Render Kustomize overlay without applying
kubectl kustomize deploy/k8s/overlays/staging

# Switch kubectl context between clusters
aws eks update-kubeconfig --name hermes-staging --region us-east-1
aws eks update-kubeconfig --name hermes-production --region us-east-1

# ArgoCD CLI (if installed)
argocd app list
argocd app sync hermes-staging

# Terraform switch environments
terraform init -reconfigure -backend-config="key=hermes/staging/terraform.tfstate" ...
terraform init -reconfigure -backend-config="key=hermes/production/terraform.tfstate" ...
```
