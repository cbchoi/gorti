#!/usr/bin/env bash
# run_fixture.sh — deterministic scripted driver for multi-federate DLC
# conformance fixtures (M37 ED-1).
#
# Every multi-federate fixture has an implicit launch protocol (who joins
# first, what to wait for, canonical name order) that used to live only in
# README prose. Ad-hoc re-runs that ignored it raced (e.g. tm_tso_ordering:
# publishers sending before the subscriber's constrain-enable → 0 RECVs
# where the protocol-respecting run gets 8/9). This driver encodes the
# protocol once, executably, in a per-fixture `driver.conf`.
#
# Usage:
#   run_fixture.sh <fixture_name> [--rtid <path>] [--port <p>]
#                  [--build-dir <dir>] [--capture]
#
#   --rtid       path to the rtid binary       (default: <repo>/bin/rtid)
#   --port       rtid listen port              (default: 8080, or the
#                fixture driver.conf `port` directive; CLI wins)
#   --build-dir  dir holding conf_<fixture>_<federate> binaries
#                (default: <repo>/cppsdk/build/tests/dlc/conformance)
#   --capture    write the canonicalized per-role capture back to
#                <fixture>/gorti-captured.<role>.log (commit artifact)
#
# driver.conf format (whitespace-separated, shell-style quoting):
#   # role  binary-suffix         order  wait-for(regex)  args           [env]
#   subscriber federate_subscriber 1     "JOIN"           ""
#   alice      federate_publisher  2     "JOIN"           "--name alice"
#   bob        federate_peer       3     ""               ""             "FED_NAME=bob"
#   port 8080            # optional directive: fixture needs this port
#
# Semantics:
#   * entries launch in ascending `order`;
#   * if the PREVIOUS entry has a wait-for regex, the driver blocks until
#     that regex appears in the previous entry's own stdout log before
#     launching the next entry (60 s timeout); otherwise a 1 s stagger;
#   * `role` must match the golden name: expected.<role>.log;
#   * every launch gets `--url grpc://127.0.0.1:<port> --fom
#     ./federation.fom.xml` appended (federates that don't parse flags
#     ignore argv entirely, so this is harmless) plus the entry's args;
#   * cwd is the fixture dir (federates load ./federation.fom.xml);
#   * the whole run is serialized under flock on /tmp/gorti-rtid-8080.lock
#     and starts from a fresh rtid (stale rtids are killed first).
#
# Verdict per role: canonical lines (grep '^<ROLE>:'-shaped prefixes) are
# canonicalized via normalize.py and diffed against the comment-stripped,
# canonicalized golden. FULL == byte-identical; otherwise PARTIAL n/m with
# a unified diff. Exit 0 iff all roles FULL; 1 on any mismatch; 2 on
# infra/launch failure.

set -u -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONF_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"                # .../tests/dlc/conformance
CPPSDK_ROOT="$(cd "$CONF_ROOT/../../.." && pwd)"          # .../cppsdk
REPO_ROOT="$(cd "$CPPSDK_ROOT/.." && pwd)"
NORMALIZE="$SCRIPT_DIR/normalize.py"

LOCK_FILE=/tmp/gorti-rtid-8080.lock
WAIT_FOR_TIMEOUT=60      # s, per wait-for regex
FEDERATE_TIMEOUT=180     # s, all federates must exit within this
DEFAULT_STAGGER=1        # s, when the previous entry has no wait-for

die() { echo "run_fixture: ERROR: $*" >&2; exit 2; }

# ---- CLI -------------------------------------------------------------------
[ $# -ge 1 ] || die "usage: run_fixture.sh <fixture_name> [--rtid <path>] [--port <p>] [--build-dir <dir>] [--capture]"
FIXTURE="$1"; shift
RTID_BIN="$REPO_ROOT/bin/rtid"
BUILD_DIR="$CPPSDK_ROOT/build/tests/dlc/conformance"
PORT=""
CAPTURE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --rtid)      RTID_BIN="$2"; shift 2 ;;
    --port)      PORT="$2"; shift 2 ;;
    --build-dir) BUILD_DIR="$2"; shift 2 ;;
    --capture)   CAPTURE=1; shift ;;
    *) die "unknown option: $1" ;;
  esac
done

FIXTURE_DIR="$CONF_ROOT/$FIXTURE"
CONF="$FIXTURE_DIR/driver.conf"
[ -d "$FIXTURE_DIR" ] || die "no such fixture: $FIXTURE_DIR"
[ -f "$CONF" ]        || die "no driver.conf for fixture '$FIXTURE' (single-federate fixtures don't need the driver)"
[ -x "$RTID_BIN" ]    || die "rtid binary not executable: $RTID_BIN (use --rtid)"
command -v python3 >/dev/null || die "python3 required (normalize.py)"

