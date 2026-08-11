#!/usr/bin/env bash
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.
# Bootstrap an EKS cluster with required platform components.
# Usage: ./bootstrap-cluster.sh <cluster-name> [region] [eso-role-arn] [kargo-role-arn] [crossplane-role-arn]
# Example: ./bootstrap-cluster.sh hermes-staging us-east-1 arn:aws:iam::123:role/eso arn:aws:iam::123:role/kargo arn:aws:iam::123:role/crossplane

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${HERE}/lib.sh"

CLUSTER="${1:?Usage: $0 <cluster-name> [region] [eso-role-arn] [kargo-role-arn] [crossplane-role-arn]}"
REGION="${2:-us-east-1}"
ESO_ROLE_ARN="${3:?Provide the External Secrets Operator IAM role ARN}"
KARGO_ROLE_ARN="${4:?Provide the Kargo controller IAM role ARN}"
CROSSPLANE_ROLE_ARN="${5:?Provide the Crossplane AWS provider IAM role ARN}"

# Finding 7. CROSSPLANE_ROLE_ARN was a required argument that this script then never
# used — operators were told to pass a value that was silently discarded, while the
# Crossplane providers picked up a hardcoded staging ARN from a checked-in manifest
# instead. Production therefore authenticated as staging, successfully, forever.
#
# All three ARNs are now checked against the cluster being bootstrapped before anything
# is installed. Terraform names every one of these roles "<cluster-name>-<purpose>", so
# passing another environment's ARN is detectable here and is caught in under a second,
# rather than surfacing as Crossplane quietly reconciling the wrong environment's
# database.
echo "==> Checking the supplied IAM role ARNs belong to ${CLUSTER}"
hermes_require_role_arn "External Secrets Operator" "$ESO_ROLE_ARN" "${CLUSTER}-external-secrets"
hermes_require_role_arn "Kargo controller" "$KARGO_ROLE_ARN" "${CLUSTER}-kargo-controller"
hermes_require_role_arn "Crossplane AWS provider" "$CROSSPLANE_ROLE_ARN" "${CLUSTER}-crossplane"
echo "    all three match"

echo "==> Configuring kubectl for cluster: $CLUSTER"
aws eks update-kubeconfig --name "$CLUSTER" --region "$REGION"

echo "==> Adding Helm repositories"
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo add jetstack https://charts.jetstack.io
helm repo add external-secrets https://charts.external-secrets.io
helm repo add argo https://argoproj.github.io/argo-helm
helm repo add crossplane-stable https://charts.crossplane.io/stable
helm repo update

echo "==> Installing NGINX Ingress Controller"
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx --create-namespace \
  --set controller.service.type=LoadBalancer \
  --set "controller.service.annotations.service\.beta\.kubernetes\.io/aws-load-balancer-type=nlb" \
  --set "controller.service.annotations.service\.beta\.kubernetes\.io/aws-load-balancer-scheme=internet-facing" \
  --wait

echo "==> Installing cert-manager"
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace \
  --set crds.enabled=true \
  --wait

echo "==> Creating Let's Encrypt ClusterIssuer"
kubectl apply -f - <<'EOF'
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: daryl@darylrobbins.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
      - http01:
          ingress:
            class: nginx
EOF

echo "==> Installing External Secrets Operator"
helm upgrade --install external-secrets external-secrets/external-secrets \
  --namespace external-secrets --create-namespace \
  --set serviceAccount.name=external-secrets-sa \
  --set "serviceAccount.annotations.eks\.amazonaws\.com/role-arn=$ESO_ROLE_ARN" \
  --wait

echo "==> Installing Argo Rollouts CRDs (required for Kargo verification)"
helm upgrade --install argo-rollouts argo/argo-rollouts \
  --namespace argo-rollouts --create-namespace \
  --set controller.replicas=0 \
  --set dashboard.enabled=false \
  --wait

echo "==> Installing ArgoCD"
helm upgrade --install argocd argo/argo-cd \
  --namespace argocd --create-namespace \
  --set 'server.extraArgs={--insecure}' \
  --wait
