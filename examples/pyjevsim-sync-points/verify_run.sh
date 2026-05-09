#!/usr/bin/env bash
# Cross-result verification for the sync-points cross-process run.
# Run after all three participant scripts have written their result
# JSON files. Returns exit 0 if every invariant holds, exit 1 otherwise.
#
# Invariants checked (mirrors runner.py::verify):
#   1. Each federate's `achieved` == ['start_simulation', 'end_simulation']
#   2. Each federate's `synchronized` == ['start_simulation', 'end_simulation']
#   3. Each federate's `sent_ticks` is exactly 1..RUNNING_TICKS

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

"${PYTHON}" - "${RESULT_DIR}" "${RUNNING_TICKS}" <<'PYEOF'
import json, pathlib, sys

d = pathlib.Path(sys.argv[1])
running_ticks = int(sys.argv[2])
labels = ["start_simulation", "end_simulation"]
names = ["alpha", "beta", "gamma"]

missing = [n for n in names if not (d / f"{n}-result.json").exists()]
if missing:
    print(f"verify_run: FAILED — missing result files for: {missing}")
    sys.exit(1)

per = {n: json.loads((d / f"{n}-result.json").read_text()) for n in names}

failures = []
for n in names:
    r = per[n]
    if r.get("achieved") != labels:
        failures.append(f"{n}.achieved={r.get('achieved')!r} != {labels!r}")
    if r.get("synchronized") != labels:
        failures.append(f"{n}.synchronized={r.get('synchronized')!r} != {labels!r}")
    sent = r.get("sent_ticks") or []
    if len(sent) != running_ticks:
        failures.append(f"{n}.sent_ticks len={len(sent)} != {running_ticks}")
    elif sent != list(range(1, running_ticks + 1)):
        failures.append(f"{n}.sent_ticks not monotonic 1..{running_ticks}: {sent}")

print(f"verify_run: alpha sent={len(per['alpha'].get('sent_ticks') or [])}  "
      f"beta sent={len(per['beta'].get('sent_ticks') or [])}  "
      f"gamma sent={len(per['gamma'].get('sent_ticks') or [])}  "
      f"running_ticks={running_ticks}")
print(f"verify_run: every federate achieved+synchronized at "
      f"{labels} : {'PASS' if not any('achieved' in f or 'synchronized' in f for f in failures) else 'FAIL'}")
print(f"verify_run: every federate sent {running_ticks} monotonic Ticks   : "
      f"{'PASS' if not any('sent_ticks' in f for f in failures) else 'FAIL'}")

if failures:
    for f in failures:
        print(f"verify_run:   {f}")
    sys.exit(1)
sys.exit(0)
PYEOF
