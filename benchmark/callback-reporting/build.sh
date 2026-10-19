#!/usr/bin/env bash
set -euo pipefail
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
output="${1:-$(dirname "$repo")/benchmark-results/callback-report-builds}"
mkdir -p "$output"
output="$(cd "$output" && pwd)"
cd "$repo"
for profile in sync async; do
    go build -tags "callback_$profile" -o "$output/rtid-$profile" ./rti/cmd/rtid
    go build -tags "callback_$profile" -o "$output/gorti-go-fair-$profile" ./verification/gorti-go-fair
done
printf 'Built callback profiles in %s\n' "$output"
