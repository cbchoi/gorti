#!/usr/bin/env bash
# Go, Python and Java validation. Each stage can also be run on its own.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"
PYTHON="${PYTHON:-python}"
STAGES=("$@")
if [ "${#STAGES[@]}" -eq 0 ]; then
  STAGES=(stubs go python java static ivct)
fi

# Validate all stage names before invoking a compiler or generating files.
for stage in "${STAGES[@]}"; do
  case "$stage" in
    stubs|go|python|java|static|ivct) ;;
    *) printf 'Unknown stage: %s (use stubs go python java static ivct)\n' "$stage" >&2; exit 2 ;;
  esac
done

for stage in "${STAGES[@]}"; do
  printf '\n== %s ==\n' "$stage"
  case "$stage" in
    stubs)
      buf generate
      "${GORTI_MAVEN:-mvn}" --batch-mode --no-transfer-progress -f javasdk/pom.xml generate-sources
      ;;
    go)
      go build -o bin/rtid ./rti/cmd/rtid
      go vet ./rti/... ./tests/...
      go test -race -timeout 300s ./rti/... ./tests/...
      ;;
    python|java|ivct)
      options=()
      if [ -n "${GORTI_VALIDATION_ARTIFACTS:-}" ]; then
        options+=(--artifacts "$GORTI_VALIDATION_ARTIFACTS/$stage")
      fi
      "$PYTHON" scripts/validate_sdks.py "$stage" "${options[@]}"
      ;;
    static)
      "$PYTHON" scripts/check_release_metadata.py
      "$PYTHON" -m unittest discover -s scripts/tests -p 'test_validation_*.py' -v
      go test ./tests/architecture
      ;;
  esac
done
printf '\nci-gates: all selected stages passed\n'
