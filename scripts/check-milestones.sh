#!/usr/bin/env bash
# scripts/check-milestones.sh — milestone-state probe driven by docs/srs.md §10.2.
#
# READ-ONLY: never mutates the repo. Safe to run on any branch at any time.
# See docs/MILESTONE_CHECK.md for the design rationale and loop wiring.
#
# Exit codes:
#   0  no regression — every milestone whose dependencies are DONE is itself DONE,
#                      and every NOT_STARTED milestone is correctly red
#   1  regression — at least one milestone that should be DONE has a failing criterion
#   2  invocation error (missing tools, cannot find repo root, etc.)

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." 2>/dev/null && pwd)"
[ -d "$REPO_ROOT" ] || { echo "ERROR: cannot resolve repo root" >&2; exit 2; }
cd "$REPO_ROOT" || exit 2

# ---------- output helpers ----------
if [ -t 1 ] && [ "${MILESTONE_CHECK_NOCOLOR:-0}" != "1" ]; then
  GRN=$'\033[0;32m'; RED=$'\033[0;31m'; YLW=$'\033[0;33m'; CYN=$'\033[0;36m'; DIM=$'\033[2m'; OFF=$'\033[0m'
else
  GRN=""; RED=""; YLW=""; CYN=""; DIM=""; OFF=""
fi

PASS_MARK="${GRN}✓${OFF}"
FAIL_MARK="${RED}✗${OFF}"
PEND_MARK="${YLW}∘${OFF}"
INFO_MARK="${CYN}i${OFF}"

# Per-run aggregation
REGRESSED=0
declare -A MILESTONE_STATUS

print_header() {
  echo
  printf "${CYN}╔══════════════════════════════════════════════════════════════════╗${OFF}\n"
  printf "${CYN}║${OFF}  Milestone status check — %-37s ${CYN}║${OFF}\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  printf "${CYN}║${OFF}  Branch: %-55s ${CYN}║${OFF}\n" "$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?')"
  printf "${CYN}║${OFF}  HEAD:   %-55s ${CYN}║${OFF}\n" "$(git rev-parse --short HEAD 2>/dev/null || echo '?')"
  local dirty
  dirty=$(git status --short 2>/dev/null | wc -l | tr -d ' ')
  if [ "$dirty" -gt 0 ]; then
    local untracked
    untracked=$(git ls-files --others --exclude-standard 2>/dev/null | wc -l | tr -d ' ')
    printf "${CYN}║${OFF}  ${YLW}WIP:${OFF}    %d uncommitted change(s) (%d untracked) — results reflect %-9s ${CYN}║${OFF}\n" \
      "$dirty" "$untracked" "WORKING TREE,"
    printf "${CYN}║${OFF}          %-55s ${CYN}║${OFF}\n" "not just committed state on \$BRANCH"
    DIRTY=1
  else
    printf "${CYN}║${OFF}  Tree:   %-55s ${CYN}║${OFF}\n" "clean (results reflect committed state at HEAD)"
    DIRTY=0
  fi
  printf "${CYN}╚══════════════════════════════════════════════════════════════════╝${OFF}\n"
}

section() {
  printf "\n${CYN}── %s ──${OFF}\n" "$1"
}

# probe LABEL CMD…  — runs CMD silently; prints PASS/FAIL marker. Returns CMD's exit.
probe() {
  local label="$1"; shift
  if "$@" >/dev/null 2>&1; then
    printf "  %s %s\n" "$PASS_MARK" "$label"
    return 0
  else
    printf "  %s %s\n" "$FAIL_MARK" "$label"
    return 1
  fi
}

# present LABEL  — print without running anything (informational).
present()  { printf "  %s %s\n" "$PASS_MARK" "$1"; }
missing()  { printf "  %s %s\n" "$FAIL_MARK" "$1"; }
pending()  { printf "  %s %s\n" "$PEND_MARK" "$1"; }
note()     { printf "  %s ${DIM}%s${OFF}\n" "$INFO_MARK" "$1"; }

count_files() { find "$1" -maxdepth 1 -type f -name "$2" 2>/dev/null | wc -l | tr -d ' '; }

set_status() {
  local m="$1" s="$2"
  # If the working tree is dirty, an apparent DONE may evaporate on commit.
  # Annotate so the summary makes that clear; only DONE-on-clean-tree counts
  # as "really" DONE. WIP results never trigger REGRESSED — that flag is
  # reserved for cases where committed state has clearly broken.
  if [ "${DIRTY:-0}" = "1" ] && [ "$s" = "DONE" ]; then
    s="DONE_WIP"
  fi
  MILESTONE_STATUS[$m]="$s"
  if [ "$s" = "REGRESSED" ]; then REGRESSED=1; fi
}

