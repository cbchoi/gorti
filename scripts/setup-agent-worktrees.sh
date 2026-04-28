#!/usr/bin/env bash
# Initialize git worktrees for the three coding agents.
#
# Each agent gets its own filesystem location, sibling to the orchestrator's
# main checkout. This is the working-directory isolation backstop required by
# docs/ORTHOGONALITY.md §1.4.
#
# Run from the orchestrator's main checkout AFTER:
#   1. `git init` is done.
#   2. The `main` branch has at least one commit.
#
# Idempotent: re-running skips worktrees that already exist.
set -euo pipefail

ORCHESTRATOR_DIR="$(git rev-parse --show-toplevel)"
PARENT_DIR="$(dirname "$ORCHESTRATOR_DIR")"
PROJECT_NAME="$(basename "$ORCHESTRATOR_DIR")"

if ! git rev-parse --verify main >/dev/null 2>&1; then
  echo "ERROR: 'main' branch does not exist. Commit at least once on main first."
  exit 1
fi

for AGENT in a b c; do
  AGENT_DIR="$PARENT_DIR/${PROJECT_NAME}-agent-$AGENT"
  AGENT_BRANCH="agent/$AGENT/scratch"

  if [ -d "$AGENT_DIR" ]; then
    echo "[skip] $AGENT_DIR already exists"
    continue
  fi

  if ! git show-ref --verify --quiet "refs/heads/$AGENT_BRANCH"; then
    git branch "$AGENT_BRANCH" main
    echo "[create] branch $AGENT_BRANCH from main"
  fi

  git worktree add "$AGENT_DIR" "$AGENT_BRANCH"
  echo "[create] worktree $AGENT_DIR on $AGENT_BRANCH"
done

echo
echo "Worktree summary:"
git worktree list
echo
echo "Agent sandboxes should be configured to operate ONLY in their respective directory:"
echo "  claude-sandbox  -> $PARENT_DIR/${PROJECT_NAME}-agent-a/"
echo "  codex-sandbox   -> $PARENT_DIR/${PROJECT_NAME}-agent-b/"
echo "  gemini-sandbox  -> $PARENT_DIR/${PROJECT_NAME}-agent-c/"
echo
echo "Orchestrator continues working in:"
echo "  $ORCHESTRATOR_DIR/"
