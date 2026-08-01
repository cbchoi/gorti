#!/usr/bin/env bash
# Runs the gamma participant federate. See alpha_run.sh for context.

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

NAME=gamma
RESULT_PATH="${RESULT_DIR}/${NAME}-result.json"

echo "${NAME}_run: dialing ${RTID_URL}, writing ${RESULT_PATH}" >&2
RC=0
"${PYTHON}" "${_HERE}/participant_main.py" \
    --url "${RTID_URL}" \
    --result "${RESULT_PATH}" \
    --name "${NAME}" \
    --running-ticks "${RUNNING_TICKS}" \
    --tick-period "${TICK_PERIOD}" \
    --join-settle "${JOIN_SETTLE}" \
    --rendezvous-timeout "${RENDEZVOUS_TIMEOUT}" || RC=$?
report_result "${RC}" "${RESULT_PATH}" "${NAME}_run" achieved synchronized sent_ticks
