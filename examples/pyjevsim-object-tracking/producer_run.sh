#!/usr/bin/env bash
# Launch the producer federate against an already-running rtid.
#
# Usage:
#   ./producer_run.sh                                # defaults
#   ./producer_run.sh --cycles 10 --tick-step 0.5    # passthrough flags
#   RTID_URL=grpc://127.0.0.1:9000 ./producer_run.sh # custom rtid endpoint
#
# Required env (or autodetected):
#   RTID_URL   default: grpc://127.0.0.1:8442
#   RTID_VENV  default: <repo>/.m21-venv (must have pysdk + grpcio installed)
#
# Pre-flight: rtid must already be listening on RTID_URL.
# Start it in another terminal first:
#   ./bin/rtid --listen :8442
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
REPO_ROOT="$(cd -- "$HERE/../.." &> /dev/null && pwd)"

RTID_URL="${RTID_URL:-grpc://127.0.0.1:8442}"
RTID_VENV="${RTID_VENV:-$REPO_ROOT/.m21-venv}"

PY="$RTID_VENV/bin/python"
if [[ ! -x "$PY" ]]; then
    echo "producer_run.sh: venv python not found at $PY" >&2
    echo "  set RTID_VENV=<path-to-venv> or create the venv first:" >&2
    echo "    python3 -m venv .m21-venv && .m21-venv/bin/pip install -e ./pysdk" >&2
    exit 1
fi

WORKDIR="$HERE/.run/manual"
mkdir -p "$WORKDIR"
RESULT="$WORKDIR/producer-result.json"

echo "[producer_run] rtid url:  $RTID_URL"
echo "[producer_run] venv:      $RTID_VENV"
echo "[producer_run] result:    $RESULT"
echo "[producer_run] make sure rtid is running on $RTID_URL"

exec env PYTHONUNBUFFERED=1 "$PY" "$HERE/producer_main.py" \
    --url "$RTID_URL" \
    --result "$RESULT" \
    --name producer \
    --cycles 5 \
    --tick-step 1.0 \
    --lookahead 1.0 \
    "$@"
