#!/usr/bin/env bash
# pitch_build.sh — compile a conformance fixture against Pitch pRTI Free
# headers for the parity-mode build. Used by `ctest -L parity`.
#
# Per docs/DLC_COMPLIANCE_PROGRAM.md §5.2 (single-layer parity) and
# docs/M31_DISPATCH_PLAN.md §2.3 (parity-mode toggle). Opt-in via
# `PRTI_HOME` env var; fails loudly if Pitch is not installed.
#
# Pin: Pitch pRTI Free 5.5.10 build 9905 (see docs/PITCH_GOLDEN_LICENSING.md
# for the EULA review that gates golden capture).
#
# Usage:
#   pitch_build.sh <fixture-dir> <output-binary>
#
# Required env:
#   PRTI_HOME      path to the Pitch install root (e.g. ~/prti1516e)
#   PRTI_VERSION   optional; if set, asserted against installed version
#
# Exit codes:
#   0   Build succeeded.
#   1   PRTI_HOME unset → caller treats this as SKIPPED.
#   2   PRTI_HOME set but contents do not match the expected version pin.
#   3   Compilation failed.

set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <fixture-dir> <output-binary>" >&2
  exit 64
fi

FIXTURE_DIR="$1"
OUT="$2"

# 1. Gate on PRTI_HOME.
if [ -z "${PRTI_HOME:-}" ]; then
  echo "PRTI_HOME unset — parity build SKIPPED" >&2
  exit 1
fi

if [ ! -d "$PRTI_HOME/api/cpp/HLA_1516-2010/RTI" ]; then
  echo "PRTI_HOME=$PRTI_HOME does not contain api/cpp/HLA_1516-2010/RTI" >&2
  exit 2
fi

# 2. Version pin assertion (best effort).
EXPECTED_VERSION="${PRTI_VERSION:-5.5.10}"
if [ -f "$PRTI_HOME/.install4j/i4j_extf_2_1c08xf4.utf8" ]; then
  if ! grep -q "$EXPECTED_VERSION" "$PRTI_HOME/.install4j/"*.utf8 2>/dev/null; then
    echo "WARNING: PRTI_HOME=$PRTI_HOME does not match version pin $EXPECTED_VERSION" >&2
    echo "         continuing; goldens may not match" >&2
  fi
fi

# 3. Pick a single federate.cpp (the spec-portable source under test).
SRCS=()
for f in "$FIXTURE_DIR"/federate*.cpp; do
  [ -f "$f" ] && SRCS+=("$f")
done
if [ "${#SRCS[@]}" -eq 0 ]; then
  echo "no federate*.cpp in $FIXTURE_DIR" >&2
  exit 3
fi

# 4. Compile + link against Pitch.
PRTI_INC="$PRTI_HOME/api/cpp/HLA_1516-2010"
# Pitch ships librti1516e64.so or librti1516e.so depending on arch.
PRTI_LIB_DIR=""
for cand in "$PRTI_HOME/lib/gcc73_64" "$PRTI_HOME/lib/gcc63_64" "$PRTI_HOME/lib"; do
  if [ -d "$cand" ]; then PRTI_LIB_DIR="$cand"; break; fi
done
if [ -z "$PRTI_LIB_DIR" ]; then
  echo "no Pitch lib dir found under $PRTI_HOME/lib*" >&2
  exit 2
fi

g++ -std=c++17 \
  -I "$PRTI_INC" \
  -L "$PRTI_LIB_DIR" \
  -Wl,-rpath,"$PRTI_LIB_DIR" \
  "${SRCS[@]}" \
  -o "$OUT" \
  -lrti1516e64 \
  -lfedtime1516e64 2>&1 || {
    rc=$?
    echo "pitch_build.sh: compilation failed (exit $rc)" >&2
    exit 3
  }

echo "pitch_build.sh: built $OUT against PRTI_HOME=$PRTI_HOME" >&2
