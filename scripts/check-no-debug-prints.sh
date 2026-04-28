#!/usr/bin/env bash
# Reject fmt.Println / print() / console.log in committed code (U-4).
set -euo pipefail

VIOLATIONS=()
for FILE in "$@"; do
  [[ -f "$FILE" ]] || continue
  case "$FILE" in
    *_test.go|*/tests/*|tests/*) continue ;;
    */scripts/*|scripts/*) continue ;;
    */examples/*) continue ;;
  esac
  case "$FILE" in
    *.go)
      if grep -nE '^[[:space:]]*fmt\.Print(ln|f)?\(' "$FILE" >/dev/null 2>&1; then
        VIOLATIONS+=("$FILE")
      fi
      ;;
    *.py)
      if grep -nE '^[[:space:]]*print\(' "$FILE" >/dev/null 2>&1; then
        VIOLATIONS+=("$FILE")
      fi
      ;;
  esac
done

if [[ ${#VIOLATIONS[@]} -gt 0 ]]; then
  echo "ERROR: debug print statements found in committed (non-test) code:"
  for F in "${VIOLATIONS[@]}"; do echo "  $F"; done
  echo "Per docs/CODING_CONVENTIONS.md U-4, use log/slog (Go) or logging (Python)."
  exit 1
fi
