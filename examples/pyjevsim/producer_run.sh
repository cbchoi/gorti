#!/usr/bin/env bash
# Runs the producer federate against a running rtid.
#
# Required ordering (rtid first, consumer before producer so the
# subscription lands before any publish):
#
#   Terminal 1:  ./rtid_run.sh
#   Terminal 2:  ./consumer_run.sh
#   Terminal 3:  ./producer_run.sh   <- this script
#   Terminal 4:  ./verify_run.sh     (after both federates exit)

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

RESULT_PATH="${RESULT_DIR}/producer-result.json"

echo "producer_run: dialing ${RTID_URL}, writing ${RESULT_PATH}" >&2
RC=0
"${PYTHON}" "${_HERE}/producer_main.py" \
    --url "${RTID_URL}" \
    --result "${RESULT_PATH}" \
    --ticks "${TICKS}" \
    --drain-ticks "${DRAIN_TICKS}" \
    --tail-ticks 0 \
    --tick-period "${TICK_PERIOD}" \
    --startup-delay 0.0 || RC=$?
report_result "${RC}" "${RESULT_PATH}" producer_run published
