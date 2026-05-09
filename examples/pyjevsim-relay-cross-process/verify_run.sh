#!/usr/bin/env bash
# Cross-result conservation check. Run after all three federate
# scripts have written their result JSON files. Returns exit 0 if
# both invariants hold, exit 1 otherwise (with a diagnostic of which
# seqs were unaccounted for).
#
# Invariants checked:
#   1. published == forwarded ∪ dropped ∪ residual
#      Every seq the generator emitted is accounted for somewhere.
#   2. received == forwarded
#      Every seq the buffer released arrived at the processor.
#
# Usage:
#   ./rtid_run.sh                              # terminal 1
#   ./processor_run.sh                         # terminal 2
#   ./buffer_run.sh                            # terminal 3
#   ./generator_run.sh                         # terminal 4
#   ./verify_run.sh                            # after all three exit

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

"${PYTHON}" - "${RESULT_DIR}" <<'PYEOF'
import json, pathlib, sys

d = pathlib.Path(sys.argv[1])
required = ["generator-result.json", "buffer-result.json", "processor-result.json"]
missing = [r for r in required if not (d / r).exists()]
if missing:
    print(f"verify_run: FAILED — missing result files: {missing}")
    print(f"verify_run:           (federate(s) crashed before writing, or never ran)")
    sys.exit(1)

g = json.loads((d / "generator-result.json").read_text())
b = json.loads((d / "buffer-result.json").read_text())
p = json.loads((d / "processor-result.json").read_text())

pub = set(g.get("published", []))
fwd = set(b.get("forwarded", []))
drp = set(b.get("dropped", []))
res = set(b.get("queue_residual", []))
recv = set(p.get("received", []))

ok1 = pub == fwd | drp | res
ok2 = recv == fwd

print(f"verify_run: published={len(pub)}  forwarded={len(fwd)}  "
      f"dropped={len(drp)}  residual={len(res)}  received={len(recv)}")
print(f"verify_run: published == forwarded ∪ dropped ∪ residual : "
      f"{'PASS' if ok1 else 'FAIL'}")
print(f"verify_run: received  == forwarded                       : "
      f"{'PASS' if ok2 else 'FAIL'}")

if not ok1:
    only_pub = sorted(pub - (fwd | drp | res))[:5]
    only_seen = sorted((fwd | drp | res) - pub)[:5]
    print(f"verify_run:   only-published (first 5) = {only_pub}")
    print(f"verify_run:   only-seen      (first 5) = {only_seen}")
if not ok2:
    only_recv = sorted(recv - fwd)[:5]
    only_fwd = sorted(fwd - recv)[:5]
    print(f"verify_run:   only-received  (first 5) = {only_recv}")
    print(f"verify_run:   only-forwarded (first 5) = {only_fwd}")

sys.exit(0 if ok1 and ok2 else 1)
PYEOF