echo "    ArgoCD admin password: $(kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)"
echo "    Access via: kubectl port-forward svc/argocd-server -n argocd 8080:443"

echo "==> Installing Kargo"
KARGO_ADMIN_PASSWORD="$(head -c 32 /dev/urandom | base64 | tr -d '/+=' | head -c 24)"
KARGO_PASSWORD_HASH="$(htpasswd -nbBC 10 "" "${KARGO_ADMIN_PASSWORD}" | cut -d: -f2)"
helm upgrade --install kargo oci://ghcr.io/akuity/kargo-charts/kargo \
  --namespace kargo --create-namespace \
  --set api.adminAccount.enabled=true \
  --set "api.adminAccount.passwordHash=${KARGO_PASSWORD_HASH}" \
  --set api.adminAccount.tokenSigningKey="$(head -c 32 /dev/urandom | base64)" \
  --set "controller.serviceAccount.annotations.eks\.amazonaws\.com/role-arn=${KARGO_ROLE_ARN}" \
  --wait
echo "    Kargo admin password: ${KARGO_ADMIN_PASSWORD}"
echo "    Access Kargo via: kubectl port-forward svc/kargo-api -n kargo 8443:443"

echo "==> Installing Crossplane"
# NOTE: this install is unpinned. function-extra-resources v0.3.0, which any future
# automation of the connection secret depends on, returns nothing at all — silently — on
# Crossplane older than 1.20. Pinning --version is a follow-up worth doing before that
# automation is attempted.
helm upgrade --install crossplane crossplane-stable/crossplane \
  --namespace crossplane-system --create-namespace \
  --wait

# Finding 7. This DeploymentRuntimeConfig used to be a checked-in manifest,
# infra/crossplane/provider/runtime-config.yaml, carrying a literal
# arn:aws:iam::<acct>:role/hermes-staging-crossplane. deploy/argocd/crossplane-infra.yaml
# syncs infra/crossplane recursively to EVERY cluster, so that one staging ARN was applied
# to production too and the production providers authenticated as staging.
#
# It is now generated here from the ARN the operator passes, which is cluster-specific by
# construction and has just been checked against $CLUSTER above. This mirrors what the
# script already does for the EnvironmentConfig below, and — because the object is created
# by kubectl rather than tracked by ArgoCD — ArgoCD will not overwrite or prune it.
#
# It must exist BEFORE aws-provider.yaml, whose Providers all reference
# runtimeConfigRef.name = default.
echo "==> Creating Crossplane DeploymentRuntimeConfig (IRSA) for ${CLUSTER}"
kubectl apply -f - <<RUNTIMEEOF
apiVersion: pkg.crossplane.io/v1beta1
kind: DeploymentRuntimeConfig
metadata:
  name: default
spec:
  serviceAccountTemplate:
    metadata:
      annotations:
        eks.amazonaws.com/role-arn: ${CROSSPLANE_ROLE_ARN}
RUNTIMEEOF

echo "==> Installing Crossplane functions and AWS provider"
kubectl apply -f infra/crossplane/compositions/aws/functions.yaml
kubectl apply -f infra/crossplane/provider/aws-provider.yaml
echo "    Waiting for provider to become healthy..."
kubectl wait provider.pkg --all --for=condition=Healthy --timeout=300s
echo "    Waiting for functions to become healthy..."
kubectl wait function.pkg --all --for=condition=Healthy --timeout=300s

echo "==> Configuring Crossplane AWS ProviderConfig (IRSA)"
# The provider-config references IRSA — the provider pods pick up the role
# via the service account annotation set by the provider family.
kubectl apply -f infra/crossplane/provider/provider-config.yaml

