#!/usr/bin/env bash
# Sourced defaults for the manual-run shell scripts (rtid_run.sh +
# fast/normal/slow_run.sh + verify_run.sh). Override via env, e.g.
# RTID_LISTEN_PORT=9000 ./rtid_run.sh.

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${_HERE}/../.." && pwd)"

: "${RTID_LISTEN_PORT:=8442}"
: "${RTID_ADMIN_PORT:=8443}"
: "${RTID_METRICS_PORT:=9090}"
: "${RTID_URL:=127.0.0.1:${RTID_LISTEN_PORT}}"

# Federate tunables.
: "${FEDERATION:=go-timed}"
: "${CYCLES:=10}"
: "${TICK_STEP:=3.0}"     # must exceed slow's lookahead (2.0)
: "${PRIMITIVE:=TAR}"     # TAR keeps the cycle loop robust; see runner_test.go

: "${RESULT_DIR:=/tmp/go-timed}"
mkdir -p "${RESULT_DIR}"

: "${RTID_BINARY:=${REPO_ROOT}/bin/rtid}"
: "${REGULATOR_BINARY:=${REPO_ROOT}/bin/go-timed}"

report_done() {
    local name=$1
    local result_path=$2
    if [[ ! -f "${result_path}" ]]; then
        echo "${name}_run: FAILED — no result file at ${result_path}" >&2
        exit 1
    fi
    local python_bin="${PYTHON:-}"
    if [[ -z "${python_bin}" ]]; then
        for candidate in python3 python.exe python; do
            if command -v "${candidate}" >/dev/null 2>&1 && \
                "${candidate}" -c 'import sys; raise SystemExit(sys.version_info < (3, 8))' >/dev/null 2>&1; then
                python_bin="${candidate}"
                break
            fi
        done
    fi
    local grants_count="?"
    if [[ -n "${python_bin}" ]]; then
        grants_count=$("${python_bin}" -c "import json; print(len(json.load(open('${result_path}'))['grants']))" 2>/dev/null || echo "?")
    fi
    echo "${name}_run: DONE — grants=${grants_count}  result=${result_path}" >&2
}
