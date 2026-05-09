#!/usr/bin/env bash
# Runs the consumer federate against a running rtid. See producer_run.sh
# for the required launch order (consumer must be up before the
# producer starts publishing -- pre-subscription publishes are dropped
# server-side).

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

RESULT_PATH="${RESULT_DIR}/consumer-result.json"

echo "consumer_run: dialing ${RTID_URL}, writing ${RESULT_PATH}" >&2
RC=0
"${PYTHON}" "${_HERE}/consumer_main.py" \
    --url "${RTID_URL}" \
    --result "${RESULT_PATH}" \
    --ticks "${TICKS}" \
    --drain-ticks "${DRAIN_TICKS}" \
    --tail-ticks "${CONSUMER_TAIL_TICKS}" \
    --tick-period "${TICK_PERIOD}" \
    --startup-delay 0.0 || RC=$?
report_result "${RC}" "${RESULT_PATH}" consumer_run received
