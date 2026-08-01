#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ROUNDS="${ROUNDS:-1000}"

if ! command -v go >/dev/null 2>&1; then
    echo "go-pingpong: Go 1.22 or later is required on PATH." >&2
    exit 1
fi
if [[ ! "${ROUNDS}" =~ ^[1-9][0-9]*$ ]]; then
    echo "go-pingpong: ROUNDS must be a positive integer (got ${ROUNDS})." >&2
    exit 1
fi

echo "go-pingpong: running ${ROUNDS} verified round trips" >&2
cd "${REPO_ROOT}"
go run -buildvcs=false ./examples/go-pingpong --rounds "${ROUNDS}"
