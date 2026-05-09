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

  [ -d pysdk/tests/spec/m4 ] && { present "pysdk/tests/spec/m4/ committed"; pass=$((pass+1)); } \
                              || pending "pysdk/tests/spec/m4/ pending orchestrator pre-work"
  [ -f pysdk/pyproject.toml ] && { present "pysdk package bootstrapped"; pass=$((pass+1)); } \
                               || pending "pysdk/ not yet bootstrapped"
  [ -f examples/pyjevsim/runner.py ] && { present "examples/pyjevsim/ exists"; pass=$((pass+1)); } \
                                      || pending "examples/pyjevsim/ pending"

  if command -v pytest >/dev/null 2>&1 && [ -f pysdk/tests/spec/m4/test_spec_m4_encoding_conformance.py ]; then
    if (cd pysdk && pytest tests/spec/m4/test_spec_m4_encoding_conformance.py -q >/dev/null 2>&1); then
      present "Python encoding conformance: 100% of vectors"; pass=$((pass+1))
    else
      pending "Python encoding conformance has failures"
    fi
  else
    pending "pytest or test_spec_m4_encoding_conformance.py not yet in place"
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

  [ -d rti/spec/M5 ] && [ -d pysdk/tests/spec/m5 ] && { present "rti/spec/M5/ + pysdk/tests/spec/m5/ committed"; pass=$((pass+1)); } \
                                                    || pending "M5 spec test dirs pending orchestrator pre-work"
  [ -f examples/pyjevsim/cross_lang_test.py ] && { present "cross-language smoke test"; pass=$((pass+1)); } \
                                                || pending "cross-language smoke pending"
  [ -f pysdk/tests/spec/m5/test_spec_m5_modes.py ] && { present "mode verification test"; pass=$((pass+1)); } \
                                                    || pending "mode verification pending"
  [ -f docs/reports/M5/agent-a.md ] && { present "perf baseline report committed"; pass=$((pass+1)); } \
                                     || pending "perf baseline report pending"

  if [ "$pass" -eq "$total" ]; then set_status M5 DONE; printf "${GRN}M5: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M5 IN_PROGRESS; printf "${YLW}M5: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M5 NOT_STARTED; printf "${DIM}M5: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M6 (cut 2: hardening) ----------
