#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd -P)"
RUNNER="${SCRIPT_DIR}/runner.py"
DEFAULT_RTID="${SCRIPT_DIR}/.run/bin/rtid"

if [[ -n "${PYTHON:-}" ]]; then
    PYTHON_BIN="${PYTHON}"
elif command -v python3 >/dev/null 2>&1; then
    PYTHON_BIN="$(command -v python3)"
elif command -v python >/dev/null 2>&1; then
    PYTHON_BIN="$(command -v python)"
else
    printf '%s\n' "run.sh: Python 3.11 or newer was not found." >&2
    exit 127
fi

export PYTHONPATH="${REPO_ROOT}/pysdk${PYTHONPATH:+:${PYTHONPATH}}"

for arg in "$@"; do
    if [[ "${arg}" == "-h" || "${arg}" == "--help" ]]; then
        exec "${PYTHON_BIN}" "${RUNNER}" "$@"
    fi
done

if ! "${PYTHON_BIN}" -c 'import sys; sys.version_info >= (3, 11) or sys.exit("Python 3.11 or newer is required"); import grpc; from rti1516e._transport import _ensure_generated_path; _ensure_generated_path(); from rti.v1 import declaration_pb2, federation_pb2, object_pb2, stream_pb2' 2>/dev/null; then
    printf '%s\n' \
        "run.sh: Python dependencies or generated gRPC bindings are unavailable." \
        "From the repository root, run:" \
        "  python -m pip install -e './pysdk[dev]'" \
        "  python -m rti1516e._proto" >&2
    exit 1
fi

rtid_target="${DEFAULT_RTID}"
next_is_rtid=false
for arg in "$@"; do
    if [[ "${next_is_rtid}" == true ]]; then
        rtid_target="${arg}"
        next_is_rtid=false
    elif [[ "${arg}" == "--rtid-binary" ]]; then
        next_is_rtid=true
    elif [[ "${arg}" == --rtid-binary=* ]]; then
        rtid_target="${arg#*=}"
    fi
done

if [[ ! -f "${rtid_target}" ]] && ! command -v go >/dev/null 2>&1; then
    printf '%s\n' \
        "run.sh: rtid was not found at ${rtid_target}." \
        "Install Go or pass --rtid-binary PATH to an existing rtid executable." >&2
    exit 1
fi

exec "${PYTHON_BIN}" "${RUNNER}" --rtid-binary "${DEFAULT_RTID}" "$@"
