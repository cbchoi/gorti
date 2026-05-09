#!/usr/bin/env bash
# Launches the rtid daemon at well-known ports for manual federate
# runs. Run this in one terminal, then generator_run.sh /
# buffer_run.sh / processor_run.sh in others.
#
# The Python runner.py allocates free ports per run; this script
# uses fixed ports so the federate scripts can dial them without a
# registry. If something else is already on the port, override:
#
#   RTID_LISTEN_PORT=9000 ./rtid_run.sh

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
echo "rtid_run: admin 127.0.0.1:${RTID_ADMIN_PORT}, metrics :${RTID_METRICS_PORT}" >&2
echo "rtid_run: save_dir=${SAVE_DIR}  log_dir=${LOG_DIR}" >&2
exec "${RTID_BINARY}" \
    --listen ":${RTID_LISTEN_PORT}" \
    --metrics-listen ":${RTID_METRICS_PORT}" \
    --admin-listen "127.0.0.1:${RTID_ADMIN_PORT}" \
    --log-level info \
    --log-dir "${LOG_DIR}" \
    --save-dir "${SAVE_DIR}"
