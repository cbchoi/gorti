#!/usr/bin/env bash
# Runs the slow federate (lookahead 2.0). Start rtid first.
set -euo pipefail
_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

if [[ ! -x "${REGULATOR_BINARY}" ]]; then
    echo "slow_run: building ${REGULATOR_BINARY}..." >&2
    (cd "${REPO_ROOT}" && go build -o bin/go-timed ./examples/go-timed)
fi

RESULT_PATH="${RESULT_DIR}/slow-result.json"
echo "slow_run: dialing ${RTID_URL}, writing ${RESULT_PATH}" >&2
"${REGULATOR_BINARY}" \
    --url "${RTID_URL}" \
    --federation "${FEDERATION}" \
    --name "slow" \
    --lookahead 2.0 \
    --primitive "${PRIMITIVE}" \
    --constrained=true \
    --cycles "${CYCLES}" \
    --tick-step "${TICK_STEP}" \
    --result "${RESULT_PATH}" \
    --fom "${_HERE}/time-advance-fom.xml"
report_done "slow" "${RESULT_PATH}"
