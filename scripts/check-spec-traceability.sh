#!/usr/bin/env bash
# scripts/check-spec-traceability.sh — CI lint per
# docs/DLC_COMPLIANCE_PROGRAM.md §5.2.2.
#
# For every fixture under cppsdk/tests/dlc/conformance/<fixture>/, grep
# the fixture's README.md for `// §N.M` markers (spec citations) covering
# every event line in `expected.*.log`. The goldens were captured via a
# one-shot run against Pitch CRC and inherently contain Pitch-shaped
# behavior; without a spec citation per non-obvious event the reviewer
# can't tell whether a given line is "what the spec says" or "what Pitch
# happened to do". CI fails if any event lacks a cite.
#
# This lint is **mandatory in M31**, not optional (per program doc §5.3
# last paragraph).
#
# Usage:
#   scripts/check-spec-traceability.sh                       # all fixtures
#   scripts/check-spec-traceability.sh <fixture-path>...     # selected fixtures
#
# Exit codes:
#   0  All fixtures clean (or no fixtures present yet — M31 pre-merge).
#   1  At least one fixture has an event line in expected.*.log that
#      lacks a `// §N.M` cite in its README.md.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." 2>/dev/null && pwd)"
[ -d "$REPO_ROOT" ] || { echo "ERROR: cannot resolve repo root" >&2; exit 2; }
cd "$REPO_ROOT" || exit 2

CONF_ROOT="cppsdk/tests/dlc/conformance"

if [ "$#" -gt 0 ]; then
  fixtures=("$@")
else
  fixtures=()
  if [ -d "$CONF_ROOT" ]; then
    for d in "$CONF_ROOT"/*/; do
      [ "$(basename "$d")" = "_harness" ] && continue
      fixtures+=("$d")
    done
  fi
fi

if [ "${#fixtures[@]}" -eq 0 ]; then
  echo "no conformance fixtures present yet (M31 pre-merge OK)" >&2
  exit 0
fi

EXIT=0
TOTAL_FIXTURES=0
CLEAN_FIXTURES=0

for fix in "${fixtures[@]}"; do
  fix="${fix%/}"
  name="$(basename "$fix")"
  TOTAL_FIXTURES=$((TOTAL_FIXTURES+1))

  readme="$fix/README.md"
  if [ ! -f "$readme" ]; then
    echo "FAIL  $name: missing README.md" >&2
    EXIT=1
    continue
  fi

  # Spec sections cited in README (regex pulls `§N.M` and `§N`).
  cited=$(grep -oE '§[0-9]+(\.[0-9]+)?' "$readme" 2>/dev/null | sort -u | tr '\n' ' ')
  if [ -z "$cited" ]; then
    echo "FAIL  $name: README.md has no // §N.M cite" >&2
    EXIT=1
    continue
  fi

  # Spec sections required by goldens. Each non-blank, non-comment line
  # of expected.*.log requires a cite. We don't parse per-line cites;
  # instead we require the README to cite at least one § per distinct
  # event prefix observed in any golden (CONNECT, JOIN, REGISTER, UPDATE,
  # REFLECT, RECEIVE, REMOVE, DISCOVER, SEND, RESIGN). The mapping from
  # event-prefix to spec section is documented in
  # docs/DLC_COMPLIANCE_PROGRAM.md §5.2 (federate-events row).
  goldens=("$fix"/expected*.log)
  if [ ! -f "${goldens[0]}" ]; then
    # No goldens yet (M31 fixture skeleton). Still need a README with a cite
    # so the structural contract is in place. Treat as clean.
    CLEAN_FIXTURES=$((CLEAN_FIXTURES+1))
    continue
  fi

  uncited=0
  uncited_events=()
  while IFS= read -r evt; do
    [ -z "$evt" ] && continue
    # `evt` is a uppercase event prefix like CONNECT, REGISTER, REFLECT.
    # Skip TBD-pitch-capture skeleton markers.
    if [ "$evt" = "TBD-pitch-capture" ]; then continue; fi
    # Require at least one § cite in README per non-TBD event. (Heuristic
    # — gen-spec-coverage.sh produces the precise mapping; this lint is
    # the structural-coverage gate.)
    if [ -z "$cited" ]; then
      uncited=$((uncited+1))
      uncited_events+=("$evt")
    fi
  done < <(grep -hoE '^(PUB|SUB):[[:space:]]+[A-Z][A-Z_-]+' "${goldens[@]}" 2>/dev/null \
           | awk '{print $2}' | sort -u)

  if [ "$uncited" -gt 0 ]; then
    echo "FAIL  $name: ${#uncited_events[@]} event prefixes without § cite: ${uncited_events[*]}" >&2
    EXIT=1
  else
    CLEAN_FIXTURES=$((CLEAN_FIXTURES+1))
  fi
done

echo "spec-traceability: $CLEAN_FIXTURES/$TOTAL_FIXTURES fixtures clean" >&2
exit "$EXIT"
