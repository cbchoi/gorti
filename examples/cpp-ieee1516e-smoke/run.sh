#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RTID_PORT="${RTID_PORT:-18080}"
HOLD_SECONDS="${HOLD_SECONDS:-0}"
CPP_BUILD_DIR="${CPP_BUILD_DIR:-${REPO_ROOT}/cppsdk/build}"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/gorti-cpp-smoke.XXXXXX")"
RTID_PID=""

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    if [[ -n "${RTID_PID}" ]] && kill -0 "${RTID_PID}" 2>/dev/null; then
        kill "${RTID_PID}" 2>/dev/null || true
        wait "${RTID_PID}" 2>/dev/null || true
    fi
    rm -rf "${WORK_DIR}"
    exit "${status}"
}
trap cleanup EXIT INT TERM

fail() {
    echo "cpp-ieee1516e-smoke: $*" >&2
    exit 1
}

if [[ ! "${RTID_PORT}" =~ ^[0-9]+$ ]] || ((RTID_PORT < 1 || RTID_PORT > 65535)); then
    fail "RTID_PORT must be an integer from 1 to 65535."
fi
if [[ ! "${HOLD_SECONDS}" =~ ^[0-9]+$ ]]; then
    fail "HOLD_SECONDS must be a non-negative integer."
fi
if [[ "${CPP_BUILD_DIR}" != /* ]]; then
    CPP_BUILD_DIR="${REPO_ROOT}/${CPP_BUILD_DIR}"
fi

PUBLISHER="${PUBLISHER_BINARY:-}"
if [[ -z "${PUBLISHER}" ]]; then
    command -v cmake >/dev/null 2>&1 || fail "CMake 3.18 or later is required to build the C++ publisher."
    if ! compgen -G "${REPO_ROOT}/cppsdk/_generated/rti/v1/*.cc" >/dev/null; then
        command -v buf >/dev/null 2>&1 || fail "generated C++ bindings are missing; install buf and run 'buf generate' from ${REPO_ROOT}."
        echo "cpp-ieee1516e-smoke: generating protobuf and gRPC bindings" >&2
        (cd "${REPO_ROOT}" && buf generate)
    fi

    if [[ ! -f "${CPP_BUILD_DIR}/CMakeCache.txt" ]]; then
        configure_args=(
            -S "${REPO_ROOT}/cppsdk"
            -B "${CPP_BUILD_DIR}"
            -DCMAKE_BUILD_TYPE=Release
        )
        if [[ -n "${CMAKE_TOOLCHAIN_FILE:-}" ]]; then
            configure_args+=("-DCMAKE_TOOLCHAIN_FILE=${CMAKE_TOOLCHAIN_FILE}")
        elif [[ -f "${CPP_BUILD_DIR}/conan_toolchain.cmake" ]]; then
            configure_args+=("-DCMAKE_TOOLCHAIN_FILE=${CPP_BUILD_DIR}/conan_toolchain.cmake")
        fi
        if [[ -n "${CMAKE_PREFIX_PATH:-}" ]]; then
            configure_args+=("-DCMAKE_PREFIX_PATH=${CMAKE_PREFIX_PATH}")
        fi
        echo "cpp-ieee1516e-smoke: configuring the C++ SDK" >&2
        if ! cmake "${configure_args[@]}"; then
            fail "CMake configure failed. Install gRPC++ and protobuf, or prepare ${CPP_BUILD_DIR} with Conan as described in the README."
        fi
    fi

    echo "cpp-ieee1516e-smoke: building cpp_ieee1516e_publisher" >&2
    if ! cmake --build "${CPP_BUILD_DIR}" --config Release \
        --target cpp_ieee1516e_publisher --parallel; then
        fail "C++ publisher build failed."
    fi
    PUBLISHER=$(find "${CPP_BUILD_DIR}" -type f -name 'cpp_ieee1516e_publisher' -print -quit)
    [[ -n "${PUBLISHER}" ]] || fail "the build completed but cpp_ieee1516e_publisher was not found below ${CPP_BUILD_DIR}."
fi
[[ -x "${PUBLISHER}" ]] || fail "publisher binary is not executable: ${PUBLISHER}"

RTID="${RTID_BINARY:-}"
if [[ -z "${RTID}" ]]; then
    command -v go >/dev/null 2>&1 || fail "Go 1.22 or later is required to build rtid."
    echo "cpp-ieee1516e-smoke: building a temporary rtid" >&2
    (cd "${REPO_ROOT}" && go build -buildvcs=false -o "${WORK_DIR}/rtid" ./rti/cmd/rtid)
    RTID="${WORK_DIR}/rtid"
fi
[[ -x "${RTID}" ]] || fail "rtid binary is not executable: ${RTID}"

mkdir -p "${WORK_DIR}/saves"
"${RTID}" \
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

echo "cpp-ieee1516e-smoke: running the publisher" >&2
if ! "${PUBLISHER}" \
    --url "grpc://127.0.0.1:${RTID_PORT}" \
    --federation "cpp-ieee1516e-smoke-${RTID_PORT}" \
    --fom "${SCRIPT_DIR}/federation.fom.xml" \
    --hold "${HOLD_SECONDS}" \
    >"${WORK_DIR}/publisher.log" 2>&1; then
    cat "${WORK_DIR}/publisher.log" >&2
    cat "${WORK_DIR}/rtid.log" >&2
    fail "publisher exited unsuccessfully."
fi
cat "${WORK_DIR}/publisher.log"
if ! grep -q "publisher: done" "${WORK_DIR}/publisher.log"; then
    fail "publisher exited without its completion marker."
fi
echo "cpp-ieee1516e-smoke: PASS - connect, publish, update, and interaction send completed" >&2
