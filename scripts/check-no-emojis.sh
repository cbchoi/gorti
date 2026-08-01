#!/usr/bin/env bash
# Reject emojis in source files (CODING_CONVENTIONS.md U-1).
set -euo pipefail

# Match common emoji ranges. This is a quick check, not exhaustive.
PATTERN=$'[\xE2\x98-\xE2\x9F\xF0\x9F\x8C-\xF0\x9F\x99]'

VIOLATIONS=()
for FILE in "$@"; do
  if [[ -f "$FILE" ]] && grep -lP "$PATTERN" "$FILE" >/dev/null 2>&1; then
    VIOLATIONS+=("$FILE")
  fi
done

if [[ ${#VIOLATIONS[@]} -gt 0 ]]; then
  echo "ERROR: emojis found in:"
  for F in "${VIOLATIONS[@]}"; do echo "  $F"; done
  echo "Per docs/CODING_CONVENTIONS.md U-1, no emojis in source."
  exit 1
fi
