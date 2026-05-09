#!/usr/bin/env bash
# Launches rtid for manual sensor + dashboard runs.

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

if [[ ! -x "${RTID_BINARY}" ]]; then
    echo "rtid_run: building ${RTID_BINARY} (one-time)..." >&2
    (cd "${REPO_ROOT}" && go build -o bin/rtid ./rti/cmd/rtid)
fi

SAVE_DIR="${RESULT_DIR}/saves"
LOG_DIR="${RESULT_DIR}/logs"
mkdir -p "${SAVE_DIR}" "${LOG_DIR}"

echo "rtid_run: listening on ${RTID_URL}" >&2
exec "${RTID_BINARY}" \
    --listen ":${RTID_LISTEN_PORT}" \
    --metrics-listen ":${RTID_METRICS_PORT}" \
    --admin-listen "127.0.0.1:${RTID_ADMIN_PORT}" \
    --log-level info \
    --log-dir "${LOG_DIR}" \
    --save-dir "${SAVE_DIR}"
