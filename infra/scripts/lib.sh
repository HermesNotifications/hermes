#!/usr/bin/env bash
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.
#
# Pure helpers shared by the infra scripts. Deliberately free of kubectl, aws and
# network calls so that infra/scripts/test-lib.sh can exercise them anywhere.
#
# Source it, do not execute it:
#   source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# hermes_urlencode <string>
#
# Percent-encodes a string for use in a URL. Needed because connection passwords land in
# the userinfo section of a URL, where an unescaped '@' or '/' silently reparses the URL
# into a different host — a failure that looks like a wrong password rather than a
# quoting bug. jq's @uri leaves only the unreserved set alone.
hermes_urlencode() {
  jq -rn --arg s "$1" '$s | @uri'
}

# hermes_database_url <user> <password> <host> <port> <dbname>
#
# ADR 0005: sslmode=require is not optional. config.Validate refuses to start a service
# whose HERMES_DATABASE_URL has sslmode absent or set to disable, so a URL built without
# it produces a CrashLoopBackOff, not a plaintext connection.
hermes_database_url() {
  local user="$1" password="$2" host="$3" port="$4" dbname="$5"
  printf 'postgres://%s:%s@%s:%s/%s?sslmode=require' \
    "$(hermes_urlencode "$user")" \
    "$(hermes_urlencode "$password")" \
    "$host" "$port" "$dbname"
}

# hermes_redis_url <auth_token> <host> <port>
#
# rediss:// rather than redis://. config.Validate checks the scheme prefix literally.
hermes_redis_url() {
  local token="$1" host="$2" port="$3"
  printf 'rediss://:%s@%s:%s' "$(hermes_urlencode "$token")" "$host" "$port"
}

# hermes_require_role_arn <label> <arn> <expected_role_name>
#
# Finding 7. The Crossplane DeploymentRuntimeConfig hardcoded
# arn:aws:iam::<acct>:role/hermes-staging-crossplane and was applied to every cluster, so
# production authenticated as staging. Nothing detected it, because assuming the wrong
# role of the right shape works perfectly — it just operates on the wrong environment's
# resources.
#
# Every IAM role this repo's Terraform creates for a cluster is named
# "<cluster-name>-<purpose>", so the cluster being bootstrapped fully determines the role
# name, and a mismatch can be caught before anything is applied.
hermes_require_role_arn() {
  local label="$1" arn="$2" want="$3"
  # A path-qualified ARN (role/some/path/NAME) is legitimate; a longer name that merely
  # starts with the expected one (NAME-readonly) is not.
  if [[ ! "$arn" =~ ^arn:aws[a-z-]*:iam::[0-9]{12}:role/(.*/)?"$want"$ ]]; then
    echo "ERROR: the ${label} role ARN does not belong to this cluster." >&2
    echo "  given:    ${arn:-<empty>}" >&2
    echo "  expected: arn:aws:iam::<account-id>:role/${want}" >&2
    echo >&2
    echo "  This is finding 7: bootstrapping one environment with another's role ARN" >&2
    echo "  succeeds silently and then operates on the wrong environment's resources." >&2
    echo "  Get the right value with:" >&2
    echo "    terraform -chdir=infra/terraform output -raw crossplane_role_arn" >&2
    return 1
  fi
  return 0
}

# hermes_connection_json <existing_json> <db_user> <db_password> <db_host> <db_port> \
#                        <db_name> <cache_token> <cache_host> <cache_port>
#
# Builds the JSON blob that ESO reads from hermes/<env>/connection, MERGED over whatever
# is already there. Merging rather than replacing matters: centrifugo_nats_url lives in
# the same secret and is not derivable from the database and cache claims (it comes from
# `go run ./cmd/natskeys`, ADR 0005 phase 4). A wholesale put would silently delete it.
#
# centrifugo_redis_password is emitted RAW. Centrifugo reads it as a standalone env var,
# not as URL userinfo, so percent-encoding it there would corrupt the password — while
# the same token inside redis_url must be encoded. They are deliberately different.
hermes_connection_json() {
  local existing="$1" db_user="$2" db_pass="$3" db_host="$4" db_port="$5" db_name="$6"
  local cache_token="$7" cache_host="$8" cache_port="$9"

  jq -n \
    --argjson existing "${existing:-\{\}}" \
    --arg database_url "$(hermes_database_url "$db_user" "$db_pass" "$db_host" "$db_port" "$db_name")" \
    --arg redis_url "$(hermes_redis_url "$cache_token" "$cache_host" "$cache_port")" \
    --arg centrifugo_redis_address "${cache_host}:${cache_port}" \
    --arg centrifugo_redis_password "$cache_token" \
    '$existing + {
       database_url: $database_url,
       redis_url: $redis_url,
       centrifugo_redis_address: $centrifugo_redis_address,
       centrifugo_redis_password: $centrifugo_redis_password
     }'
}
