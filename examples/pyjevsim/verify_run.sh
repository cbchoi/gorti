#!/usr/bin/env bash
# Cross-result verification. Run after both federate scripts have
# written their result JSON files. Returns exit 0 if the accounting
# invariant holds, exit 1 otherwise (with a diagnostic of which seqs
# were unaccounted for).
#
# Invariant checked:
#   received == published
#     With one publisher + one subscriber + gRPC's in-order delivery
#     guarantee, every seq the producer emits should arrive at the
#     consumer in order, exactly once. If this FAILS, something is
#     structurally wrong (subscription didn't land in time, federate
#     crashed mid-loop, etc.).
#
# Usage:
#   ./rtid_run.sh                 # terminal 1
#   ./consumer_run.sh             # terminal 2  (subscribe first!)
#   ./producer_run.sh             # terminal 3
#   ./verify_run.sh               # after both exit

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

"${PYTHON}" - "${RESULT_DIR}" <<'PYEOF'
import json, pathlib, sys

d = pathlib.Path(sys.argv[1])
required = ["producer-result.json", "consumer-result.json"]
missing = [r for r in required if not (d / r).exists()]
if missing:
    print(f"verify_run: FAILED — missing result files: {missing}")
    print(f"verify_run:           (federate(s) crashed before writing, or never ran)")
    sys.exit(1)

p = json.loads((d / "producer-result.json").read_text())
c = json.loads((d / "consumer-result.json").read_text())

pub = p.get("published", [])
recv = c.get("received", [])

if not pub:
    print(f"verify_run: FAILED — producer published nothing "
          f"(was the producer started after the consumer subscribed?)")
    sys.exit(1)

ok = recv == pub

print(f"verify_run: published={len(pub)}  received={len(recv)}")
print(f"verify_run: received == published : {'PASS' if ok else 'FAIL'}")

if not ok:
    pub_set, recv_set = set(pub), set(recv)
    only_pub = sorted(pub_set - recv_set)[:5]
    only_recv = sorted(recv_set - pub_set)[:5]
    print(f"verify_run:   only-published (first 5) = {only_pub}")
    print(f"verify_run:   only-received  (first 5) = {only_recv}")
    if recv and recv != sorted(recv):
        print(f"verify_run:   note: received is not monotonic — "
              f"in-order delivery was violated")

sys.exit(0 if ok else 1)
PYEOF
