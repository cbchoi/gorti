#!/usr/bin/env bash
# ci-gates.sh — the full gate sequence for the DLC compliance program.
#
# Usage: scripts/ci-gates.sh [stage...]
#   stages: stubs go cpp lockfile static sweep ivct   (default: all, in order)
#
# Runs identically locally and in CI (.github/workflows/conformance.yml).
# Every gate here exists because its absence cost a debugging cycle at
# least once during M31-M37:
#   - stubs:    genproto / _generated are gitignored; a stale set after a
#               proto change produces undefined-symbol or wrong-wire bugs.
#   - cpp:      the conf_* fixture targets are EXCLUDE_FROM_ALL — a plain
#               `cmake --build` silently leaves stale fixture binaries
#               (the M37 tm_ner false-regression).
#   - sweep:    verdicts are compared against EXPECTED_VERDICTS.txt in
#               BOTH directions (regression AND undocumented improvement).
set -u -o pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

CONF_DIR=cppsdk/tests/dlc/conformance
BUILD_DIR=cppsdk/build
FAILED=0

note()  { printf '\n== %s ==\n' "$*"; }
fail()  { printf 'GATE FAILED: %s\n' "$*" >&2; FAILED=1; }
die()   { printf 'FATAL: %s\n' "$*" >&2; exit 2; }

# ---------------------------------------------------------------- stubs --
gate_stubs() {
  note "stubs: buf generate (go + python + cpp from proto/)"
  command -v buf >/dev/null || die "buf not installed (CI installs via buf-setup-action; locally: https://buf.build)"
  buf generate || die "buf generate failed"
  [ -d rti/internal/genproto/rti/v1 ] || die "genproto missing after generate"
  [ -d cppsdk/_generated/rti/v1 ]     || die "cppsdk/_generated missing after generate"
}

# ------------------------------------------------------------------- go --
gate_go() {
  note "go: build rtid + vet + test ./rti/..."
  go build -o bin/rtid ./rti/cmd/rtid || { fail "rtid build"; return; }
  go vet ./rti/...                    || fail "go vet"
  go test -timeout 300s ./rti/...     || fail "go test ./rti/..."
}

# ------------------------------------------------------------------ cpp --
gate_cpp() {
  note "cpp: configure + build lib, runtime suites, and ALL conf_* fixtures"
  local tc="${CONAN_TOOLCHAIN:-$BUILD_DIR/conan_toolchain.cmake}"
  [ -f "$tc" ] || die "conan toolchain not found at $tc (run conan install first; CI does this)"
  cmake -S cppsdk -B "$BUILD_DIR" \
        -DCMAKE_TOOLCHAIN_FILE="$(realpath "$tc")" \
        -DCMAKE_BUILD_TYPE=Release >/dev/null || die "cmake configure"
  cmake --build "$BUILD_DIR" -j"$(nproc)" || { fail "cpp build (default targets)"; return; }

  # conf_* fixture targets are EXCLUDE_FROM_ALL — build them EXPLICITLY.
  # Anchor on the help line format ("... <target>") so substrings of
  # test_conf_* driver targets don't produce phantom target names.
  local conf_targets
  conf_targets=$(cmake --build "$BUILD_DIR" --target help 2>/dev/null \
                 | awk '$1 == "..." && $2 ~ /^conf_/ {print $2}' | sort -u)
  [ -n "$conf_targets" ] || die "no conf_* targets found"
  # shellcheck disable=SC2086
  cmake --build "$BUILD_DIR" -j"$(nproc)" --target $conf_targets \
    || { fail "conf_* fixture build"; return; }

  note "cpp: runtime suites"
  local t
  for t in test_conf__runtime_exceptions test_conf__runtime_callback_bridge \
           test_conf__runtime_encoding test_conf__runtime_mom_surface; do
    "$BUILD_DIR/tests/dlc/conformance/_runtime/$t" >/dev/null \
      || fail "runtime suite $t"
  done
}

# ------------------------------------------------------------- lockfile --
gate_lockfile() {
  note "lockfile: all TUs must compile (GREEN state, 0 RED)"
  local pass=0 red=0 tu
  while IFS= read -r tu; do
    if g++ -c -std=c++17 -I cppsdk/include "$tu" -o /dev/null 2>/dev/null; then
      pass=$((pass+1))
    else
      red=$((red+1)); echo "  RED: $tu"
    fi
  done < <(find cppsdk/tests/dlc/lockfile -name '*.cpp')
  echo "  lockfile: $pass PASS / $red RED"
  [ "$red" -eq 0 ] || fail "lockfile has $red RED TUs"
}