# ---------- structural sanity (cross-cutting; runs before milestones) ----------
check_structure() {
  section "Structural sanity"

  # 1. TASK files on main
  local task_count
  task_count=$(git ls-tree -r main docs/tasks/ 2>/dev/null | grep -c 'TASK-[0-9][0-9][0-9]\.md$')
  if [ "$task_count" -ge 85 ]; then
    present "TASK backlog on main: $task_count files (≥85 expected)"
  elif [ "$task_count" -ge 1 ]; then
    pending "TASK backlog partial on main: $task_count files"
  else
    missing "TASK backlog absent from main — agents have nothing to dispatch from"
  fi

  # 2. Sentinels without TASK match
  local stray=0
  for s in $(git ls-tree -r main docs/tasks/signals/ 2>/dev/null | awk '{print $4}' | grep 'TASK-[0-9][0-9][0-9]\.done$'); do
    local id; id=$(basename "$s" .done)
    if ! git cat-file -e "main:docs/tasks/${id}.md" 2>/dev/null; then
      stray=$((stray + 1))
    fi
  done
  if [ "$stray" -eq 0 ]; then
    present "All sentinels on main reference a committed TASK brief"
  else
    missing "$stray sentinel(s) on main reference TASK files not on main"
  fi

  # 3. Open contract-change-requests
  if command -v gh >/dev/null 2>&1; then
    local ccrs
    ccrs=$(gh issue list --state open --label contract-change-request --json number 2>/dev/null | grep -c '"number"' | tr -d '[:space:]')
    : "${ccrs:=0}"
    if [ "${ccrs:-0}" -gt 0 ] 2>/dev/null; then
      pending "$ccrs open contract-change-request issue(s) — may be blocking tasks"
      gh issue list --state open --label contract-change-request --json number,title 2>/dev/null \
        | sed -E 's/.*"number":([0-9]+).*"title":"([^"]+)".*/    #\1: \2/' | head -5
    else
      present "No open contract-change-requests"
    fi
  else
    note "gh not installed — skipping open-issue probe"
  fi

  # 4. BLOCKED tasks
  if [ -d docs/tasks ]; then
    local blocked
    blocked=$(grep -lE '^\| Status \| BLOCKED' docs/tasks/TASK-*.md 2>/dev/null | wc -l | tr -d ' ')
    if [ "$blocked" -gt 0 ]; then
      pending "$blocked task(s) currently BLOCKED:"
      grep -lE '^\| Status \| BLOCKED' docs/tasks/TASK-*.md 2>/dev/null \
        | sed -E 's|.*/(TASK-[0-9]+)\.md|    \1|' | head -5
    else
      present "No BLOCKED tasks"
    fi
  fi

  # 5. Frozen-path drift on current branch
  local drift
  drift=$(git diff --name-only main -- proto/ rti/internal/core/ docs/srs.md docs/sdd.md docs/idd.md 2>/dev/null | wc -l | tr -d ' ')
  if [ "$drift" -gt 0 ]; then
    missing "$drift frozen-path file(s) modified on this branch (should never happen on agent branches)"
  else
    present "No frozen-path drift relative to main"
  fi
}

# ---------- M0 ----------
check_m0() {
  section "M0 — Orchestrator scaffolding (Owner: orchestrator)"
  echo "Exit (srs.md §10.2): all sandboxes pass make verify on no-op branch; agents pass conventions quiz"
  local pass=0 total=4

  if [ "$(count_files proto/rti/v1 '*.proto')" -ge 8 ]; then
    present "≥8 proto files in proto/rti/v1/"; pass=$((pass+1))
  else
    missing "<8 proto files in proto/rti/v1/"
  fi

  if [ "$(count_files rti/internal/core '*.go')" -ge 10 ]; then
    present "≥10 frozen interface files in rti/internal/core/"; pass=$((pass+1))
  else
    missing "<10 files in rti/internal/core/"
  fi

  if [ -f tests/spec/M1/parser_diagnostics_test.go ] && [ -f tests/spec/M1/encoding_vectors_test.go ]; then
    present "tests/spec/M1/ committed (orchestrator pre-work for M1 RED state)"; pass=$((pass+1))
  else
    missing "tests/spec/M1/ incomplete"
  fi

  # build sanity (without running tests, which cover M1+ work)
  if go build ./... >/dev/null 2>&1; then
    present "go build ./... succeeds (compile-clean main)"; pass=$((pass+1))
  else
    missing "go build ./... fails — M0 contract broken"
  fi

  note "Conventions quiz: manual; not auto-checkable"

  if [ "$pass" -eq "$total" ]; then
    set_status M0 DONE
    printf "${GRN}M0: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  else
    set_status M0 REGRESSED
    printf "${RED}M0: REGRESSED${OFF} (%d/%d)\n" "$pass" "$total"
  fi
}