echo "==> Creating EnvironmentConfig with VPC context from Terraform outputs"
# Query Terraform outputs for VPC context. Requires terraform CLI and access
# to the state backend. Run from the repo root.
VPC_ID=$(cd infra/terraform && terraform output -raw vpc_id)
SUBNET_IDS_JSON=$(cd infra/terraform && terraform output -json private_subnet_ids)
SUBNET_A=$(echo "$SUBNET_IDS_JSON" | jq -r '.[0]')
SUBNET_B=$(echo "$SUBNET_IDS_JSON" | jq -r '.[1]')
SUBNET_C=$(echo "$SUBNET_IDS_JSON" | jq -r '.[2] // empty')
NODE_SG_ID=$(cd infra/terraform && terraform output -raw node_security_group_id)

kubectl apply -f - <<ENVEOF
apiVersion: apiextensions.crossplane.io/v1beta1
kind: EnvironmentConfig
metadata:
  name: hermes-vpc-context
data:
  vpcId: "$VPC_ID"
  privateSubnetIdA: "$SUBNET_A"
  privateSubnetIdB: "$SUBNET_B"
  privateSubnetIdC: "${SUBNET_C:-}"
  nodeSecurityGroupId: "$NODE_SG_ID"
  environment: "${CLUSTER##hermes-}"
  region: "$REGION"
ENVEOF

echo "==> Applying Crossplane XRDs and compositions"
kubectl apply -f infra/crossplane/xrds/
kubectl apply -f infra/crossplane/compositions/aws/

ENVIRONMENT="${CLUSTER##hermes-}"

echo "==> Bootstrap complete for cluster: $CLUSTER"
echo ""
echo "Next steps:"
echo "  1. Configure DNS to point to the NLB created by ingress-nginx"
echo "  2. Apply ArgoCD Applications: kubectl apply -f deploy/argocd/"
echo "  3. Apply Kargo resources — BOTH commands, in this order:"
echo "       kubectl apply -f deploy/kargo/project.yaml"
echo "       kubectl apply -R -f deploy/kargo/"
echo "     -R because deploy/kargo/ has subdirectories (analysis/, stages/) and a bare"
echo "     'kubectl apply -f <dir>' is NOT recursive — it would silently skip the"
echo "     promotion stages and the health-check AnalysisTemplate and its RBAC, so the"
echo "     production promotion gate would 403 with nothing saying why."
echo "     project.yaml first because the Kargo Project owns the hermes namespace that"
echo "     everything under analysis/ and stages/ is namespaced into, and a recursive"
echo "     apply walks analysis/ before project.yaml alphabetically."
echo "  4. Crossplane claims are managed by ArgoCD — they deploy automatically"
echo ""
echo "###############################################################################"
echo "# REQUIRED MANUAL STEP - THE CLUSTER WILL NOT START WITHOUT IT (finding 12)"
echo "###############################################################################"
echo ""
echo "  The Crossplane secrets composition creates hermes/${ENVIRONMENT}/connection and"
echo "  hermes/${ENVIRONMENT}/app as EMPTY containers. It writes no values into either."
echo "  Every ExternalSecret in deploy/k8s/overlays/${ENVIRONMENT}/ reads keys out of"
echo "  them, so until they are seeded every one fails to resolve, the hermes-secrets"
echo "  Secret is never created, and every pod stays in CreateContainerConfigError."
echo ""
echo "  Nothing else in this repository will tell you that. Seed them:"
echo ""
echo "    a) Wait for the database and cache claims to report Ready:"
echo "         kubectl -n hermes get hermesdatabaseclaim,hermescacheclaim"
echo ""
echo "    b) Derive and write the connection values:"
echo "         ./infra/scripts/seed-connection-secret.sh ${ENVIRONMENT} ${REGION} --dry-run"
echo "         ./infra/scripts/seed-connection-secret.sh ${ENVIRONMENT} ${REGION}"
echo ""
echo "    c) Seed the values no infrastructure can derive, by hand:"
echo "         hermes/${ENVIRONMENT}/app        jwt_secret, api_key_hmac_secret,"
echo "                                          centrifugo_token_secret, centrifugo_api_key"
echo "         hermes/${ENVIRONMENT}/nats-nkeys  go run ./cmd/natskeys -format json"
echo "         centrifugo_nats_url, into hermes/${ENVIRONMENT}/connection, same source"
echo ""
echo "###############################################################################"