# --------------------------------------------------------------- static --
gate_static() {
  note "static: spec-traceability + hygiene checks"
  bash scripts/check-spec-traceability.sh || fail "spec-traceability"
  bash scripts/check-no-debug-prints.sh 2>/dev/null || true  # advisory
}

# ---------------------------------------------------------------- sweep --
gate_sweep() {
  note "sweep: all conformance fixtures vs EXPECTED_VERDICTS.txt"
  [ -x bin/rtid ] || die "bin/rtid missing (run the go stage first)"
  local exp_file=$CONF_DIR/EXPECTED_VERDICTS.txt
  [ -f "$exp_file" ] || die "$exp_file missing"

  local fixture expect rc line
  while IFS= read -r line; do
    case "$line" in \#*|"") continue ;; esac
    fixture=${line%% *}; expect=${line#* }
    case "$expect" in
      SKIP:*)
        echo "  $fixture: SKIP (${expect#SKIP:})" ;;
      FULL|PARTIAL)
        if [ ! -f "$CONF_DIR/$fixture/driver.conf" ]; then
          fail "$fixture: expected $expect but has no driver.conf"; continue
        fi
        bash "$CONF_DIR/_harness/run_fixture.sh" "$fixture" \
             --rtid "$REPO/bin/rtid" >"/tmp/ci-sweep-$fixture.log" 2>&1
        rc=$?
        case "$expect:$rc" in
          FULL:0)    echo "  $fixture: FULL (as expected)" ;;
          PARTIAL:1) echo "  $fixture: PARTIAL (documented divergence, as expected)" ;;
          PARTIAL:0) fail "$fixture: now FULL but expected PARTIAL — update EXPECTED_VERDICTS.txt (improvement must be recorded)" ;;
          FULL:1)    fail "$fixture: verdict regressed from FULL (see /tmp/ci-sweep-$fixture.log)"
                     tail -20 "/tmp/ci-sweep-$fixture.log" ;;
          *)         fail "$fixture: harness error rc=$rc (see /tmp/ci-sweep-$fixture.log)"
                     tail -20 "/tmp/ci-sweep-$fixture.log" ;;
        esac ;;
      *) fail "unknown expectation '$expect' for $fixture" ;;
    esac
  done < "$exp_file"

  # Reverse direction: a fixture on disk that the expectations file forgot.
  local d n
  for d in "$CONF_DIR"/*/; do
    n=$(basename "$d")
    case "$n" in _harness|_runtime) continue ;; esac
    grep -q "^$n " "$exp_file" || fail "fixture $n missing from EXPECTED_VERDICTS.txt"
  done
}

# ----------------------------------------------------------------- ivct --
gate_ivct() {
  note "ivct: IVCT-inspired Python conformance subset vs live rtid"
  [ -x bin/rtid ] || die "bin/rtid missing (run the go stage first)"
  # CI: python3.11 + pip-installed grpcio/protobuf/pytest and the
  # buf-generated pysdk stubs (stubs stage) are already in place.
  # Locally the repo .venv provides grpcio/protobuf; if this fails to
  # import, run with:
  #   PYTHONPATH=$REPO/.venv/lib/python3.11/site-packages \
  #     PYTHON=python3.11 scripts/ci-gates.sh ivct
  # (pysdk/rti1516e/_generated must be regenerated after proto changes —
  #  stale stubs fail on the M37 ownership request flags.)
  "${PYTHON:-python}" -m pytest tests/conformance/rti/ivct-subset -v \
    || fail "ivct subset — locally: PYTHONPATH=\$REPO/.venv/lib/python3.11/site-packages PYTHON=python3.11 scripts/ci-gates.sh ivct (and regenerate pysdk stubs after proto changes)"
}

# ----------------------------------------------------------------- main --
STAGES=("$@")
[ ${#STAGES[@]} -eq 0 ] && STAGES=(stubs go cpp lockfile static sweep ivct)

for s in "${STAGES[@]}"; do
  case "$s" in
    stubs)    gate_stubs ;;
    go)       gate_go ;;
    cpp)      gate_cpp ;;
    lockfile) gate_lockfile ;;
    static)   gate_static ;;
    sweep)    gate_sweep ;;
    ivct)     gate_ivct ;;
    *) die "unknown stage: $s (stages: stubs go cpp lockfile static sweep ivct)" ;;
  esac
done

if [ "$FAILED" -ne 0 ]; then
  echo; echo "ci-gates: FAILED"; exit 1
fi
echo; echo "ci-gates: ALL GATES GREEN"
