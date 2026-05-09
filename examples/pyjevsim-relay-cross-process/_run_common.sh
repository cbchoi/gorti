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

# report_result — print a DONE/FAILED summary after a federate's
# python exits. Each federate script calls this with its python exit
# code, the result JSON path, the script name, and the keys it
# expects to find in the result. Exits the shell on failure so the
# user sees a clear non-zero return in the terminal.
#
# Usage:
#   "${PYTHON}" generator_main.py ... || RC=$?
#   report_result "${RC:-0}" "${RESULT_PATH}" generator_run published
report_result() {
    local rc=$1
    local result_path=$2
    local script=$3
    shift 3

    if [[ ${rc} -ne 0 ]]; then
        echo "${script}: FAILED — python exited with ${rc}" >&2
        exit "${rc}"
    fi
    if [[ ! -f "${result_path}" ]]; then
        echo "${script}: FAILED — no result file at ${result_path}" >&2
        echo "${script}                (federate likely crashed before writing)" >&2
        exit 1
    fi

    local report
    report=$("${PYTHON}" -c "
import json, sys
d = json.load(open(sys.argv[1]))
out = []
for k in sys.argv[2:]:
    v = d.get(k)
    out.append(f'{k}={len(v)}' if isinstance(v, list) else f'{k}=?')
print(' '.join(out))
" "${result_path}" "$@" 2>/dev/null) || report="(parse error)"
    echo "${script}: DONE — ${report}  result=${result_path}" >&2
}
