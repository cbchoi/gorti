#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RTID_PORT="${RTID_PORT:-18442}"
PEER_DELAY="${PEER_DELAY:-3s}"
RUN_TIMEOUT="${RUN_TIMEOUT:-30s}"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/gorti-tar-wait.XXXXXX")"
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
    echo "go-tar-wait: $*" >&2
    exit 1
}

if ! command -v go >/dev/null 2>&1; then
    fail "Go 1.22 or later is required on PATH."
fi
if [[ ! "${RTID_PORT}" =~ ^[0-9]+$ ]] || ((RTID_PORT < 1 || RTID_PORT > 65535)); then
    fail "RTID_PORT must be an integer from 1 to 65535."
fi

cd "${REPO_ROOT}"
echo "go-tar-wait: building temporary binaries" >&2
go build -buildvcs=false -o "${WORK_DIR}/rtid" ./rti/cmd/rtid
go build -buildvcs=false -o "${WORK_DIR}/go-tar-wait" ./examples/go-tar-wait

mkdir -p "${WORK_DIR}/saves"
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

COMMON_ARGS=(
    --url="127.0.0.1:${RTID_PORT}"
    --federation="tar-wait-run-${RTID_PORT}"
    --peer-delay="${PEER_DELAY}"
    --timeout="${RUN_TIMEOUT}"
    --fom="${SCRIPT_DIR}/tar-wait-fom.xml"
)

echo "go-tar-wait: starting waiter and peer (peer delay ${PEER_DELAY})" >&2
"${WORK_DIR}/go-tar-wait" --role=waiter "${COMMON_ARGS[@]}" \
    >"${WORK_DIR}/waiter.log" 2>&1 &
WAITER_PID=$!
CHILD_PIDS+=("${WAITER_PID}")

"${WORK_DIR}/go-tar-wait" --role=peer "${COMMON_ARGS[@]}" \
    >"${WORK_DIR}/peer.log" 2>&1 &
PEER_PID=$!
CHILD_PIDS+=("${PEER_PID}")

waiter_status=0
peer_status=0
wait "${WAITER_PID}" || waiter_status=$?
wait "${PEER_PID}" || peer_status=$?

cat "${WORK_DIR}/waiter.log"
cat "${WORK_DIR}/peer.log"
if ((waiter_status != 0 || peer_status != 0)); then
    cat "${WORK_DIR}/rtid.log" >&2
    fail "verification failed (waiter=${waiter_status}, peer=${peer_status})."
fi

echo "go-tar-wait: PASS - the peer delay held and then released TAR(5)" >&2
