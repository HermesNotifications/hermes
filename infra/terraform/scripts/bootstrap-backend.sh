#!/usr/bin/env bash
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.
set -euo pipefail

REGION="${1:-us-east-1}"
ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"
BUCKET_NAME="hermes-terraform-state-${ACCOUNT_ID}"

echo "Bootstrapping Terraform backend in region: ${REGION}"

# ------------------------------------------------------------------------------
# S3 Bucket for state (Terraform 1.10+ uses native S3 locking via use_lockfile)
# ------------------------------------------------------------------------------
echo "Creating S3 bucket: ${BUCKET_NAME}..."

if aws s3api head-bucket --bucket "${BUCKET_NAME}" 2>/dev/null; then
  echo "Bucket ${BUCKET_NAME} already exists, skipping creation."
else
  if [ "${REGION}" = "us-east-1" ]; then
    aws s3api create-bucket \
      --bucket "${BUCKET_NAME}" \
      --region "${REGION}"
  else
    aws s3api create-bucket \
      --bucket "${BUCKET_NAME}" \
      --region "${REGION}" \
      --create-bucket-configuration "LocationConstraint=${REGION}"
  fi
  echo "Bucket created."
fi

echo "Enabling versioning..."
aws s3api put-bucket-versioning \
  --bucket "${BUCKET_NAME}" \
  --versioning-configuration Status=Enabled

echo "Enabling server-side encryption..."
aws s3api put-bucket-encryption \
  --bucket "${BUCKET_NAME}" \
  --server-side-encryption-configuration '{
    "Rules": [{
      "ApplyServerSideEncryptionByDefault": {
        "SSEAlgorithm": "AES256"
      },
      "BucketKeyEnabled": true
    }]
  }'

echo "Blocking public access..."
aws s3api put-public-access-block \
  --bucket "${BUCKET_NAME}" \
  --public-access-block-configuration \
    "BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true"

echo ""
echo "Terraform backend bootstrap complete!"
echo ""
echo "Initialize Terraform with:"
echo "  ENVIRONMENT=staging  # or production"
echo "  terraform init \\"
echo "    -backend-config=\"bucket=${BUCKET_NAME}\" \\"
echo "    -backend-config=\"key=hermes/\${ENVIRONMENT}/terraform.tfstate\" \\"
echo "    -backend-config=\"region=${REGION}\" \\"
echo "    -backend-config=\"use_lockfile=true\" \\"
echo "    -backend-config=\"encrypt=true\""
