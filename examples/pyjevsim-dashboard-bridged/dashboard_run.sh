#!/usr/bin/env bash
# Runs the dashboard federate against a running rtid. See
# sensor_run.sh for the required launch order (dashboard MUST be
# subscribed before the sensor starts updating).

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

RESULT_PATH="${RESULT_DIR}/dashboard-result.json"

echo "dashboard_run: dialing ${RTID_URL}, writing ${RESULT_PATH}" >&2
RC=0
"${PYTHON}" "${_HERE}/dashboard_main.py" \
    --url "${RTID_URL}" \
    --result "${RESULT_PATH}" \
    --ticks "${TICKS}" \
    --mode "${MODE}" \
    --amplitude "${AMPLITUDE}" \
    --tick-period "${TICK_PERIOD}" \
    --drain-ticks "${DASHBOARD_DRAIN_TICKS}" \
    --startup-delay 0.0 || RC=$?
report_result "${RC}" "${RESULT_PATH}" dashboard_run received discovered