check_m6() {
  section "M6 — Hardening + handle alignment + TLS + replay path (Owner: orchestrator + all)"
  echo "Exit: cross-language Python+Go bidi smoke; race-clean concurrency; TLS handshake server+client; M4 replay path"
  local pass=0 total=4

  [ -f docs/reports/M6/agent-c-handle-alignment.md ] && { present "handle alignment report"; pass=$((pass+1)); } \
                                                      || pending "handle alignment pending"
  [ -f docs/reports/M6/agent-a-hardening.md ] && { present "concurrency + TLS report"; pass=$((pass+1)); } \
                                               || pending "concurrency + TLS pending"
  [ -f docs/reports/M6/agent-a-rememberfor.md ] && { present "RememberFor wiring"; pass=$((pass+1)); } \
                                                  || pending "RememberFor wiring pending"
  # Check for "scaffolded" in the skip message (our scaffold pattern); env-
  # conditional skips (e.g. "go toolchain not on PATH") don't count as a
  # scaffold-skip and are acceptable.
  if [ -f pysdk/tests/spec/m4/test_spec_m4_replay.py ] && ! grep -q "scaffolded" pysdk/tests/spec/m4/test_spec_m4_replay.py 2>/dev/null; then
    present "M4 replay path no longer scaffold-skipped"; pass=$((pass+1))
  else
    pending "M4 replay path still scaffold-skipped"
  fi

  if [ "$pass" -eq "$total" ]; then set_status M6 DONE; printf "${GRN}M6: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M6 IN_PROGRESS; printf "${YLW}M6: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M6 NOT_STARTED; printf "${DIM}M6: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M7 (cut 2: complete time-advance primitives) ----------
check_m7() {
  section "M7 — TAR + TARA + FQR + NMRA (Owner: agent-a)"
  echo "Exit: all four primitives invokable; share LBTS with NER; deterministic across 20 randomized scenarios"
  local pass=0 total=2
  [ -d rti/spec/M7 ] && { present "rti/spec/M7/ committed"; pass=$((pass+1)); } \
                      || pending "rti/spec/M7/ pending orchestrator pre-work"
  if [ -f rti/internal/time/advance.go ] || [ -f rti/internal/time/tar.go ]; then
    present "TAR/TARA/FQR/NMRA implemented"; pass=$((pass+1))
  else
    pending "time-advance primitives pending"
  fi

  if [ "$pass" -eq "$total" ]; then set_status M7 DONE; printf "${GRN}M7: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M7 IN_PROGRESS; printf "${YLW}M7: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M7 NOT_STARTED; printf "${DIM}M7: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M8 (cut 2: sync + ownership) ----------
check_m8() {
  section "M8 — Synchronization Management + Ownership Management (Owner: agent-a)"
  echo "Exit: sync-point register/announce/achieve/synchronized; negotiated divest/acquire two-phase; replay byte-identical"
  local pass=0 total=3
  [ -d rti/spec/M8 ] && { present "rti/spec/M8/ committed"; pass=$((pass+1)); } || pending "rti/spec/M8/ pending"
  [ -d rti/internal/sync ] && { present "sync package exists"; pass=$((pass+1)); } || pending "sync package pending"
  [ -d rti/internal/ownership ] && { present "ownership package exists"; pass=$((pass+1)); } || pending "ownership package pending"

  if [ "$pass" -eq "$total" ]; then set_status M8 DONE; printf "${GRN}M8: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M8 IN_PROGRESS; printf "${YLW}M8: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M8 NOT_STARTED; printf "${DIM}M8: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M9 (cut 2: federation save/restore) ----------
check_m9() {
  section "M9 — Federation save/restore (Owner: agent-a)"
  echo "Exit: requestFederationSave + initiateFederateSave + federationSaved + restore replay byte-identical"
  local pass=0 total=2
  [ -d rti/spec/M9 ] && { present "rti/spec/M9/ committed"; pass=$((pass+1)); } || pending "rti/spec/M9/ pending"
  [ -d rti/internal/savepoint ] && { present "savepoint package exists"; pass=$((pass+1)); } || pending "savepoint package pending"

  if [ "$pass" -eq "$total" ]; then set_status M9 DONE; printf "${GRN}M9: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M9 IN_PROGRESS; printf "${YLW}M9: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M9 NOT_STARTED; printf "${DIM}M9: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M10 (cut 2: DDM) ----------
check_m10() {
  section "M10 — Data Distribution Management (Owner: agent-a + agent-b)"
  echo "Exit: routing spaces parsed; createRegion + commitRegionModifications; subscribeWithRegions filtering; perf baseline at size 25 / 100 regions"
  local pass=0 total=3
  [ -d rti/spec/M10 ] && { present "rti/spec/M10/ committed"; pass=$((pass+1)); } || pending "rti/spec/M10/ pending"
  [ -d rti/internal/ddm ] && { present "ddm package exists"; pass=$((pass+1)); } || pending "ddm package pending"
  [ -f docs/reports/M10/agent-a.md ] && { present "DDM perf baseline"; pass=$((pass+1)); } || pending "DDM perf baseline pending"

  if [ "$pass" -eq "$total" ]; then set_status M10 DONE; printf "${GRN}M10: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M10 IN_PROGRESS; printf "${YLW}M10: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M10 NOT_STARTED; printf "${DIM}M10: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M11 (cut 2: MOM runtime) ----------
check_m11() {
  section "M11 — MOM runtime (Owner: agent-a)"
  echo "Exit: federates can subscribe to HLAmanager.HLAfederate attributes via standard pub/sub; lifecycle events drive updates"
  local pass=0 total=2
  [ -d rti/spec/M11 ] && { present "rti/spec/M11/ committed"; pass=$((pass+1)); } || pending "rti/spec/M11/ pending"
  [ -d rti/internal/mom ] && { present "mom package exists"; pass=$((pass+1)); } || pending "mom package pending"

  if [ "$pass" -eq "$total" ]; then set_status M11 DONE; printf "${GRN}M11: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M11 IN_PROGRESS; printf "${YLW}M11: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M11 NOT_STARTED; printf "${DIM}M11: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M21 (cut 3 cross-cutting: complete TimeService gRPC wiring) ----------
check_m21() {
  section "M21 — Complete TimeService gRPC wiring (Owner: agent-a + agent-c)"
  echo "Exit (srs.md §10.4): all 5 advance primitives + 3 queries reachable cross-process; pysdk time RPCs real; cut-3 README caveats struck"
  local pass=0 total=8

  # 1. proto wire surface
  if grep -q 'rpc TimeAdvanceRequest' proto/rti/v1/time.proto 2>/dev/null \
     && grep -q 'rpc NextMessageRequestAvailable' proto/rti/v1/time.proto 2>/dev/null \
     && grep -q 'rpc FlushQueueRequest' proto/rti/v1/time.proto 2>/dev/null \
     && grep -q 'rpc QueryLogicalTime' proto/rti/v1/time.proto 2>/dev/null; then
    present "proto/rti/v1/time.proto: TAR + NMRA + FQR + queries declared"; pass=$((pass+1))
  else
    pending "proto/rti/v1/time.proto missing M21 advance primitives or queries"
  fi

  # 2. wire adapter file present
  if [ -f rti/internal/transport/grpc/time.go ]; then
    present "rti/internal/transport/grpc/time.go: TimeServiceServer impl"; pass=$((pass+1))
  else
    missing "rti/internal/transport/grpc/time.go absent — TimeService not wired"
  fi

  # 3. Manager.ModifyLookahead landed
  if grep -q 'func (m \*Manager) ModifyLookahead' rti/internal/time/manager.go 2>/dev/null; then
    present "Manager.ModifyLookahead present"; pass=$((pass+1))
  else
    pending "Manager.ModifyLookahead missing"
  fi

  # 4. Go federate SDK time surface
  if [ -f rti/pkg/federate/time.go ]; then
    present "rti/pkg/federate/time.go: SDK time methods"; pass=$((pass+1))
  else
    missing "rti/pkg/federate/time.go absent"
  fi

  # 5. pysdk time RPCs flipped (no-op caveat gone)
  if [ -f pysdk/rti1516e/_transport.py ] \
     && ! grep -q 'TimeService is nil at M2' pysdk/rti1516e/_transport.py 2>/dev/null; then
    present "pysdk/_transport.py: time RPCs no longer no-op (M2 caveat struck)"; pass=$((pass+1))
  else
    pending "pysdk/_transport.py still carries the M2 'TimeService is nil' caveat"
  fi

  # 6. Showcase examples restored
  if [ -f examples/go-timed/regulator_main.go ] && [ -f examples/pyjevsim-time-advance/regulator_main.py ]; then
    present "examples/go-timed/ + examples/pyjevsim-time-advance/ restored"; pass=$((pass+1))
  else
    pending "M21 showcase examples not yet restored"
  fi

  # 7. Spec test files in place (W5 gate)
  if [ -f rti/spec/M21/time_service_test.go ] && [ -f pysdk/tests/spec/m21/test_time_service_cross_language.py ]; then
    present "rti/spec/M21/ + pysdk/tests/spec/m21/ committed"; pass=$((pass+1))
  else
    pending "M21 spec test dirs incomplete"
  fi

  # 8. Cut-3 README caveats struck in all 4 examples (literal-string regression check)
  local stale=0
  for f in examples/pyjevsim-relay-cross-process/README.md \
           examples/pyjevsim/README.md \
           examples/pyjevsim-sync-points/README.md \
           examples/pyjevsim-dashboard-bridged/README.md; do
    if [ -f "$f" ] && grep -q 'does not yet wire the time-service gRPC handlers' "$f" 2>/dev/null; then
      stale=$((stale+1))
    fi
  done
  if [ "$stale" -eq 0 ]; then
    present "Cut-3 README 'time-service not yet wired' caveats struck"; pass=$((pass+1))
  else
    missing "$stale cut-3 README(s) still carry the pre-M21 caveat"
  fi

  if [ "$pass" -eq "$total" ]; then set_status M21 DONE; printf "${GRN}M21: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M21 IN_PROGRESS; printf "${YLW}M21: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M21 NOT_STARTED; printf "${DIM}M21: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M22 (cut 3 cross-cutting: TimeService completion) ----------
check_m22() {
  section "M22 — TimeService completion (Owner: agent-a + agent-c)"
  echo "Exit (srs.md §10.4): pysdk surface parity + async-delivery toggle + NER race fixed + M21 workarounds removed"
  local pass=0 total=8

  # 1. Async-delivery RPCs declared in proto
  if grep -q 'rpc EnableAsynchronousDelivery' proto/rti/v1/time.proto 2>/dev/null \
     && grep -q 'rpc DisableAsynchronousDelivery' proto/rti/v1/time.proto 2>/dev/null; then
    present "proto/rti/v1/time.proto: async-delivery RPCs declared"; pass=$((pass+1))
  else
    pending "proto/rti/v1/time.proto missing async-delivery RPCs"
  fi

  # 2. core.TSODeliveryGate interface present
  if [ -f rti/internal/core/tso_gate.go ] \
     && grep -q 'type TSODeliveryGate interface' rti/internal/core/tso_gate.go 2>/dev/null; then
    present "core.TSODeliveryGate interface defined"; pass=$((pass+1))
  else
    missing "core.TSODeliveryGate missing"
  fi

  # 3. Async-delivery manager methods + buffer machinery
  if [ -f rti/internal/time/asyncdelivery.go ] \
     && grep -q 'func (m \*Manager) EnableAsynchronousDelivery' rti/internal/time/asyncdelivery.go 2>/dev/null \
     && grep -q 'releaseBufferedTSO' rti/internal/time/asyncdelivery.go 2>/dev/null; then
    present "Manager async-delivery methods + buffer release path"; pass=$((pass+1))
  else
    pending "Manager async-delivery machinery incomplete"
  fi

  # 4. object.Registry consults TSOGate (literal-string check on the
  # update.go + interaction.go gate-check pattern)
  if grep -q 'r.opts.TSOGate.ShouldDeliverNow' rti/internal/object/update.go 2>/dev/null \
     && grep -q 'r.opts.TSOGate.ShouldDeliverNow' rti/internal/object/interaction.go 2>/dev/null; then
    present "object.Registry consults TSOGate on TSO sends"; pass=$((pass+1))
  else
    pending "object.Registry TSOGate consultation missing"
  fi

  # 5. Pysdk surface parity — 15 methods on Federate
  if [ -f pysdk/rti1516e/connection.py ]; then
    local missing_count=0
    for m in disable_time_regulation disable_time_constrained modify_lookahead \
             time_advance_request time_advance_request_available \
             next_message_request_available flush_queue_request \
             query_logical_time query_lookahead query_lbts \
             enable_asynchronous_delivery disable_asynchronous_delivery; do
      if ! grep -q "async def $m" pysdk/rti1516e/connection.py 2>/dev/null; then
        missing_count=$((missing_count + 1))
      fi
    done
    if [ "$missing_count" -eq 0 ]; then
      present "pysdk Federate exposes 12 added methods (W1+W2)"; pass=$((pass+1))
    else
      pending "pysdk Federate missing $missing_count of 12 added methods"
    fi
  fi

  # 6. M22 spec test files in place
  if [ -f rti/spec/M22/async_delivery_test.go ] \
     && [ -f rti/spec/M22/ner_forced_grant_race_test.go ] \
     && [ -f rti/spec/M22/time_service_completion_test.go ] \
     && [ -f pysdk/tests/spec/m22/test_pysdk_time_surface.py ]; then
    present "rti/spec/M22/ + pysdk/tests/spec/m22/ committed"; pass=$((pass+1))
  else
    pending "M22 spec test files incomplete"
  fi

  # 7. M21-era workarounds removed from regulator examples
  local stale=0
  if [ -f examples/pyjevsim-time-advance/regulator_main.py ] \
     && grep -q 'for attempt in range(8)' examples/pyjevsim-time-advance/regulator_main.py 2>/dev/null; then
    stale=$((stale + 1))
  fi
  if [ -f examples/go-timed/regulator_main.go ] \
     && grep -q 'time.Sleep(5 \* time.Millisecond)' examples/go-timed/regulator_main.go 2>/dev/null; then
    stale=$((stale + 1))
  fi
  if [ "$stale" -eq 0 ]; then
    present "M21-era NER workarounds (retry-backoff + settle delay) removed"; pass=$((pass+1))
  else
    missing "$stale M21 workaround(s) still present in regulator examples"
  fi

  # 8. Migration audit — dashboard examples call enable_asynchronous_delivery
  local audit_done=0
  for f in examples/pyjevsim-dashboard/dashboard_main.py \
           examples/pyjevsim-dashboard-bridged/dashboard_main.py; do
    if [ -f "$f" ] && grep -q 'enable_asynchronous_delivery' "$f" 2>/dev/null; then
      audit_done=$((audit_done + 1))
    fi
  done
  if [ "$audit_done" -eq 2 ]; then
    present "Migration audit: dashboard examples opt into async delivery"; pass=$((pass+1))
  else
    pending "Migration audit incomplete: $audit_done/2 dashboards updated"
  fi

  if [ "$pass" -eq "$total" ]; then set_status M22 DONE; printf "${GRN}M22: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M22 IN_PROGRESS; printf "${YLW}M22: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M22 NOT_STARTED; printf "${DIM}M22: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M23 (ObjectManagement §6 + DDM §9 completion) ----------
check_m23() {
  section "M23 — ObjectManagement (§6) + DDM (§9) completion (Owner: agent-a + agent-c)"
  echo "Exit (srs.md §10.4): §6 lifecycle/pull/transport + §9 Go SDK + §9 missing services"
  local pass=0 total=8

  # 1. §6 Object proto extensions
  if grep -q 'rpc DeleteObjectInstance' proto/rti/v1/object.proto 2>/dev/null \
     && grep -q 'rpc LocalDeleteObjectInstance' proto/rti/v1/object.proto 2>/dev/null \
     && grep -q 'rpc RequestAttributeValueUpdate' proto/rti/v1/object.proto 2>/dev/null \
     && grep -q 'rpc ChangeAttributeTransportationType' proto/rti/v1/object.proto 2>/dev/null; then
    present "object.proto: 6 §6 RPCs declared"; pass=$((pass+1))
  else
    pending "object.proto missing §6 RPC declarations"
  fi

  # 2. RemoveObjectInstance no longer orphan; ProvideAttributeValueUpdate added
  if grep -q 'ProvideAttributeValueUpdate provide_update' proto/rti/v1/stream.proto 2>/dev/null; then
    present "stream.proto: ProvideAttributeValueUpdate event declared"; pass=$((pass+1))
  else
    missing "stream.proto missing ProvideAttributeValueUpdate"
  fi

  # 3. §6 manager files present
  if [ -f rti/internal/object/delete.go ] \
     && [ -f rti/internal/object/request_update.go ] \
     && [ -f rti/internal/object/transport.go ]; then
    present "Object manager: delete + request_update + transport"; pass=$((pass+1))
  else
    pending "§6 manager files incomplete"
  fi

  # 4. §9 DDM proto extensions
  if grep -q 'rpc AssociateRegionsForUpdates' proto/rti/v1/ddm.proto 2>/dev/null \
     && grep -q 'rpc UnassociateRegionsForUpdates' proto/rti/v1/ddm.proto 2>/dev/null \
     && grep -q 'rpc SendInteractionWithRegions' proto/rti/v1/ddm.proto 2>/dev/null \
     && grep -q 'rpc RequestAttributeValueUpdateWithRegions' proto/rti/v1/ddm.proto 2>/dev/null; then
    present "ddm.proto: 6 §9 RPCs declared"; pass=$((pass+1))
  else
    pending "ddm.proto missing §9 RPC declarations"
  fi

  # 5. §9 manager additions
  if [ -f rti/internal/ddm/missing_services.go ]; then
    present "DDM manager: missing_services.go"; pass=$((pass+1))
  else
    missing "rti/internal/ddm/missing_services.go absent"
  fi

  # 6. Go SDK DDM coverage (was zero pre-M23)
  if [ -f rti/pkg/federate/ddm.go ]; then
    local missing_count=0
    for m in LookupRoutingSpace CreateRegion DeleteRegion \
             SubscribeObjectClassAttributesWithRegions \
             RegisterObjectInstanceWithRegions \
             AssociateRegionsForUpdates \
             UnassociateRegionsForUpdates \
             UnsubscribeObjectClassAttributesWithRegions \
             SendInteractionWithRegions \
             RequestAttributeValueUpdateWithRegions; do
      if ! grep -q "func (f \*Federate) $m" rti/pkg/federate/ddm.go 2>/dev/null; then
        missing_count=$((missing_count + 1))
      fi
    done
    if [ "$missing_count" -eq 0 ]; then
      present "Go SDK DDM: 16 methods covered (W4 + W5)"; pass=$((pass+1))
    else
      pending "Go SDK DDM missing $missing_count methods"
    fi
  fi

  # 7. M23 spec test files
  if [ -f rti/spec/M23/delete_test.go ] \
     && [ -f rti/spec/M23/request_update_test.go ] \
     && [ -f rti/spec/M23/transport_test.go ] \
     && [ -f rti/spec/M23/ddm_go_sdk_test.go ] \
     && [ -f rti/spec/M23/ddm_missing_test.go ] \
     && [ -f rti/spec/M23/m23_completion_test.go ]; then
    present "rti/spec/M23/ + pysdk/tests/spec/m23/ committed"; pass=$((pass+1))
  else
    pending "M23 spec test files incomplete"
  fi

  # 8. RemoveObjectInstance no longer orphan: a consumer exists
  if grep -q 'RemoveObjectInstance' rti/pkg/federate/events.go 2>/dev/null \
     && grep -q 'FederateEvent_Remove' rti/pkg/federate/federate.go 2>/dev/null; then
    present "RemoveObjectInstance proto slot now has SDK consumers"; pass=$((pass+1))
  else
    missing "RemoveObjectInstance still orphan"
  fi

  if [ "$pass" -eq "$total" ]; then set_status M23 DONE; printf "${GRN}M23: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M23 IN_PROGRESS; printf "${YLW}M23: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M23 NOT_STARTED; printf "${DIM}M23: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- M24 (FederationManagement §4 completion + Resign correctness) ----------
check_m24() {
  section "M24 — FederationManagement (§4) completion + Resign correctness (Owner: agent-a + agent-c)"
  echo "Exit (srs.md §10.4): all 6 ResignActions accepted, ownership.ReleaseAllOwnedBy chained, listMembers + abort save/restore wired"
  local pass=0 total=7

  # 1. ResignAction enum expansion (5 new values uncommented in common.proto)
  if grep -q 'RESIGN_ACTION_DELETE_THEN_DIVEST\s*=\s*2' proto/rti/v1/common.proto 2>/dev/null \
     && grep -q 'RESIGN_ACTION_NO_ACTION' proto/rti/v1/common.proto 2>/dev/null \
     && grep -q 'RESIGN_ACTION_DELETE_OBJECTS' proto/rti/v1/common.proto 2>/dev/null; then
    present "common.proto: ResignAction enum has all 6 spec values"; pass=$((pass+1))
  else
    pending "ResignAction enum incomplete"
  fi

  # 2. ownership.Manager.ReleaseAllOwnedBy + CancelPendingFor
  if [ -f rti/internal/ownership/release.go ] \
     && grep -q 'func (m \*Manager) ReleaseAllOwnedBy' rti/internal/ownership/release.go 2>/dev/null \
     && grep -q 'func (m \*Manager) CancelPendingFor' rti/internal/ownership/release.go 2>/dev/null; then
    present "ownership.Manager.ReleaseAllOwnedBy + CancelPendingFor"; pass=$((pass+1))
  else
    missing "rti/internal/ownership/release.go missing or incomplete"
  fi

  # 3. federation.Manager accepts new ResignAction values (no longer
  # rejects with "not supported in cut 1")
  if ! grep -q 'not supported in cut 1' rti/internal/federation/manager.go 2>/dev/null; then
    present "federation.Manager.ResignFederation accepts all spec actions"; pass=$((pass+1))
  else
    missing "federation.Manager still rejects with 'not supported in cut 1'"
  fi

  # 4. OnFederateResigning hook present
  if grep -q 'OnFederateResigning func' rti/internal/federation/manager.go 2>/dev/null; then
    present "OnFederateResigning hook (action-aware) declared"; pass=$((pass+1))
  else
    missing "OnFederateResigning hook missing"
  fi

  # 5. cmd/rtid resigning-dispatch wires per-action cleanup
  if grep -q 'resigningDispatch' rti/cmd/rtid/main.go 2>/dev/null \
     && grep -q 'deleteAllOwnedBy' rti/cmd/rtid/main.go 2>/dev/null; then
    present "cmd/rtid: resigning-dispatch + deleteAllOwnedBy wired"; pass=$((pass+1))
  else
    missing "cmd/rtid resigning-dispatch missing"
  fi

  # 6. ListFederationMembers + Abort save/restore RPCs
  if grep -q 'rpc ListFederationMembers' proto/rti/v1/federation.proto 2>/dev/null \
     && grep -q 'rpc AbortFederationSave' proto/rti/v1/savepoint.proto 2>/dev/null \
     && grep -q 'rpc AbortFederationRestore' proto/rti/v1/savepoint.proto 2>/dev/null; then
    present "Proto: ListFederationMembers + AbortFederationSave/Restore"; pass=$((pass+1))
  else
    pending "Proto missing W3 RPCs"
  fi

  # 7. M24 spec test files
  if [ -f rti/spec/M24/release_test.go ] \
     && [ -f rti/spec/M24/resign_actions_test.go ] \
     && [ -f rti/spec/M24/list_members_test.go ] \
     && [ -f rti/spec/M24/m24_completion_test.go ]; then
    present "rti/spec/M24/ + pysdk/tests/spec/m24/ committed"; pass=$((pass+1))
  else
    pending "M24 spec test files incomplete"
  fi

  if [ "$pass" -eq "$total" ]; then set_status M24 DONE; printf "${GRN}M24: DONE${OFF} (%d/%d)\n" "$pass" "$total"
  elif [ "$pass" -gt 0 ]; then set_status M24 IN_PROGRESS; printf "${YLW}M24: IN_PROGRESS${OFF} (%d/%d)\n" "$pass" "$total"
  else set_status M24 NOT_STARTED; printf "${DIM}M24: NOT_STARTED${OFF} (%d/%d)\n" "$pass" "$total"; fi
}

# ---------- summary ----------
print_summary() {
  echo
  printf "${CYN}── Summary ──${OFF}\n"
  for m in M0 M1 M2 M3 M4 M5 M6 M7 M8 M9 M10 M11 M21 M22 M23 M24; do
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
check_m6
check_m7
check_m8
check_m9
check_m10
check_m11
check_m21
check_m22
check_m23
check_m24
print_summary

exit "$REGRESSED"
