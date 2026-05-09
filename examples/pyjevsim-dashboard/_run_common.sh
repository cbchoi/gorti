#!/usr/bin/env bash
set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${_HERE}/../.." && pwd)"

: "${RTID_LISTEN_PORT:=8442}"
: "${RTID_ADMIN_PORT:=8443}"
: "${RTID_METRICS_PORT:=9090}"
: "${RTID_URL:=grpc://127.0.0.1:${RTID_LISTEN_PORT}}"

: "${TICKS:=10}"
: "${MODE:=sequence}"
: "${AMPLITUDE:=100}"
: "${TICK_PERIOD:=0.05}"
: "${DASHBOARD_DRAIN_TICKS:=20}"

: "${RESULT_DIR:=/tmp/pyjevsim-dashboard-cross}"
mkdir -p "${RESULT_DIR}"

: "${RTID_BINARY:=${REPO_ROOT}/bin/rtid}"
: "${PYTHON:=python3}"

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
        exit 1
    fi
    local report
    report=$("${PYTHON}" -c "
import json, sys
d = json.load(open(sys.argv[1]))
out = []
for k in sys.argv[2:]:
    v = d.get(k)
    out.append(f'{k}={len(v)}' if isinstance(v, list) else f'{k}={v!r}')
print(' '.join(out))
" "${result_path}" "$@" 2>/dev/null) || report="(parse error)"
    echo "${script}: DONE — ${report}  result=${result_path}" >&2
}