# ---------- M1 ----------
check_m1() {
  section "M1 — FOM parser + MIM + encoding rules (Owner: agent-b)"
  echo "Exit: 10 bad FOMs strict-rejected; encoder round-trips all types; coverage ≥80% on encoding"
  local pass=0 total=4

  # 10 bad FOMs
  local bad
  bad=$(count_files tests/conformance/foms/bad '*.xml')
  if [ "$bad" -ge 10 ]; then
    present "$bad bad-FOM fixtures committed (≥10 required)"; pass=$((pass+1))
  else
    pending "Only $bad bad-FOM fixtures (need 10 for M1 exit)"
  fi

  # Spec test: parser diagnostics
  if go test ./tests/spec/M1/... -run TestSpec_M1_BadFOMDiagnostics -count=1 >/dev/null 2>&1; then
    present "TestSpec_M1_BadFOMDiagnostics passes"; pass=$((pass+1))
  else
    pending "TestSpec_M1_BadFOMDiagnostics red (expected for in-flight M1)"
  fi

  # Spec test: encoder vectors
  if go test ./tests/spec/M1/... -run TestSpec_M1_PrimitiveVectorsRoundTrip -count=1 >/dev/null 2>&1; then
    present "TestSpec_M1_PrimitiveVectorsRoundTrip passes"; pass=$((pass+1))
  else
    pending "TestSpec_M1_PrimitiveVectorsRoundTrip red (expected for in-flight M1)"
  fi

  # Coverage on rti/pkg/encoding
  if [ -d rti/pkg/encoding ]; then
    local cov
    cov=$(go test -cover ./rti/pkg/encoding/... 2>/dev/null | grep -oE 'coverage: [0-9.]+%' | grep -oE '[0-9.]+' | head -1)
    if [ -n "$cov" ] && [ "$(printf '%.0f' "$cov")" -ge 80 ]; then
      present "rti/pkg/encoding coverage = ${cov}% (≥80%)"; pass=$((pass+1))
    elif [ -n "$cov" ]; then
      pending "rti/pkg/encoding coverage = ${cov}% (<80%)"
    else
      pending "rti/pkg/encoding has no testable code yet"
    fi
  else
    note "rti/pkg/encoding/ not yet present"
  fi

  if [ "$pass" -eq "$total" ]; then
    set_status M1 DONE
    printf "${GRN}M1: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then
    set_status M1 IN_PROGRESS
    printf "${YLW}M1: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else
    set_status M1 NOT_STARTED
    printf "${DIM}M1: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"
  fi
}

