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
│  │                         │  │  9 services + NATS   │  │   │  │
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
│  │  10 repos   │  │  TF state       │                          │
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
| EKS | `t4g.large` nodes, 2-4 count | `m7g.large` nodes, 3-10 count |
| ECR | 10 repositories (AES256 encrypted) | Shared (apply once) |
| IAM (CICD) | GitHub Actions OIDC role | Shared (apply once) |
| IAM (Crossplane) | IRSA role for Crossplane AWS provider | Per-cluster |

> **Note:** Aurora PostgreSQL, ElastiCache Valkey, and Secrets Manager are now managed by Crossplane compositions inside the EKS cluster. See [Deploy with ArgoCD](#7-deploy-with-argocd--kargo) for details.

---

## 3. Configure GitHub Actions

Set your AWS account ID as a repository secret (keeps it out of the codebase):

```bash
gh secret set AWS_ACCOUNT_ID --body "$(aws sts get-caller-identity --query Account --output text)"
```

The CD workflow derives the ECR registry URL and IAM role ARN at runtime, so it carries no
hardcoded account IDs. The deploy manifests are a different matter — see the table above.

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

> `example.com` above is a stand-in for your own domain. Note what the overlays in this
> repository currently commit, which is not symmetric: staging is already set to a real
> domain (`staging.hermes.dgr.io`, in `overlays/staging/kustomization.yaml` and its ingress
> patch), while **production is still the placeholder** `hermes.example.com`
> (`overlays/production/kustomization.yaml`). Production will not serve traffic until that is
> replaced.

> **Note:** If using Route53, you can create an Alias record (A record) instead of CNAME, which avoids the extra DNS lookup.

---

## 6. Update Placeholders

### ECR registry URL

Replace `ACCOUNT_ID.dkr.ecr.us-east-1.amazonaws.com` in:

The account ID `471524413120` is hardcoded in **six** non-documentation files, not one.
Missing any of them leaves a deployment pointing at the wrong registry or assuming a role in
the wrong account:

| File | What to replace |
|------|-----------------|
| `deploy/kargo/warehouse.yaml` | `471524413120` in all image repoURLs |
| `deploy/kargo/stages/staging.yaml` | `471524413120` in the image references |
| `deploy/kargo/stages/production.yaml` | `471524413120` in the image references |
| `deploy/k8s/overlays/staging/images/kustomization.yaml` | `REGISTRY/hermes-*` image newName values |
| `deploy/k8s/overlays/production/images/kustomization.yaml` | `REGISTRY/hermes-*` image newName values |
| `infra/crossplane/provider/runtime-config.yaml` | the `eks.amazonaws.com/role-arn` annotation |

Get the actual ECR URL:
```bash
cd infra/terraform && terraform output -raw ecr_registry_url
```

> **Note:** `.github/workflows/cd.yml` does **not** need updating — it derives the ECR registry at runtime from the ECR login step.

### GitHub repository URL

These files already point at the real repository (`git@github.com:darylrobbins/hermes.git`);
there is no `OWNER` placeholder left to replace. The table is kept because a fork or a
rename still has to update all four in step:

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

### Application secrets

Secrets are split into two Secrets Manager entries **by who owns them**, because a secret
has a single value and whoever writes it owns all of it. Mixing the two is how a documented
"just update the value" procedure gets silently reverted on the next reconcile.

| Secret | Owner | Contents |
|---|---|---|
| `hermes/<env>/app` | You, seeded once | `jwt_secret`, `api_key_hmac_secret`, `centrifugo_token_secret`, `centrifugo_api_key` |
| `hermes/<env>/connection` | Crossplane | `database_url`, `redis_url`, `centrifugo_redis_address`, `centrifugo_redis_password` |

Crossplane creates both containers but writes a version only for `/connection`, so anything
you put in `/app` is permanent.

Seed `/app` before the first deploy. `centrifugo_token_secret` **must equal** `jwt_secret` —
Centrifugo validates the same Hermes-issued tokens the services do:

```bash
ENV=staging
JWT=$(openssl rand -base64 48)

aws secretsmanager put-secret-value --secret-id "hermes/$ENV/app" --secret-string "$(jq -n \
  --arg jwt "$JWT" \
  --arg hmac "$(openssl rand -base64 48)" \
  --arg capi "$(openssl rand -base64 32)" \
  '{jwt_secret: $jwt, api_key_hmac_secret: $hmac, centrifugo_token_secret: $jwt, centrifugo_api_key: $capi}')"
```

> **`/connection` must currently be seeded by hand as well.** The composition creates the
> container but does not yet assemble its contents — see the note in
> `infra/crossplane/compositions/aws/secrets.yaml` and finding 12. Read the values from the
> Crossplane-written connection secrets in `crossplane-system`, and note the two constraints
> the services enforce at startup (ADR 0005): `database_url` must carry `sslmode=require` or
> stricter, and `redis_url` must use `rediss://`. A service given a plaintext URL refuses to
> start rather than connecting in the clear.

```bash
DB=$(kubectl get secret -n crossplane-system hermes-database-conn -o jsonpath='{.data}')
# database_url:  postgres://<username>:<password>@<endpoint>:<port>/hermes?sslmode=require
# redis_url:     rediss://:<auth_token>@<endpoint>:6379/0
# centrifugo_redis_address / centrifugo_redis_password: the same endpoint and auth_token
```

### Webhook URLs

After deploying, update the webhook URLs in SSM Parameter Store:
```bash
aws ssm put-parameter --name "/hermes/staging/email_webhook_url" \
  --value "https://your-email-provider.com/send" --overwrite

aws ssm put-parameter --name "/hermes/staging/sms_webhook_url" \
  --value "https://your-sms-provider.com/send" --overwrite
```

### Let's Encrypt email

The ACME contact in `infra/scripts/bootstrap-cluster.sh` is already a real address. Change it
if you are deploying your own instance — Let's Encrypt sends expiry and revocation notices
there, so it should be a monitored mailbox rather than an individual.

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

Push any commit to `main` to trigger the CD pipeline. This builds all 10 images — the 9 services plus `hermes-migrate` — and pushes them to ECR. Kargo then detects the new images and promotes to staging.

```bash
gh run watch
```

---

## 8. Verify Deployment

### Check pods

```bash
kubectl get pods -n hermes
```

All 9 service deployments + NATS StatefulSet + Centrifugo should be Running. The 9 are
admin, send, dispatch, inbox, user, and the four workers (events, email, sms, inbox).

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

**Horizontal (pod count):** Four of the nine production services have an HPA — `hermes-admin`,
`hermes-dispatch`, `hermes-inbox` and `hermes-worker-email` (`overlays/production/hpa/`). The
other five run at the fixed replica count committed in `patches/replicas.yaml`. To adjust an
HPA-managed service:
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

Secrets live in AWS Secrets Manager and are synced to Kubernetes by External Secrets Operator
(1h refresh). They are split by ownership — see [Application secrets](#application-secrets) —
and **which secret a value lives in determines how you rotate it.**

**Rotating operator-owned values** (`jwt_secret`, `api_key_hmac_secret`,
`centrifugo_token_secret`, `centrifugo_api_key`) is a straightforward
`put-secret-value` against `hermes/<env>/app`, because Crossplane never writes there. Two
caveats: `centrifugo_token_secret` must keep matching `jwt_secret`, and rotating `jwt_secret`
invalidates every issued JWT. Note also that changing `HERMES_JWT_SECRET` does **not** rotate
the Hermes-internal signing key row — see [architecture.md](architecture.md).

**Rotating the database password is not an out-of-band operation, and the procedure
previously documented here did not work.** It told you to `aws rds modify-db-cluster` and then
overwrite the secret by hand. Crossplane owns that password: `compositions/aws/database.yaml`
sets `autoGeneratePassword: true` with a `masterPasswordSecretRef`, so it reconciles the
password back and the hand-written secret value is overwritten. The old instructions even
hedged — "Crossplane manages the secret resource, but you can update the value" — which is
precisely the trap.

Rotate it through Crossplane instead, by causing it to regenerate and propagate. The exact
mechanism depends on the provider version and **has not been verified against a running
cluster**, so treat the following as the shape of the procedure rather than a tested recipe,
and rehearse it in staging first:

```bash
# 1. Trigger regeneration of the master password via the Crossplane-managed resource,
#    NOT via `aws rds modify-db-cluster`, which will be reverted.
# 2. Wait for the claim to report Ready and for the connection secret in
#    crossplane-system to carry the new password.
# 3. Re-derive hermes/<env>/connection from it — until the composition assembles that
#    secret automatically (finding 12), this step is manual.

# Force ESO to resync rather than waiting up to an hour.
kubectl annotate externalsecret hermes-secrets -n hermes force-sync=$(date +%s) --overwrite

# Restart to pick up the new value.
kubectl rollout restart deployment -n hermes
```

**Rotate JWT secret:** Same pattern — update Secrets Manager, force ESO resync, restart pods. Existing JWTs will be invalidated.

**Rotate Valkey auth token:** Update ElastiCache auth token, update Secrets Manager, force resync, restart.

### Database Migrations

Migrations are defined as a K8s Job in `deploy/k8s/base/migration-job.yaml`, which runs
`cmd/migrate` with the database URL from Secrets Manager.

> **They do not currently run automatically.** The `argocd.argoproj.io/hook: PreSync` and
> `hook-delete-policy` annotations on that Job are commented out, so ArgoCD applies it as an
> ordinary resource rather than a sync hook. A `Job`'s pod template is immutable, so the
> second promotion that changes the image tag fails to apply. Until the hook is re-enabled,
> run migrations manually using the command below. See finding 11 in the
> [2026-07-27 architecture review](reviews/2026-07-27-architecture-review.md).

**Run migrations manually:**
```bash
kubectl delete job hermes-migrate -n hermes --ignore-not-found
kubectl apply -f deploy/k8s/base/migration-job.yaml
```

**Rolling back a migration is not automated.** `cmd/migrate` takes only `-database-url` and
`-migrations-path`, and calls `Up()` unconditionally — it has no direction, step count, or
`down` subcommand, and the image built from it contains a single binary at `/service`. The
`.down.sql` files in `migrations/` are real and paired with every `.up.sql`, but nothing in
this repository applies them.

To roll back, run the down migration yourself against the database with a golang-migrate CLI
installed separately, or apply the `.down.sql` by hand inside a transaction:

```bash
# Inspect what would be reverted first.
cat migrations/000017_rename_tenant_to_organization.down.sql

# Then apply it deliberately, against a database you have backed up.
psql "$DATABASE_URL" -1 -f migrations/000017_rename_tenant_to_organization.down.sql
```

Applying a `.down.sql` by hand does not update the `schema_migrations` version, so correct it
in the same transaction or the next `Up()` will skip or re-run the wrong step.

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

> **Important:** Only upgrade one minor version at a time (e.g., 1.35 → 1.36). The current
> default is `1.35` (`infra/terraform/variables.tf`). Check the
> [EKS release calendar](https://docs.aws.amazon.com/eks/latest/userguide/kubernetes-versions.html)
> for deprecations.

### Monitoring and Debugging

**View logs:**
```bash
# Specific service
kubectl logs -n hermes -l app.kubernetes.io/name=hermes-admin --tail=100 -f

# All services.
# NOTE: this selector currently matches NOTHING. The kustomize `labels:` transformer in
# deploy/k8s/base/kustomization.yaml runs with includeSelectors: false and no
# includeTemplates, so app.kubernetes.io/part-of lands on resource metadata but never on
# pod templates. See finding 47 in the architecture review. Until that is fixed, select by
# component instead:
kubectl logs -n hermes -l app.kubernetes.io/component=api --tail=50

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
