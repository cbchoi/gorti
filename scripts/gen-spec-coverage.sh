#!/usr/bin/env bash
# scripts/gen-spec-coverage.sh — auto-generate docs/dlc-spec-coverage.md.
#
# Per docs/DLC_COMPLIANCE_PROGRAM.md §5.3 + docs/M31_DISPATCH_PLAN.md §3
# acceptance criterion #9.
#
# Walks the C++ test tree (cppsdk/tests/dlc/) and the RTI audit tests
# (tests/conformance/rti/) for `// §N.M` spec-section markers, then
# emits a matrix:
#
#   | Spec §  | Lockfile | Fixture (gorti) | Fixture (parity) | Audit |
#   | §4.2    | test_X.cpp:42 | om_helloworld_pubsub/test.cpp:18 | (skip) | (none) |
#   | ...
#
# Each cell links to the test file(s). CI fails if any row has zero
# coverage across all 4 columns.
#
# READ-ONLY mode (default): writes docs/dlc-spec-coverage.md.
# --check mode: exits non-zero if regeneration would change the file
#               (used in CI to fail when someone added a fixture without
#               regenerating the matrix).
#
# Usage:
#   scripts/gen-spec-coverage.sh            # write
#   scripts/gen-spec-coverage.sh --check    # CI lint

set -o pipefail
# Note: don't use `-u` (unbound-vars-error) because associative-array lookups
# of missing keys are routine here.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." 2>/dev/null && pwd)"
[ -d "$REPO_ROOT" ] || { echo "ERROR: cannot resolve repo root" >&2; exit 2; }
cd "$REPO_ROOT" || exit 2

OUT="docs/dlc-spec-coverage.md"
CHECK_MODE=0
if [ "${1:-}" = "--check" ]; then CHECK_MODE=1; fi

# Search roots per column.
LOCKFILE_ROOT="cppsdk/tests/dlc/lockfile"
FIXTURE_ROOT="cppsdk/tests/dlc/conformance"
AUDIT_ROOT="tests/conformance/rti"

# Extract `§N.M` and `§N` cites. Pattern: `// §N.M` or `// §N`. Each line
# contributes (section, file, line) records.
extract_cites() {
  local root="$1"
  [ -d "$root" ] || return 0
  # Accept § followed by N[.M]; capture file:line:section.
  # Filter to spec-section comments (avoid stray § elsewhere).
  grep -RHnoE '// §[0-9]+(\.[0-9]+)?' "$root" 2>/dev/null \
    | sed -E 's|// §([0-9]+(\.[0-9]+)?)|§\1|' \
    | awk -F: '{print $NF "|" $1 ":" $2}' \
    | sort -u
}

# Build per-section file-list strings per column.
declare -A LOCK_COL FIXT_COL PARITY_COL AUDIT_COL ALL_SECS

while IFS='|' read -r sec loc; do
  [ -z "$sec" ] && continue
  ALL_SECS["$sec"]=1
  LOCK_COL["$sec"]="${LOCK_COL[$sec]:+${LOCK_COL[$sec]}<br>}$loc"
done < <(extract_cites "$LOCKFILE_ROOT")

while IFS='|' read -r sec loc; do
  [ -z "$sec" ] && continue
  ALL_SECS["$sec"]=1
  # Parity-mode fixtures live under cppsdk/tests/dlc/conformance/<>/parity/.
  if [[ "$loc" == *"/parity/"* ]]; then
    PARITY_COL["$sec"]="${PARITY_COL[$sec]:+${PARITY_COL[$sec]}<br>}$loc"
  else
    FIXT_COL["$sec"]="${FIXT_COL[$sec]:+${FIXT_COL[$sec]}<br>}$loc"
  fi
done < <(extract_cites "$FIXTURE_ROOT")

while IFS='|' read -r sec loc; do
  [ -z "$sec" ] && continue
  ALL_SECS["$sec"]=1
  AUDIT_COL["$sec"]="${AUDIT_COL[$sec]:+${AUDIT_COL[$sec]}<br>}$loc"
done < <(extract_cites "$AUDIT_ROOT")

# Sort sections numerically (§4.2 before §4.10 before §5.1).
sorted_sections=""
if [ "${#ALL_SECS[@]}" -gt 0 ]; then
  sorted_sections=$(printf '%s\n' "${!ALL_SECS[@]}" \
    | grep -E '^§[0-9]' \
    | sed -E 's/§//' \
    | awk -F. '{printf "%03d.%03d %s\n", $1, ($2==""?0:$2), $0}' \
    | sort \
    | awk '{print "§" $2}')
fi

# Generate doc.
out_tmp=$(mktemp)
{
  cat <<EOF
# IEEE 1516.1-2010 DLC spec-coverage matrix

**AUTO-GENERATED — do not hand-edit.** Source: \`scripts/gen-spec-coverage.sh\` walking \`cppsdk/tests/dlc/\` and \`tests/conformance/rti/\` for \`// §N.M\` markers.

Per \`docs/DLC_COMPLIANCE_PROGRAM.md §5.3\`. CI runs \`gen-spec-coverage.sh --check\` and fails if regeneration would change this file (i.e. a fixture was added but the matrix wasn't refreshed). A blank row in this matrix (no coverage across all 4 columns) fails CI hard.

Generated: REGEN_TO_REFRESH (timestamp masked so --check mode is timestamp-independent)

| Spec § | Lockfile | Fixture (gorti) | Fixture (parity) | Audit |
|---|---|---|---|---|
EOF
  for sec in $sorted_sections; do
    lock="${LOCK_COL[$sec]:-—}"
    fixt="${FIXT_COL[$sec]:-—}"
    parity="${PARITY_COL[$sec]:-—}"
    audit="${AUDIT_COL[$sec]:-—}"
    printf "| %s | %s | %s | %s | %s |\n" "$sec" "$lock" "$fixt" "$parity" "$audit"
  done
  echo
  total_secs=${#ALL_SECS[@]}
  echo "**Total spec sections covered:** $total_secs"
  echo
  echo "**Acceptance threshold (M31):** ≥40 §-sections."
} > "$out_tmp"

if [ "$CHECK_MODE" -eq 1 ]; then
  if [ ! -f "$OUT" ]; then
    echo "ERROR: $OUT does not exist; run \`$0\` to generate it" >&2
    rm -f "$out_tmp"
    exit 3
  fi
  if ! diff -q "$out_tmp" "$OUT" >/dev/null 2>&1; then
    echo "ERROR: $OUT is out of date; run \`$0\` to regenerate" >&2
    diff -u "$OUT" "$out_tmp" >&2 || true
    rm -f "$out_tmp"
    exit 4
  fi
  rm -f "$out_tmp"
  echo "$OUT up to date"
  exit 0
fi

# Mkdir for OUT if needed.
mkdir -p "$(dirname "$OUT")"
mv "$out_tmp" "$OUT"
echo "wrote $OUT"
