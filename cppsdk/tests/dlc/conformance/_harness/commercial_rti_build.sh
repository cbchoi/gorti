#!/usr/bin/env bash
# Compile a conformance fixture against a locally licensed IEEE 1516.1-2010
# C++ API. Provider-specific paths remain outside version control.

set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <fixture-dir> <output-binary>" >&2
  exit 64
fi

FIXTURE_DIR="$1"
OUT="$2"
INCLUDE_DIR="${REFERENCE_RTI_INCLUDE_DIR:-}"
LIBRARY_DIR="${REFERENCE_RTI_LIBRARY_DIR:-}"
LIBRARIES="${REFERENCE_RTI_LIBRARIES:-}"

if [ -z "$INCLUDE_DIR" ] || [ -z "$LIBRARY_DIR" ] || [ -z "$LIBRARIES" ]; then
  echo "REFERENCE_RTI_INCLUDE_DIR, REFERENCE_RTI_LIBRARY_DIR, and REFERENCE_RTI_LIBRARIES are required; parity build SKIPPED" >&2
  exit 1
fi
if [ ! -d "$INCLUDE_DIR/RTI" ]; then
  echo "REFERENCE_RTI_INCLUDE_DIR does not contain RTI headers: $INCLUDE_DIR" >&2
  exit 2
fi
if [ ! -d "$LIBRARY_DIR" ]; then
  echo "REFERENCE_RTI_LIBRARY_DIR does not exist: $LIBRARY_DIR" >&2
  exit 2
fi

SOURCES=()
for source in "$FIXTURE_DIR"/federate*.cpp; do
  [ -f "$source" ] && SOURCES+=("$source")
done
if [ "${#SOURCES[@]}" -eq 0 ]; then
  echo "no federate*.cpp in $FIXTURE_DIR" >&2
  exit 3
fi

read -r -a LINK_LIBRARIES <<< "$LIBRARIES"
g++ -std=c++17 \
  -I "$INCLUDE_DIR" \
  -L "$LIBRARY_DIR" \
  -Wl,-rpath,"$LIBRARY_DIR" \
  "${SOURCES[@]}" \
  -o "$OUT" \
  "${LINK_LIBRARIES[@]}" 2>&1 || {
    status=$?
    echo "commercial_rti_build.sh: compilation failed (exit $status)" >&2
    exit 3
  }

echo "commercial_rti_build.sh: built $OUT against the configured IEEE 1516.1-2010 API" >&2
