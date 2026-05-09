#!/usr/bin/env bash
# Runs the generator federate against a running rtid.
#
# Required ordering (rtid first, consumers before generator so their
# subscriptions land before any publish):
#
#   Terminal 1:  ./rtid_run.sh
#   Terminal 2:  ./processor_run.sh
#   Terminal 3:  ./buffer_run.sh
#   Terminal 4:  ./generator_run.sh   <- this script

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

RESULT_PATH="${RESULT_DIR}/generator-result.json"

echo "generator_run: dialing ${RTID_URL}, writing ${RESULT_PATH}" >&2
RC=0
"${PYTHON}" "${_HERE}/generator_main.py" \
    --url "${RTID_URL}" \
    --result "${RESULT_PATH}" \
    --gen-messages "${GEN_MESSAGES}" \
    --capacity "${CAPACITY}" \
    --service-period "${SERVICE_PERIOD}" \
    --drain-ticks "${DRAIN_TICKS}" \
    --tail-ticks 0 \
    --tick-period "${TICK_PERIOD}" \
    --startup-delay 0.0 || RC=$?
report_result "${RC}" "${RESULT_PATH}" generator_run published