# ---- driver.conf parse -----------------------------------------------------
CONF_PORT=""
ROLES=(); SUFFIXES=(); ORDERS=(); WAITFORS=(); ARGSS=(); ENVS=()
while IFS= read -r line || [ -n "$line" ]; do
  # strip comments + blanks
  line="${line%%#*}"
  [ -z "${line//[[:space:]]/}" ] && continue
  # shell-style field split (driver.conf is repo-controlled)
  eval "fields=( $line )" || die "unparseable driver.conf line: $line"
  if [ "${fields[0]}" = "port" ]; then
    CONF_PORT="${fields[1]}"
    continue
  fi
  [ "${#fields[@]}" -ge 5 ] || die "driver.conf entry needs >=5 fields: $line"
  ROLES+=("${fields[0]}")
  SUFFIXES+=("${fields[1]}")
  ORDERS+=("${fields[2]}")
  WAITFORS+=("${fields[3]}")
  ARGSS+=("${fields[4]}")
  ENVS+=("${fields[5]:-}")
done < "$CONF"
[ "${#ROLES[@]}" -ge 1 ] || die "driver.conf has no entries"

[ -n "$PORT" ] || PORT="${CONF_PORT:-8080}"

# entries sorted by launch order (stable)
mapfile -t ORDER_IDX < <(
  for i in "${!ORDERS[@]}"; do printf '%s %s\n' "${ORDERS[$i]}" "$i"; done \
    | sort -s -n -k1,1 | awk '{print $2}')

RUN_DIR="$(mktemp -d "${TMPDIR:-/tmp}/gorti-run-${FIXTURE}.XXXXXX")"
echo "run_fixture: fixture=$FIXTURE port=$PORT rundir=$RUN_DIR"

# ---- serialized rtid lifecycle ----------------------------------------------
exec 200>"$LOCK_FILE"
flock -w 900 200 || die "could not acquire $LOCK_FILE within 900s"

# kill stale rtids (both --listen and -listen spellings of the Go flag)
for p in $(pgrep -f "rtid --listen" || true); do kill "$p" 2>/dev/null; done
for p in $(pgrep -f "rtid -listen" || true); do kill "$p" 2>/dev/null; done
sleep 0.3

RTID_PID=""
FED_PIDS=()
cleanup() {
  for p in "${FED_PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done
  [ -n "$RTID_PID" ] && kill "$RTID_PID" 2>/dev/null
}
trap cleanup EXIT

# rtid cwd = RUN_DIR: rtid writes savepoint bundles (gorti-saves/) to its
# cwd; inheriting the caller's cwd leaks bundles across runs and a stale
# bundle makes the next requestFederationSave abort (M37 EE:
# fm_save_restore_roundtrip lost FEDERATION_SAVED on every second run).
# A fresh rtid must mean fresh persistent state too.
( cd "$RUN_DIR" && exec "$RTID_BIN" --listen "127.0.0.1:$PORT" --admin-listen "" ) \
  >"$RUN_DIR/rtid.log" 2>&1 &
RTID_PID=$!

