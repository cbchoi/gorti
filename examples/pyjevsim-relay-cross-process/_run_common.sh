#!/usr/bin/env bash
# Sourced by rtid_run.sh and the three federate_run scripts. Holds
# the defaults so all four scripts agree on which rtid to dial,
# where results land, and how to tune the pipeline.
#
# Override any variable by exporting it before invoking a script:
#
#   RTID_LISTEN_PORT=9000 ./rtid_run.sh
#   RTID_URL=grpc://127.0.0.1:9000 ./generator_run.sh
#
# This mirrors the per-run defaults the Python runner.py uses, but
# with FIXED ports instead of free-port discovery so the federate
# scripts know exactly where to dial without a registry.

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${_HERE}/../.." && pwd)"

# Ports — match docs/rtid-tui.md conventions.
: "${RTID_LISTEN_PORT:=8442}"
: "${RTID_ADMIN_PORT:=8443}"
: "${RTID_METRICS_PORT:=9090}"
: "${RTID_URL:=grpc://127.0.0.1:${RTID_LISTEN_PORT}}"

# Federate tunables — defaults match _federate_common.common_parser.
: "${GEN_MESSAGES:=50}"
: "${CAPACITY:=5}"
: "${SERVICE_PERIOD:=2}"
: "${DRAIN_TICKS:=30}"
: "${TICK_PERIOD:=0.05}"

# Per-federate tail ticks — runner.py uses 0 / 20 / 40 for
# generator / buffer / processor. Buffer < processor so the
# processor is still draining when the buffer's last emit lands.
: "${BUFFER_TAIL_TICKS:=20}"
: "${PROCESSOR_TAIL_TICKS:=40}"

# Result + log destinations.
: "${RESULT_DIR:=/tmp/pyjevsim-relay-cross}"
mkdir -p "${RESULT_DIR}"

# rtid binary location.
: "${RTID_BINARY:=${REPO_ROOT}/bin/rtid}"

# Python interpreter.
: "${PYTHON:=python3}"
