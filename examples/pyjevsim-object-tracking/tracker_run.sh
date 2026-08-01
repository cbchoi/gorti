#!/usr/bin/env bash
# Launch a tracker federate against an already-running rtid.
#
# Usage:
#   ./tracker_run.sh tracker-A                                # name=tracker-A
#   ./tracker_run.sh tracker-B --cycles 10                    # passthrough flags
#   RTID_URL=grpc://127.0.0.1:9000 ./tracker_run.sh observer  # custom rtid endpoint
#
# Required positional arg:
#   $1   federate name (e.g., tracker-A, tracker-B). Must be unique within the
#        federation — joining with a name already in use returns ErrFederateNameInUse.
#
# Required env (or autodetected):
#   RTID_URL   default: grpc://127.0.0.1:8442
#   RTID_VENV  default: <repo>/.m21-venv (must have pysdk + grpcio installed)
#
# Pre-flight: rtid AND the producer must already be running. The tracker
# blocks on a DiscoverObjectInstance event before entering its NMRA loop;
# without the producer in the federation, the tracker exits with a
# 'wait_for_discover: no Discover within ...' timeout.
set -euo pipefail

if [[ $# -lt 1 ]]; then
    echo "usage: $0 <federate-name> [extra-flags...]" >&2
    echo "  example: $0 tracker-A" >&2
    echo "  example: $0 tracker-B --cycles 10" >&2
    exit 2
fi

NAME="$1"
shift

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
REPO_ROOT="$(cd -- "$HERE/../.." &> /dev/null && pwd)"

RTID_URL="${RTID_URL:-grpc://127.0.0.1:8442}"
RTID_VENV="${RTID_VENV:-$REPO_ROOT/.m21-venv}"

PY="$RTID_VENV/bin/python"
if [[ ! -x "$PY" ]]; then
    echo "tracker_run.sh: venv python not found at $PY" >&2
    echo "  set RTID_VENV=<path-to-venv> or create the venv first:" >&2
    echo "    python3 -m venv .m21-venv && .m21-venv/bin/pip install -e ./pysdk" >&2
    exit 1
fi

WORKDIR="$HERE/.run/manual"
mkdir -p "$WORKDIR"
RESULT="$WORKDIR/${NAME}-result.json"

echo "[tracker_run] name:      $NAME"
echo "[tracker_run] rtid url:  $RTID_URL"
echo "[tracker_run] venv:      $RTID_VENV"
echo "[tracker_run] result:    $RESULT"
echo "[tracker_run] make sure rtid + producer are running on $RTID_URL"

exec env PYTHONUNBUFFERED=1 "$PY" "$HERE/tracker_main.py" \
    --url "$RTID_URL" \
    --result "$RESULT" \
    --name "$NAME" \
    --cycles 5 \
    --tick-step 1.0 \
    --lookahead 1.0 \
    "$@"
