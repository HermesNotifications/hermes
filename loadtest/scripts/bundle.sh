#!/usr/bin/env bash
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.
set -euo pipefail

# Bundle k6 scenarios into single self-contained files under loadtest/dist/.
# k6 built-ins and remote imports stay external.
cd "$(dirname "$0")/../.."
mkdir -p loadtest/dist

for s in loadtest/scenarios/*.js; do
  name=$(basename "$s" .js)
  npx --yes esbuild@^0.24.0 "$s" \
    --bundle \
    --platform=neutral \
    --format=esm \
    --target=es2020 \
    --external:k6 \
    --external:"k6/*" \
    --external:"https://*" \
    --outfile="loadtest/dist/${name}.js"
done

echo "Bundled $(ls loadtest/dist/*.js | wc -l | tr -d ' ') scenario(s) to loadtest/dist/"
