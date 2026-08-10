#!/usr/bin/env bash
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.
#
# Tests for infra/scripts/lib.sh. No cluster, no AWS, no network — these are pure
# functions and this runs anywhere bash and jq exist.
#
#   ./infra/scripts/test-lib.sh

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${HERE}/lib.sh"

PASS=0
FAIL=0

expect_eq() { # <label> <expected> <actual>
  if [[ "$2" == "$3" ]]; then
    PASS=$((PASS + 1))
    echo "  ok   $1"
  else
    FAIL=$((FAIL + 1))
    echo "  FAIL $1"
    echo "         expected: $2"
    echo "         actual:   $3"
  fi
}

expect_ok() { # <label> <command...>
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    PASS=$((PASS + 1))
    echo "  ok   $label"
  else
    FAIL=$((FAIL + 1))
    echo "  FAIL $label (expected success, got exit $?)"
  fi
}

expect_fail() { # <label> <command...>
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    FAIL=$((FAIL + 1))
    echo "  FAIL $label (expected failure, but it succeeded)"
  else
    PASS=$((PASS + 1))
    echo "  ok   $label"
  fi
}

echo "hermes_urlencode"
expect_eq "leaves an alphanumeric token alone" \
  "abc123XYZ" "$(hermes_urlencode 'abc123XYZ')"
expect_eq "escapes the characters that break userinfo" \
  "a%3Ab%40c%2Fd%3Fe%23f%25g" "$(hermes_urlencode 'a:b@c/d?e#f%g')"

echo
echo "hermes_database_url"
expect_eq "builds a URL carrying sslmode=require" \
  "postgres://hermes:pw@db.example.com:5432/hermes?sslmode=require" \
  "$(hermes_database_url hermes pw db.example.com 5432 hermes)"
# ADR 0005: config.Validate rejects a database URL whose sslmode is absent or 'disable'.
# A password containing '?' could otherwise terminate the path and swallow the query.
expect_eq "a password containing ? and @ cannot break the query string" \
  "postgres://hermes:p%3Fw%40x@db.example.com:5432/hermes?sslmode=require" \
  "$(hermes_database_url hermes 'p?w@x' db.example.com 5432 hermes)"

echo
echo "hermes_redis_url"
expect_eq "uses rediss:// because config.Validate demands TLS" \
  "rediss://:tok@cache.example.com:6379" \
  "$(hermes_redis_url tok cache.example.com 6379)"
expect_eq "percent-encodes the auth token" \
  "rediss://:a%2Fb%3Ac@cache.example.com:6379" \
  "$(hermes_redis_url 'a/b:c' cache.example.com 6379)"

echo
echo "hermes_require_role_arn"
expect_ok "accepts the role belonging to this cluster" \
  hermes_require_role_arn Crossplane \
  "arn:aws:iam::111122223333:role/hermes-production-crossplane" \
  hermes-production-crossplane
# Finding 7: this is the exact mistake — production bootstrapped with staging's ARN.
expect_fail "rejects another environment's role" \
  hermes_require_role_arn Crossplane \
  "arn:aws:iam::111122223333:role/hermes-staging-crossplane" \
  hermes-production-crossplane
expect_fail "rejects the right name with the wrong suffix" \
  hermes_require_role_arn Crossplane \
  "arn:aws:iam::111122223333:role/hermes-production-kargo-controller" \
  hermes-production-crossplane
expect_fail "rejects something that is not a role ARN at all" \
  hermes_require_role_arn Crossplane \
  "hermes-production-crossplane" \
  hermes-production-crossplane
expect_fail "rejects an empty ARN" \
  hermes_require_role_arn Crossplane "" hermes-production-crossplane
expect_fail "rejects a prefix match that is not the whole role name" \
  hermes_require_role_arn Crossplane \
  "arn:aws:iam::111122223333:role/hermes-production-crossplane-readonly" \
  hermes-production-crossplane
expect_ok "accepts a path-qualified role ARN" \
  hermes_require_role_arn Crossplane \
  "arn:aws:iam::111122223333:role/hermes/hermes-production-crossplane" \
  hermes-production-crossplane

echo
echo "hermes_connection_json"
expect_eq "emits exactly the four derivable keys, merged over what exists" \
  '{"centrifugo_nats_url":"tls://x","database_url":"postgres://hermes:pw@db:5432/hermes?sslmode=require","redis_url":"rediss://:tok@cache:6379","centrifugo_redis_address":"cache:6379","centrifugo_redis_password":"tok"}' \
  "$(hermes_connection_json '{"centrifugo_nats_url":"tls://x","database_url":"stale"}' hermes pw db 5432 hermes tok cache 6379 | jq -c .)"
expect_eq "works when no prior secret exists" \
  '{"database_url":"postgres://hermes:pw@db:5432/hermes?sslmode=require","redis_url":"rediss://:tok@cache:6379","centrifugo_redis_address":"cache:6379","centrifugo_redis_password":"tok"}' \
  "$(hermes_connection_json '{}' hermes pw db 5432 hermes tok cache 6379 | jq -c .)"
# centrifugo_redis_password is a standalone Centrifugo env var, not userinfo in a URL,
# so it must NOT be percent-encoded even though redis_url's copy is.
expect_eq "leaves centrifugo_redis_password raw while encoding it inside redis_url" \
  'a/b:c' \
  "$(hermes_connection_json '{}' hermes pw db 5432 hermes 'a/b:c' cache 6379 | jq -r .centrifugo_redis_password)"
expect_eq "and the same token is encoded inside redis_url" \
  'rediss://:a%2Fb%3Ac@cache:6379' \
  "$(hermes_connection_json '{}' hermes pw db 5432 hermes 'a/b:c' cache 6379 | jq -r .redis_url)"

echo
echo "----"
echo "passed: $PASS  failed: $FAIL"
[[ $FAIL -eq 0 ]]
