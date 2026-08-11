#!/usr/bin/env bash
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.
#
# Finding 12. Populate hermes/<env>/connection in AWS Secrets Manager from the connection
# secrets that the Crossplane database and cache claims write into the cluster.
#
# WHY THIS SCRIPT EXISTS AND IS NOT A COMPOSITION
#
# The HermesSecretsBundle XRD promises seven connectionSecretKeys including database_url
# and redis_url, and every ExternalSecret in deploy/k8s/overlays/*/external-secrets.yaml
# reads them from hermes/<env>/connection. Nothing writes them. The composition creates
# the secret as an empty container and stops, so the whole read path fails to resolve
# until a human fills it in — and until now nothing in the repository said so.
#
# Automating it inside the composition IS possible; the route is recorded in full in
# infra/crossplane/compositions/aws/secrets.yaml. It was not taken in this change because
# it could not be verified here (no cluster, and `crossplane render` needs one or a
# container runtime), because the installed Crossplane version is unpinned and the
# required function silently returns nothing on anything older than 1.20, and because
# Crossplane owning this secret makes it unsafe to hand-edit ever again.
#
# This script is the honest version: an explicit, idempotent, re-runnable operator step.
#
# USAGE
#   ./infra/scripts/seed-connection-secret.sh <environment> [region] [--dry-run]
#   ./infra/scripts/seed-connection-secret.sh staging us-east-1 --dry-run
#
# Re-run it after anything that rotates the Aurora password or the ElastiCache auth token.
# It MERGES over the existing secret rather than replacing it, so centrifugo_nats_url —
# which comes from `go run ./cmd/natskeys`, not from these claims — survives.
#
# WHAT IT DOES NOT DO
#   - hermes/<env>/app       (jwt_secret, api_key_hmac_secret, centrifugo_token_secret,
#                             centrifugo_api_key) — none are derivable from infrastructure
#   - hermes/<env>/nats-nkeys — see `go run ./cmd/natskeys -format json`
#   - centrifugo_nats_url    — same source; preserved here, never written here

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${HERE}/lib.sh"

ENVIRONMENT="${1:?Usage: $0 <environment> [region] [--dry-run]}"
REGION="${2:-us-east-1}"
DRY_RUN="${3:-}"

NAMESPACE="hermes"
DB_SECRET="hermes-database-conn"
CACHE_SECRET="hermes-cache-conn"
TARGET="hermes/${ENVIRONMENT}/connection"

k8s_secret_key() { # <secret-name> <key>
  local out
  if ! out="$(kubectl -n "$NAMESPACE" get secret "$1" -o "jsonpath={.data.$2}" 2>/dev/null)"; then
    echo "ERROR: cannot read secret $1 in namespace $NAMESPACE." >&2
    echo "  Is the Crossplane claim reconciled? Check:" >&2
    echo "    kubectl -n $NAMESPACE get hermesdatabaseclaim,hermescacheclaim" >&2
    exit 1
  fi
  if [[ -z "$out" ]]; then
    echo "ERROR: secret $1 exists but has no key '$2'." >&2
    echo "  The claim may still be provisioning — Crossplane writes connection details" >&2
    echo "  only once the underlying resource reports Ready. Check:" >&2
    echo "    kubectl -n $NAMESPACE describe hermesdatabaseclaim hermes-database" >&2
    exit 1
  fi
  printf '%s' "$out" | base64 -d
}

echo "==> Reading Crossplane connection secrets from namespace ${NAMESPACE}"
DB_USER="$(k8s_secret_key "$DB_SECRET" username)"
DB_PASS="$(k8s_secret_key "$DB_SECRET" password)"
DB_HOST="$(k8s_secret_key "$DB_SECRET" endpoint)"
DB_PORT="$(k8s_secret_key "$DB_SECRET" port)"
CACHE_TOKEN="$(k8s_secret_key "$CACHE_SECRET" auth_token)"
CACHE_HOST="$(k8s_secret_key "$CACHE_SECRET" endpoint)"
CACHE_PORT="$(k8s_secret_key "$CACHE_SECRET" port)"

echo "    database endpoint: ${DB_HOST}:${DB_PORT}"
echo "    cache endpoint:    ${CACHE_HOST}:${CACHE_PORT}"

echo "==> Reading any existing value of ${TARGET} so nothing already there is lost"
EXISTING='{}'
if EXISTING_RAW="$(aws secretsmanager get-secret-value \
  --secret-id "$TARGET" --region "$REGION" \
  --query SecretString --output text 2>/dev/null)"; then
  if printf '%s' "$EXISTING_RAW" | jq -e . >/dev/null 2>&1; then
    EXISTING="$EXISTING_RAW"
    echo "    merging over $(printf '%s' "$EXISTING" | jq -r 'keys | join(", ")')"
  else
    echo "    existing value is not JSON; refusing to merge over it." >&2
    echo "    Inspect it by hand before re-running." >&2
    exit 1
  fi
else
  echo "    no existing value (or the secret shell has no version yet)"
fi

PAYLOAD="$(hermes_connection_json "$EXISTING" \
  "$DB_USER" "$DB_PASS" "$DB_HOST" "$DB_PORT" hermes \
  "$CACHE_TOKEN" "$CACHE_HOST" "$CACHE_PORT")"

if [[ "$DRY_RUN" == "--dry-run" ]]; then
  echo "==> --dry-run: the following WOULD be written to ${TARGET}"
  echo "    (this prints live credentials to your terminal)"
  printf '%s\n' "$PAYLOAD" | jq .
  exit 0
fi

echo "==> Writing ${TARGET}"
aws secretsmanager put-secret-value \
  --secret-id "$TARGET" \
  --region "$REGION" \
  --secret-string "$PAYLOAD" \
  --output text --query VersionId

echo "==> Done. ExternalSecrets refresh hourly; to force one now:"
echo "    kubectl -n ${NAMESPACE} annotate externalsecret hermes-secrets force-sync=\$(date +%s) --overwrite"
echo
echo "    Still not seeded by this script, and still required:"
echo "      hermes/${ENVIRONMENT}/app        jwt_secret, api_key_hmac_secret,"
echo "                                        centrifugo_token_secret, centrifugo_api_key"
echo "      hermes/${ENVIRONMENT}/nats-nkeys  go run ./cmd/natskeys -format json"
echo "      centrifugo_nats_url in ${TARGET}  same source as the nkeys"
