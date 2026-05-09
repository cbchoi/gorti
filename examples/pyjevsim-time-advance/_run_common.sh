#!/usr/bin/env bash
set -euo pipefail
_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${_HERE}/../.." && pwd)"

: "${RTID_LISTEN_PORT:=8442}"
: "${RTID_ADMIN_PORT:=8443}"
: "${RTID_METRICS_PORT:=9090}"
: "${RTID_URL:=grpc://127.0.0.1:${RTID_LISTEN_PORT}}"

: "${CYCLES:=10}"
: "${TICK_STEP:=3.0}"
: "${RESULT_DIR:=/tmp/pyjevsim-time-advance-cross}"
mkdir -p "${RESULT_DIR}"

: "${RTID_BINARY:=${REPO_ROOT}/bin/rtid}"
: "${PYTHON:=python3}"
