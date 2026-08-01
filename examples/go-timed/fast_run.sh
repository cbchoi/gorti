#!/usr/bin/env bash
# Runs the fast federate (lookahead 0.5). Start rtid first.
set -euo pipefail
_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

if [[ ! -x "${REGULATOR_BINARY}" ]]; then
    echo "fast_run: building ${REGULATOR_BINARY}..." >&2
    (cd "${REPO_ROOT}" && go build -o bin/go-timed ./examples/go-timed)
fi

RESULT_PATH="${RESULT_DIR}/fast-result.json"
echo "fast_run: dialing ${RTID_URL}, writing ${RESULT_PATH}" >&2
"${REGULATOR_BINARY}" \
    --url "${RTID_URL}" \
    --federation "${FEDERATION}" \
    --name "fast" \
    --lookahead 0.5 \
    --primitive "${PRIMITIVE}" \
    --constrained=true \
    --cycles "${CYCLES}" \
    --tick-step "${TICK_STEP}" \
    --result "${RESULT_PATH}" \
    --fom "${_HERE}/time-advance-fom.xml"
report_done "fast" "${RESULT_PATH}"
