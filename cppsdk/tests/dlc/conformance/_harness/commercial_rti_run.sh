#!/usr/bin/env bash
# Run a binary built against a locally licensed IEEE 1516.1-2010 RTI.
# The RTI server may already be online or may be launched through an explicit,
# provider-neutral executable supplied by the local environment.

set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <reference-rti-built-binary> <log-output>" >&2
  exit 64
fi

BIN="$1"
LOG="$2"
STARTUP_SECONDS="${REFERENCE_RTI_SERVER_STARTUP_SECONDS:-5}"
SERVER_EXECUTABLE="${REFERENCE_RTI_SERVER_EXECUTABLE:-}"
SERVER_PID=""
SERVER_LOG=""

if [ ! -x "$BIN" ]; then
  echo "$BIN is not executable" >&2
  exit 64
fi

cleanup() {
  if [ -n "$SERVER_PID" ]; then
    kill -TERM "$SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "$SERVER_LOG" ]; then
    rm -f "$SERVER_LOG"
  fi
}
trap cleanup EXIT

if [ -n "$SERVER_EXECUTABLE" ]; then
  if [ ! -x "$SERVER_EXECUTABLE" ]; then
    echo "REFERENCE_RTI_SERVER_EXECUTABLE is not executable: $SERVER_EXECUTABLE" >&2
    exit 2
  fi
  SERVER_LOG="$(mktemp -t reference-rti-server.XXXXXX.log)"
  read -r -a SERVER_ARGUMENTS <<< "${REFERENCE_RTI_SERVER_ARGUMENTS:-}"
  "$SERVER_EXECUTABLE" "${SERVER_ARGUMENTS[@]}" > "$SERVER_LOG" 2>&1 &
  SERVER_PID=$!
  sleep "$STARTUP_SECONDS"
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "reference RTI server failed to start; log at $SERVER_LOG" >&2
    exit 2
  fi
fi

read -r -a FEDERATE_ARGUMENTS <<< "${REFERENCE_RTI_FEDERATE_ARGUMENTS:-}"
if ! "$BIN" "${FEDERATE_ARGUMENTS[@]}" > "$LOG" 2>&1; then
  status=$?
  echo "commercial_rti_run.sh: federate exited non-zero ($status); log at $LOG" >&2
  exit 3
fi

echo "commercial_rti_run.sh: captured log at $LOG" >&2
