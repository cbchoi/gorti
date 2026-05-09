#!/usr/bin/env bash
# Runs the buffer federate against a running rtid. See generator_run.sh
# for the required launch order.

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

RESULT_PATH="${RESULT_DIR}/buffer-result.json"

echo "buffer_run: dialing ${RTID_URL}, writing ${RESULT_PATH}" >&2
exec "${PYTHON}" "${_HERE}/buffer_main.py" \
    --url "${RTID_URL}" \
    --result "${RESULT_PATH}" \
    --gen-messages "${GEN_MESSAGES}" \
    --capacity "${CAPACITY}" \
    --service-period "${SERVICE_PERIOD}" \
    --drain-ticks "${DRAIN_TICKS}" \
    --tail-ticks "${BUFFER_TAIL_TICKS}" \
    --tick-period "${TICK_PERIOD}" \
    --startup-delay 0.0
