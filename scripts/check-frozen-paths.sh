#!/usr/bin/env bash
# Reject writes to frozen paths from agent branches.
# See docs/AGENTS.md §4 for the canonical list.
set -euo pipefail

BRANCH="$(git rev-parse --abbrev-ref HEAD)"

# Orchestrator branches may write anywhere.
case "$BRANCH" in
  main|release/*|hotfix/*|orchestrator/*) exit 0 ;;
esac

# Agent branches must match the namespace.
case "$BRANCH" in
  agent/a/*|agent/b/*|agent/c/*) ;;
  *)
    echo "WARN: branch '$BRANCH' is neither orchestrator nor agent namespace; skipping frozen-paths check"
    exit 0
    ;;
esac

FROZEN_PATTERNS=(
  '^proto/'
  '^rti/internal/core/'
  '^docs/AGENTS\.md$'
  '^docs/srs\.md$'
  '^docs/sdd\.md$'
  '^docs/idd\.md$'
  '^docs/CODING_CONVENTIONS\.md$'
  '^docs/TDD\.md$'
  '^docs/WORKFLOW\.md$'
  '^docs/ORTHOGONALITY\.md$'
  '^docs/DISPATCH\.md$'
  '^docs/agent-(a-rti-core|b-fom-encoding|c-pysdk)\.md$'
  '^docs/templates/'
  '^docs/tasks/'
  '^tests/spec/'
  '^scripts/'
  '^Makefile$'
  '^buf\.(yaml|gen\.yaml)$'
  '^\.golangci\.yml$'
  '^ruff\.toml$'
  '^\.pre-commit-config\.yaml$'
  '^\.github/'
  '^LICENSE$'
  '^README\.md$'
  '^CHANGELOG\.md$'
  '^CHANGELOG-MASTERPLAN\.md$'
)

# Files explicitly allowed even when they live under otherwise-frozen paths.
# Sentinels under docs/tasks/signals/ are the agent's "task done" signal; see
# docs/tasks/signals/README.md and docs/DISPATCH.md §10.
ALLOW_PATTERNS=(
  '^docs/tasks/signals/TASK-[0-9]+\.done$'
)

VIOLATIONS=()
while IFS= read -r FILE; do
  ALLOWED=0
  for PATTERN in "${ALLOW_PATTERNS[@]}"; do
    if [[ "$FILE" =~ $PATTERN ]]; then
      ALLOWED=1
      break
    fi
  done
  if [[ $ALLOWED -eq 1 ]]; then
    continue
  fi

  for PATTERN in "${FROZEN_PATTERNS[@]}"; do
    if [[ "$FILE" =~ $PATTERN ]]; then
      VIOLATIONS+=("$FILE")
      break
    fi
  done
done < <(git diff --cached --name-only --diff-filter=ACMRT)

if [[ ${#VIOLATIONS[@]} -gt 0 ]]; then
  echo "ERROR: branch '$BRANCH' attempted to write frozen paths:"
  for F in "${VIOLATIONS[@]}"; do echo "  $F"; done
  echo
  echo "Frozen paths can only be edited by the orchestrator. Open a"
  echo "'contract-change-request:' issue per docs/WORKFLOW.md §4.2."
  exit 1
fi
