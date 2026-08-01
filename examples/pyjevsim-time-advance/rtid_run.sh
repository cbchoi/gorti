#!/usr/bin/env bash
set -euo pipefail
_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

if [[ ! -x "${RTID_BINARY}" ]]; then
    (cd "${REPO_ROOT}" && go build -o bin/rtid ./rti/cmd/rtid)
fi
mkdir -p "${RESULT_DIR}/saves" "${RESULT_DIR}/logs"
echo "rtid_run: listening on ${RTID_URL}" >&2
exec "${RTID_BINARY}" \
    --listen ":${RTID_LISTEN_PORT}" \
    --metrics-listen ":${RTID_METRICS_PORT}" \
    --admin-listen "127.0.0.1:${RTID_ADMIN_PORT}" \
    --log-level info \
    --audit-replay-plugin event-journal \
    --log-dir "${RESULT_DIR}/logs" \
    --save-dir "${RESULT_DIR}/saves"
