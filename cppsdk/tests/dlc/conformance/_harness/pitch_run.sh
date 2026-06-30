#!/usr/bin/env bash
# pitch_run.sh — start the Pitch CRC, run a Pitch-built fixture binary,
# capture the log. Used by `ctest -L parity`.
#
# Per docs/DLC_COMPLIANCE_PROGRAM.md §5.2 (single-layer parity). Opt-in via
# `PRTI_HOME` env var. Pin: Pitch pRTI Free 5.5.10 build 9905.
#
# Usage:
#   pitch_run.sh <pitch-built-binary> <log-output>
#
# Env:
#   PRTI_HOME             required — Pitch install root
#   PRTI_CRC_HOST_PORT    optional, default 127.0.0.1:8989
#   PRTI_CRC_STARTUP_S    optional, default 5 (settle delay)
#
# Exit codes:
#   0  Federate ran; log captured.
#   1  PRTI_HOME unset → caller treats this as SKIPPED.
#   2  CRC failed to start.
#   3  Federate process errored.

set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <pitch-built-binary> <log-output>" >&2
  exit 64
fi

BIN="$1"
LOG="$2"

if [ -z "${PRTI_HOME:-}" ]; then
  echo "PRTI_HOME unset — parity run SKIPPED" >&2
  exit 1
fi
if [ ! -x "$BIN" ]; then
  echo "$BIN not executable" >&2
  exit 64
fi

CRC_HOST_PORT="${PRTI_CRC_HOST_PORT:-127.0.0.1:8989}"
STARTUP_S="${PRTI_CRC_STARTUP_S:-5}"

# 1. Start the CRC headlessly.
#    Pitch ships bin/prti (the headless CRC) under $PRTI_HOME/bin.
CRC_BIN=""
for cand in "$PRTI_HOME/bin/prti" "$PRTI_HOME/bin/prtid"; do
  if [ -x "$cand" ]; then CRC_BIN="$cand"; break; fi
done
if [ -z "$CRC_BIN" ]; then
  echo "no prti binary under $PRTI_HOME/bin" >&2
  exit 2
fi

CRC_LOG="$(mktemp -t pitch-crc.XXXXXX.log)"
"$CRC_BIN" > "$CRC_LOG" 2>&1 &
CRC_PID=$!
trap "kill -TERM $CRC_PID 2>/dev/null || true; rm -f $CRC_LOG" EXIT

# Settle delay — CRC startup is non-deterministic on cold cache.
sleep "$STARTUP_S"

if ! kill -0 "$CRC_PID" 2>/dev/null; then
  echo "Pitch CRC failed to start; log at $CRC_LOG" >&2
  cat "$CRC_LOG" >&2 || true
  exit 2
fi

# 2. Run the federate; capture its log.
if ! "$BIN" --crc "$CRC_HOST_PORT" > "$LOG" 2>&1; then
  rc=$?
  echo "pitch_run.sh: federate exited non-zero ($rc); log at $LOG" >&2
  exit 3
fi

echo "pitch_run.sh: captured log at $LOG" >&2