# wait for the listen socket
rtid_up=0
for _ in $(seq 1 100); do
  if (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null; then rtid_up=1; break; fi
  kill -0 "$RTID_PID" 2>/dev/null || break
  sleep 0.1
done
[ "$rtid_up" = 1 ] || { sed -n '1,20p' "$RUN_DIR/rtid.log" >&2; die "rtid did not open 127.0.0.1:$PORT"; }

# ---- launch protocol ---------------------------------------------------------
launch_one() { # $1=role $2=suffix $3=args $4=env
  local role="$1" suffix="$2" args="$3" envkv="$4"
  local bin="$BUILD_DIR/conf_${FIXTURE}_${suffix}"
  [ -x "$bin" ] || die "federate binary missing: $bin (build target conf_${FIXTURE}_${suffix})"
  local -a extra=() envarr=()
  [ -n "$args" ] && read -ra extra <<<"$args"
  [ -n "$envkv" ] && read -ra envarr <<<"$envkv"
  (
    cd "$FIXTURE_DIR" || exit 127
    # stdbuf -oL: printf-based fixtures are block-buffered to a file, and
    # wait-for gates need their markers (e.g. `SUB: JOIN`) live, not at exit.
    exec env "${envarr[@]}" stdbuf -oL "$bin" \
      --url "grpc://127.0.0.1:$PORT" --fom ./federation.fom.xml \
      "${extra[@]}"
  ) >"$RUN_DIR/$role.raw.log" 2>"$RUN_DIR/$role.err.log" &
  FED_PIDS+=("$!")
  echo "run_fixture: launched $role (conf_${FIXTURE}_${suffix} ${envkv:+env=$envkv }args='$args') pid=${FED_PIDS[-1]}"
}

n="${#ORDER_IDX[@]}"
for k in "${!ORDER_IDX[@]}"; do
  i="${ORDER_IDX[$k]}"
  launch_one "${ROLES[$i]}" "${SUFFIXES[$i]}" "${ARGSS[$i]}" "${ENVS[$i]}"
  # gate the NEXT launch on this entry's wait-for (or default stagger)
  if [ "$((k + 1))" -lt "$n" ]; then
    wf="${WAITFORS[$i]}"
    if [ -n "$wf" ]; then
      ok=0
      deadline=$(( $(date +%s) + WAIT_FOR_TIMEOUT ))
      while [ "$(date +%s)" -le "$deadline" ]; do
        if grep -qE "$wf" "$RUN_DIR/${ROLES[$i]}.raw.log" 2>/dev/null; then ok=1; break; fi
        kill -0 "${FED_PIDS[-1]}" 2>/dev/null || break   # died early: fail fast
        sleep 0.2
      done
      # a federate may legitimately exit right after printing the marker
      [ "$ok" = 1 ] || grep -qE "$wf" "$RUN_DIR/${ROLES[$i]}.raw.log" 2>/dev/null \
        || die "wait-for '$wf' never appeared in ${ROLES[$i]} log ($RUN_DIR/${ROLES[$i]}.raw.log)"
    else
      sleep "$DEFAULT_STAGGER"
    fi
  fi
done

# ---- wait for all federates ---------------------------------------------------
deadline=$(( $(date +%s) + FEDERATE_TIMEOUT ))
timed_out=0
for j in "${!FED_PIDS[@]}"; do
  p="${FED_PIDS[$j]}"
  while kill -0 "$p" 2>/dev/null; do
    if [ "$(date +%s)" -gt "$deadline" ]; then
      echo "run_fixture: TIMEOUT — killing remaining federates" >&2
      timed_out=1
      break 2
    fi
    sleep 0.2
  done
done
if [ "$timed_out" = 1 ]; then
  for p in "${FED_PIDS[@]}"; do kill -9 "$p" 2>/dev/null; done
fi

kill "$RTID_PID" 2>/dev/null
wait "$RTID_PID" 2>/dev/null
RTID_PID=""

# ---- per-role verdicts ---------------------------------------------------------
overall=0
echo "== verdicts ($FIXTURE) =="
for i in "${!ROLES[@]}"; do
  role="${ROLES[$i]}"
  exp="$FIXTURE_DIR/expected.$role.log"
  raw="$RUN_DIR/$role.raw.log"
  if [ ! -f "$exp" ]; then
    echo "  $role: NO-GOLDEN (expected.$role.log missing)"; overall=1; continue
  fi
  # canonical event lines only (role prefixes may be lowercase, e.g. `alice:`)
  grep -E '^[A-Za-z_]+:' "$raw" >"$RUN_DIR/$role.canon.pre" 2>/dev/null || true
  python3 "$NORMALIZE" "$RUN_DIR/$role.canon.pre" -o "$RUN_DIR/$role.captured.canon" \
    || die "normalize.py failed on $role capture"
  # golden: strip whole-line and trailing `# ...` comments, then canonicalize
  sed 's/[[:space:]]*#.*$//' "$exp" | grep -v '^[[:space:]]*$' >"$RUN_DIR/$role.expected.pre" || true
  python3 "$NORMALIZE" "$RUN_DIR/$role.expected.pre" -o "$RUN_DIR/$role.expected.canon" \
    || die "normalize.py failed on $role golden"

  total=$(grep -c '' "$RUN_DIR/$role.expected.canon" || true)
  if cmp -s "$RUN_DIR/$role.expected.canon" "$RUN_DIR/$role.captured.canon"; then
    echo "  $role: FULL ($total/$total)"
  else
    matched=$(diff --unchanged-line-format=$'=\n' --old-line-format=$'<\n' \
                   --new-line-format=$'>\n' \
                   "$RUN_DIR/$role.expected.canon" "$RUN_DIR/$role.captured.canon" \
              | grep -c '^=' || true)
    captured_total=$(grep -c '' "$RUN_DIR/$role.captured.canon" || true)
    extra=$(( captured_total - matched ))
    echo "  $role: PARTIAL ($matched/$total golden matched, +$extra extra) — diff (expected vs captured):"
    diff -u "$RUN_DIR/$role.expected.canon" "$RUN_DIR/$role.captured.canon" \
      | sed 's/^/    /' | head -60
    overall=1
  fi
  if [ "$CAPTURE" = 1 ]; then
    cp "$RUN_DIR/$role.captured.canon" "$FIXTURE_DIR/gorti-captured.$role.log"
    echo "    capture written: $FIXTURE_DIR/gorti-captured.$role.log"
  fi
done
[ "$timed_out" = 1 ] && { echo "  (run TIMED OUT — verdicts above are of a truncated run)"; overall=1; }

echo "run_fixture: logs in $RUN_DIR"
exit "$overall"
