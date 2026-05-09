#!/usr/bin/env bash
# Cross-result verification for the sensor + dashboard run. Run after
# both federate scripts have written their result JSON files.
#
# Invariants:
#   1. dashboard.received == sensor.published   (in-order, no drops)
#   2. dashboard.discovered names sensor.instance_name
#
# Returns exit 0 on PASS, 1 on FAIL.

set -euo pipefail

_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

"${PYTHON}" - "${RESULT_DIR}" <<'PYEOF'
import json, pathlib, sys

d = pathlib.Path(sys.argv[1])
required = ["sensor-result.json", "dashboard-result.json"]
missing = [r for r in required if not (d / r).exists()]
if missing:
    print(f"verify_run: FAILED — missing result files: {missing}")
    sys.exit(1)

s = json.loads((d / "sensor-result.json").read_text())
v = json.loads((d / "dashboard-result.json").read_text())

published = s.get("published") or []
received = v.get("received") or []
discovered = v.get("discovered") or []
instance = s.get("instance_name")

if not published:
    print("verify_run: FAILED — sensor published nothing "
          "(was sensor started after dashboard subscribed?)")
    sys.exit(1)

ok_recv = received == published
ok_disc = bool(discovered) and (instance is None or any(rec[1] == instance for rec in discovered))

print(f"verify_run: published={len(published)}  received={len(received)}  "
      f"discovered={len(discovered)}")
print(f"verify_run: received == published                : {'PASS' if ok_recv else 'FAIL'}")
print(f"verify_run: dashboard saw DiscoverObjectInstance : {'PASS' if ok_disc else 'FAIL'}")

if not ok_recv:
    pub_set, recv_set = set(published), set(received)
    only_pub = sorted(pub_set - recv_set)[:5]
    only_recv = sorted(recv_set - pub_set)[:5]
    print(f"verify_run:   only-published (first 5) = {only_pub}")
    print(f"verify_run:   only-received  (first 5) = {only_recv}")
if not ok_disc:
    print(f"verify_run:   discovered={discovered}  instance_name={instance!r}")

sys.exit(0 if (ok_recv and ok_disc) else 1)
PYEOF
