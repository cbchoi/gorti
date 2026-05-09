#!/usr/bin/env bash
set -euo pipefail
_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

RESULT_PATH="${RESULT_DIR}/normal-result.json"
echo "normal_run: dialing ${RTID_URL}, writing ${RESULT_PATH}" >&2
exec "${PYTHON}" "${_HERE}/regulator_main.py" \
    --url "${RTID_URL}" \
    --result "${RESULT_PATH}" \
    --name "normal" \
    --lookahead 1.0 \
    --cycles "${CYCLES}" \
    --tick-step "${TICK_STEP}"
