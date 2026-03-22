#!/usr/bin/env bash
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
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

if [ $# -lt 2 ]; then
  echo "Usage: $0 <environment> <command> [extra args...]"
  echo "  environment: staging | production"
  echo "  command:     init | plan | apply | destroy | ..."
  exit 1
fi

ENVIRONMENT="$1"
COMMAND="$2"
shift 2

if [[ "${ENVIRONMENT}" != "staging" && "${ENVIRONMENT}" != "production" ]]; then
  echo "Error: environment must be 'staging' or 'production', got '${ENVIRONMENT}'"
  exit 1
fi

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

terraform "${COMMAND}" -var-file="${TFVARS_FILE}" "$@"
