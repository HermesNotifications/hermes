#!/usr/bin/env bash
# Bootstrap an EKS cluster with required platform components.
# Usage: ./bootstrap-cluster.sh <cluster-name> [region] [eso-role-arn]
# Example: ./bootstrap-cluster.sh hermes-staging us-east-1 arn:aws:iam::123:role/...

set -euo pipefail

CLUSTER="${1:?Usage: $0 <cluster-name> [region] [eso-role-arn]}"
REGION="${2:-us-east-1}"
ESO_ROLE_ARN="${3:?Provide the External Secrets Operator IAM role ARN}"

echo "==> Configuring kubectl for cluster: $CLUSTER"
aws eks update-kubeconfig --name "$CLUSTER" --region "$REGION"

echo "==> Adding Helm repositories"
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo add jetstack https://charts.jetstack.io
helm repo add external-secrets https://charts.external-secrets.io
helm repo add argo https://argoproj.github.io/argo-helm
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
    email: ops@example.com
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
  --wait
echo "    Kargo admin password: ${KARGO_ADMIN_PASSWORD}"
echo "    Access Kargo via: kubectl port-forward svc/kargo-api -n kargo 8443:443"

echo "==> Bootstrap complete for cluster: $CLUSTER"
echo ""
echo "Next steps:"
echo "  1. Configure DNS to point to the NLB created by ingress-nginx"
echo "  2. Apply ArgoCD Applications: kubectl apply -f deploy/argocd/"
echo "  3. Apply Kargo resources: kubectl apply -f deploy/kargo/"