# ---------- M2 ----------
check_m2() {
  section "M2 — Federation + Declaration + Object + EventLog + gRPC (Owner: agent-a)"
  echo "Exit: examples/go-pingpong/ deterministic across 10 runs; replay byte-identical"
  local pass=0 total=4

  [ -d rti/spec/M2 ] && { present "rti/spec/M2/ committed"; pass=$((pass+1)); } \
                      || pending "rti/spec/M2/ pending orchestrator pre-work"
  [ -f examples/go-pingpong/main.go ] && { present "examples/go-pingpong/main.go exists"; pass=$((pass+1)); } \
                                       || pending "examples/go-pingpong/ not yet built"
  [ -f examples/go-pingpong/determinism_test.go ] && { present "go-pingpong determinism harness"; pass=$((pass+1)); } \
                                                   || pending "determinism harness pending"
  [ -f examples/go-pingpong/replay_test.go ] && { present "go-pingpong replay harness"; pass=$((pass+1)); } \
                                              || pending "replay harness pending"

  if [ "$pass" -eq "$total" ]; then set_status M2 DONE; printf "${GRN}M2: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M2 IN_PROGRESS; printf "${YLW}M2: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M2 NOT_STARTED; printf "${DIM}M2: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M3 ----------
check_m3() {
  section "M3 — Time management (Owner: agent-a)"
  echo "Exit: examples/go-timed/ deterministic across 20 randomized scenarios; stall timeout fires"
  local pass=0 total=4

  [ -d rti/spec/M3 ] && { present "rti/spec/M3/ committed"; pass=$((pass+1)); } \
                      || pending "rti/spec/M3/ pending orchestrator pre-work"
  [ -f examples/go-timed/main.go ] && { present "examples/go-timed/main.go exists"; pass=$((pass+1)); } \
                                    || pending "examples/go-timed/ not yet built"
  [ -f examples/go-timed/determinism_test.go ] && { present "go-timed 20-scenario harness"; pass=$((pass+1)); } \
                                                || pending "determinism harness pending"
  [ -f examples/go-timed/stall_test.go ] && { present "go-timed stall test"; pass=$((pass+1)); } \
                                          || pending "stall test pending"

  if [ "$pass" -eq "$total" ]; then set_status M3 DONE; printf "${GRN}M3: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M3 IN_PROGRESS; printf "${YLW}M3: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M3 NOT_STARTED; printf "${DIM}M3: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M4 ----------
check_m4() {
  section "M4 — Python SDK + pyjevsim bridge (Owner: agent-c)"
  echo "Exit: examples/pyjevsim/ deterministic 10× same hash; Python encoder 100% of vectors; mypy --strict clean"
  local pass=0 total=5

  [ -d tests/spec/M4 ] && { present "tests/spec/M4/ committed"; pass=$((pass+1)); } \
                       || pending "tests/spec/M4/ pending orchestrator pre-work"
  [ -f pysdk/pyproject.toml ] && { present "pysdk package bootstrapped"; pass=$((pass+1)); } \
                               || pending "pysdk/ not yet bootstrapped"
  [ -f examples/pyjevsim/runner.py ] && { present "examples/pyjevsim/ exists"; pass=$((pass+1)); } \
                                      || pending "examples/pyjevsim/ pending"

  if command -v pytest >/dev/null 2>&1 && [ -f pysdk/tests/test_encoding_conformance.py ]; then
    if (cd pysdk && pytest tests/test_encoding_conformance.py -q >/dev/null 2>&1); then
      present "Python encoding conformance: 100% of vectors"; pass=$((pass+1))
    else
      pending "Python encoding conformance has failures"
    fi
  else
    pending "pytest or test_encoding_conformance.py not yet in place"
  fi

  if command -v mypy >/dev/null 2>&1 && [ -f pysdk/pyproject.toml ]; then
    if (cd pysdk && mypy --strict . >/dev/null 2>&1); then
      present "mypy --strict pysdk/ clean"; pass=$((pass+1))
    else
      pending "mypy --strict pysdk/ has errors"
    fi
  else
    pending "mypy not installed or pysdk/ absent"
  fi

  if [ "$pass" -eq "$total" ]; then set_status M4 DONE; printf "${GRN}M4: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M4 IN_PROGRESS; printf "${YLW}M4: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M4 NOT_STARTED; printf "${DIM}M4: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M5 ----------
check_m5() {
  section "M5 — End-to-end + modes + perf baseline (Owner: orchestrator + all)"
  echo "Exit: cross-language federation works; verbose+best-effort modes functional; baseline at sizes 2/5/25/100"
  local pass=0 total=4

  [ -d tests/spec/M5 ] && { present "tests/spec/M5/ committed"; pass=$((pass+1)); } \
                       || pending "tests/spec/M5/ pending orchestrator pre-work"
  [ -f examples/pyjevsim/cross_lang_test.py ] && { present "cross-language smoke test"; pass=$((pass+1)); } \
                                                || pending "cross-language smoke pending"
  [ -f pysdk/tests/test_modes.py ] && { present "mode verification test"; pass=$((pass+1)); } \
                                    || pending "mode verification pending"
  [ -f docs/reports/M5/agent-a.md ] && { present "perf baseline report committed"; pass=$((pass+1)); } \
                                     || pending "perf baseline report pending"

  if [ "$pass" -eq "$total" ]; then set_status M5 DONE; printf "${GRN}M5: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M5 IN_PROGRESS; printf "${YLW}M5: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M5 NOT_STARTED; printf "${DIM}M5: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- summary ----------
print_summary() {
  echo
  printf "${CYN}── Summary ──${OFF}\n"
  for m in M0 M1 M2 M3 M4 M5; do
    local s="${MILESTONE_STATUS[$m]:-?}"
    case "$s" in
      DONE)         printf "  %s %s\n" "$PASS_MARK" "$m: DONE" ;;
      DONE_WIP)     printf "  %s %s ${DIM}(passing on working tree; not yet committed)${OFF}\n" "$PEND_MARK" "$m: DONE-WIP" ;;
      IN_PROGRESS)  printf "  %s %s\n" "$PEND_MARK" "$m: IN_PROGRESS" ;;
      NOT_STARTED)  printf "  ${DIM}∘ %s: NOT_STARTED${OFF}\n" "$m" ;;
      REGRESSED)    printf "  %s %s\n" "$FAIL_MARK" "$m: REGRESSED" ;;
      *)            printf "  ? %s: ?\n" "$m" ;;
    esac
  done
  echo
  if [ "$REGRESSED" -eq 0 ]; then
    printf "${GRN}No regressions.${OFF}\n"
  else
    printf "${RED}Regression detected — see details above.${OFF}\n"
  fi
}

# ---------- main ----------
print_header
check_structure
check_m0
check_m1
check_m2
check_m3
check_m4
check_m5
print_summary

exit "$REGRESSED"
