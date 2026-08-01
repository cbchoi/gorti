#!/usr/bin/env bash
set -Eeuo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "$HERE/../.." >/dev/null 2>&1 && pwd)"
EXAMPLE_NAME="$(basename -- "$HERE")"

fail() {
    echo "$EXAMPLE_NAME: $*" >&2
    exit 1
}

resolve_python() {
    local candidate
    local candidates=()
    [[ -n "${PYTHON:-}" ]] && candidates+=("$PYTHON")
    if [[ -n "${RTID_VENV:-}" ]]; then
        candidates+=("$RTID_VENV/bin/python" "$RTID_VENV/Scripts/python.exe")
    fi
    candidates+=(
        "$REPO_ROOT/.venv/bin/python"
        "$REPO_ROOT/.venv/Scripts/python.exe"
        "$REPO_ROOT/.m21-venv/bin/python"
        "$REPO_ROOT/.m21-venv/Scripts/python.exe"
        python3
        python
    )

    for candidate in "${candidates[@]}"; do
        if [[ -x "$candidate" ]] || command -v "$candidate" >/dev/null 2>&1; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done
    return 1
}

PY="$(resolve_python)" || fail "Python 3.11 or newer was not found. Set PYTHON or RTID_VENV."

"$PY" -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 11) else 1)' \
    || fail "Python 3.11 or newer is required (selected: $PY)."

PYSDK="$REPO_ROOT/pysdk" "$PY" -c '
import os
import sys
sys.path.insert(0, os.environ["PYSDK"])
try:
    import grpc  # noqa: F401
    import google.protobuf  # noqa: F401
    import rti1516e.connection  # noqa: F401
except (ImportError, RuntimeError) as exc:
    print(f"runtime dependency check failed: {exc}", file=sys.stderr)
    raise SystemExit(1)
' || fail "Install the SDK dependencies with: $PY -m pip install -e \"$REPO_ROOT/pysdk\""

rtid_args=()
explicit_rtid=""
runner_args=("$@")
for ((i = 0; i < ${#runner_args[@]}; i++)); do
    case "${runner_args[$i]}" in
        --rtid-binary)
            ((i + 1 < ${#runner_args[@]})) \
                || fail "--rtid-binary requires a path."
            explicit_rtid="${runner_args[$((i + 1))]}"
            ;;
        --rtid-binary=*)
            explicit_rtid="${runner_args[$i]#--rtid-binary=}"
            ;;
    esac
done

if [[ -n "$explicit_rtid" ]]; then
    [[ -f "$explicit_rtid" ]] || fail "--rtid-binary does not exist: $explicit_rtid"
elif [[ -n "${RTID_BINARY:-}" ]]; then
    [[ -f "$RTID_BINARY" ]] || fail "RTID_BINARY does not exist: $RTID_BINARY"
    rtid_args+=(--rtid-binary "$RTID_BINARY")
elif [[ ! -f "$REPO_ROOT/bin/rtid" && ! -f "$REPO_ROOT/bin/rtid.exe" && \
        ! -f "$HERE/.run/bin/rtid" && ! -f "$HERE/.run/bin/rtid.exe" ]]; then
    command -v go >/dev/null 2>&1 \
        || fail "rtid was not found and the Go toolchain is unavailable. Set RTID_BINARY or install Go."
fi

exec "$PY" "$HERE/runner.py" "${rtid_args[@]}" "${runner_args[@]}"
