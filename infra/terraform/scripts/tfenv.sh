#!/usr/bin/env bash
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.
#
# Terraform wrapper that handles backend init and environment selection.
#
# Usage:
#   ./scripts/tfenv.sh <environment> <command> [extra args...]
#
# Examples:
#   ./scripts/tfenv.sh staging plan
#   ./scripts/tfenv.sh staging apply
#   ./scripts/tfenv.sh production plan -target=module.eks
#   ./scripts/tfenv.sh staging destroy
#   ./scripts/tfenv.sh loadtest output -raw workload_availability_zone
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

if [ $# -lt 2 ]; then
  echo "Usage: $0 <environment> <command> [extra args...]"
  echo "  environment: staging | production | loadtest"
  echo "  command:     init | plan | apply | destroy | ..."
  exit 1
fi

ENVIRONMENT="$1"
COMMAND="$2"
shift 2

case "${ENVIRONMENT}" in
staging | production | loadtest) ;;
*)
  echo "Error: environment must be 'staging', 'production' or 'loadtest', got '${ENVIRONMENT}'"
  exit 1
  ;;
esac

TFVARS_FILE="${TF_DIR}/environments/${ENVIRONMENT}.tfvars"
if [ ! -f "${TFVARS_FILE}" ]; then
  echo "Error: ${TFVARS_FILE} not found"
  exit 1
fi

# Read region from the tfvars file (handles both quoted and unquoted values)
REGION="$(grep '^aws_region' "${TFVARS_FILE}" | sed 's/.*= *"\{0,1\}\([a-z0-9-]*\)"\{0,1\}/\1/' | tr -d '[:space:]')"
if [ -z "${REGION}" ]; then
  echo "Error: aws_region not found in ${TFVARS_FILE}"
  exit 1
fi

ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text --region "${REGION}")"
BUCKET_NAME="hermes-terraform-state-${ACCOUNT_ID}"

export AWS_DEFAULT_REGION="${REGION}"

cd "${TF_DIR}"

# Always ensure backend is initialized for the right environment
terraform init -reconfigure \
  -backend-config="bucket=${BUCKET_NAME}" \
  -backend-config="key=hermes/${ENVIRONMENT}/terraform.tfstate" \
  -backend-config="region=${REGION}" \
  -backend-config="use_lockfile=true" \
  -backend-config="encrypt=true"

if [ "${COMMAND}" = "init" ]; then
  # Already done
  exit 0
fi

# Restore secrets pending deletion — Secrets Manager enforces a recovery window
# that blocks re-creation. Restore first so Terraform can manage the resource.
reconcile_secret() {
  local secret_name="$1"
  local tf_address="$2"

  local secret_json
  secret_json="$(aws secretsmanager describe-secret --secret-id "${secret_name}" 2>/dev/null)" || return 0

  # Restore if pending deletion
  local deleted_date
  deleted_date="$(echo "${secret_json}" | jq -r '.DeletedDate // empty')"
  if [ -n "${deleted_date}" ]; then
    echo "Restoring secret '${secret_name}' from pending deletion..."
    aws secretsmanager restore-secret --secret-id "${secret_name}"
  fi

  # Import into state if not already tracked
  if ! terraform state show "${tf_address}" >/dev/null 2>&1; then
    local secret_arn
    secret_arn="$(echo "${secret_json}" | jq -r '.ARN')"
    echo "Importing existing secret '${secret_name}' into Terraform state..."
    terraform import -var-file="${TFVARS_FILE}" "${tf_address}" "${secret_arn}"
  fi
}

if [ "${COMMAND}" = "apply" ] || [ "${COMMAND}" = "plan" ]; then
  reconcile_secret "hermes/${ENVIRONMENT}" "module.secrets.aws_secretsmanager_secret.hermes"
fi

terraform "${COMMAND}" -var-file="${TFVARS_FILE}" "$@"
