#!/usr/bin/env bash
# Runs the sensor federate against a running rtid.
#
# Required ordering: rtid first, dashboard second (so its
# subscribe_object_class lands before any update_attributes), then
# sensor:
#
#   Terminal 1:  ./rtid_run.sh
#   Terminal 2:  ./dashboard_run.sh
#   Terminal 3:  ./sensor_run.sh           <- this script
#   Terminal 4:  ./verify_run.sh           (after both federates exit)

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

RESULT_PATH="${RESULT_DIR}/sensor-result.json"

echo "sensor_run: dialing ${RTID_URL}, writing ${RESULT_PATH}" >&2
RC=0
"${PYTHON}" "${_HERE}/sensor_main.py" \
    --url "${RTID_URL}" \
    --result "${RESULT_PATH}" \
    --ticks "${TICKS}" \
    --mode "${MODE}" \
    --amplitude "${AMPLITUDE}" \
    --tick-period "${TICK_PERIOD}" \
    --drain-ticks 0 \
    --startup-delay 0.0 || RC=$?
report_result "${RC}" "${RESULT_PATH}" sensor_run published
