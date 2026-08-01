#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

fail() {
    echo "ieee1516e-chat-parity: $*" >&2
    exit 1
}

PYTHON_BIN="${PYTHON:-}"
if [[ -z "${PYTHON_BIN}" ]]; then
    for candidate in python3 python.exe python; do
        if command -v "${candidate}" >/dev/null 2>&1 && \
            "${candidate}" -c 'import sys; raise SystemExit(sys.version_info < (3, 11))' >/dev/null 2>&1; then
            PYTHON_BIN="${candidate}"
            break
        fi
    done
fi
if [[ -z "${PYTHON_BIN}" ]]; then
    fail "Python 3.11 or later is required on PATH."
fi
"${PYTHON_BIN}" -c 'import sys; raise SystemExit(sys.version_info < (3, 11))' >/dev/null 2>&1 || fail "PYTHON does not name a working Python 3.11+ interpreter: ${PYTHON_BIN}"
command -v go >/dev/null 2>&1 || fail "Go 1.22 or later is required to build rtid."

if ! compgen -G "${REPO_ROOT}/pysdk/rti1516e/_generated/rti/v1/*_pb2.py" >/dev/null; then
    echo "ieee1516e-chat-parity: generating gRPC bindings" >&2
    "${PYTHON_BIN}" "${REPO_ROOT}/pysdk/rti1516e/_proto.py" ||
        fail "Python code generation failed; install with: ${PYTHON_BIN} -m pip install -e '${REPO_ROOT}/pysdk[dev]'"
fi

if ! "${PYTHON_BIN}" -c "import sys; assert sys.version_info >= (3, 11); import grpc, google.protobuf, pytest" >/dev/null 2>&1; then
    fail "Python 3.11 plus grpcio, protobuf, and pytest are required. Install with: ${PYTHON_BIN} -m pip install -e '${REPO_ROOT}/pysdk[dev]'"
fi

echo "ieee1516e-chat-parity: running the language-neutral Chat contract" >&2
cd "${REPO_ROOT}"
"${PYTHON_BIN}" -m pytest -q -s \
    tests/reference_examples/test_reference_example_contracts.py::test_gorti_chat_matches_ieee1516_language_neutral_contract
