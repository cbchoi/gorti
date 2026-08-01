#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RTID_PORT="${RTID_PORT:-18452}"
CYCLES="${CYCLES:-10}"
TICK_STEP="${TICK_STEP:-3.0}"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/gorti-go-timed.XXXXXX")"
RTID_PID=""
CHILD_PIDS=()

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    for pid in "${CHILD_PIDS[@]}" "${RTID_PID}"; do
        if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
            kill "${pid}" 2>/dev/null || true
        fi
    done
    for pid in "${CHILD_PIDS[@]}" "${RTID_PID}"; do
        if [[ -n "${pid}" ]]; then
            wait "${pid}" 2>/dev/null || true
        fi
    done
    rm -rf "${WORK_DIR}"
    exit "${status}"
}
trap cleanup EXIT INT TERM

fail() {
    echo "go-timed: $*" >&2
    exit 1
}

if ! command -v go >/dev/null 2>&1; then
    fail "Go 1.22 or later is required on PATH."
fi
PYTHON="${PYTHON:-}"
if [[ -n "${PYTHON}" ]] && ! "${PYTHON}" -c 'import sys; raise SystemExit(sys.version_info < (3, 8))' >/dev/null 2>&1; then
    fail "PYTHON does not name a working Python 3 interpreter: ${PYTHON}"
fi
if [[ -z "${PYTHON}" ]]; then
    for candidate in python3 python.exe python; do
        if command -v "${candidate}" >/dev/null 2>&1 && \
            "${candidate}" -c 'import sys; raise SystemExit(sys.version_info < (3, 8))' >/dev/null 2>&1; then
            PYTHON="${candidate}"
            break
        fi
    done
fi
if [[ -z "${PYTHON}" ]]; then
    fail "Python 3 is required for the result verifier."
fi
if ! TICK_STEP="${TICK_STEP}" "${PYTHON}" -c \
    'import math, os; value = float(os.environ["TICK_STEP"]); raise SystemExit(not math.isfinite(value) or value <= 2.0)' \
    >/dev/null 2>&1; then
    fail "TICK_STEP must be a finite number greater than the slow federate lookahead of 2.0."
fi
if [[ ! "${RTID_PORT}" =~ ^[0-9]+$ ]] || ((RTID_PORT < 1 || RTID_PORT > 65535)); then
    fail "RTID_PORT must be an integer from 1 to 65535."
fi
if [[ ! "${CYCLES}" =~ ^[1-9][0-9]*$ ]]; then
    fail "CYCLES must be a positive integer."
fi

cd "${REPO_ROOT}"
echo "go-timed: building temporary RTI and federate binaries" >&2
go build -buildvcs=false -o "${WORK_DIR}/rtid" ./rti/cmd/rtid
go build -buildvcs=false -o "${WORK_DIR}/go-timed" ./examples/go-timed

mkdir -p "${WORK_DIR}/results" "${WORK_DIR}/saves"
"${WORK_DIR}/rtid" \
    --listen="127.0.0.1:${RTID_PORT}" \
    --metrics-listen="127.0.0.1:0" \
    --admin-listen= \
    --log-level=warn \
    --save-dir="${WORK_DIR}/saves" \
    >"${WORK_DIR}/rtid.log" 2>&1 &
RTID_PID=$!

for _ in {1..100}; do
    if ! kill -0 "${RTID_PID}" 2>/dev/null; then
        cat "${WORK_DIR}/rtid.log" >&2
        fail "rtid exited before accepting connections."
    fi
    if (exec 3<>"/dev/tcp/127.0.0.1/${RTID_PORT}") 2>/dev/null; then
        break
    fi
    sleep 0.1
done
if ! (exec 3<>"/dev/tcp/127.0.0.1/${RTID_PORT}") 2>/dev/null; then
    cat "${WORK_DIR}/rtid.log" >&2
    fail "rtid did not listen on 127.0.0.1:${RTID_PORT} within 10 seconds."
fi

start_federate() {
    local name=$1
    local lookahead=$2
    "${WORK_DIR}/go-timed" \
        --url="127.0.0.1:${RTID_PORT}" \
        --federation="go-timed-run-${RTID_PORT}" \
        --name="${name}" \
        --lookahead="${lookahead}" \
        --primitive=TAR \
        --constrained=true \
        --cycles="${CYCLES}" \
        --tick-step="${TICK_STEP}" \
        --result="${WORK_DIR}/results/${name}-result.json" \
        --fom="${SCRIPT_DIR}/time-advance-fom.xml" \
        >"${WORK_DIR}/${name}.log" 2>&1 &
    CHILD_PIDS+=("$!")
}

echo "go-timed: starting fast, normal, and slow federates" >&2
start_federate fast 0.5
start_federate normal 1.0
start_federate slow 2.0

federate_status=0
for pid in "${CHILD_PIDS[@]}"; do
    wait "${pid}" || federate_status=1
done
for name in fast normal slow; do
    cat "${WORK_DIR}/${name}.log"
done
if ((federate_status != 0)); then
    cat "${WORK_DIR}/rtid.log" >&2
    fail "one or more federates failed."
fi

RESULT_DIR="${WORK_DIR}/results" CYCLES="${CYCLES}" PYTHON="${PYTHON}" \
    bash "${SCRIPT_DIR}/verify_run.sh"
echo "go-timed: PASS - all cross-process time invariants held" >&2
