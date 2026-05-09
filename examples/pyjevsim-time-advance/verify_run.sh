#!/usr/bin/env bash
set -euo pipefail
_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

"${PYTHON}" - "${RESULT_DIR}" "${CYCLES}" <<'PYEOF'
import json
import pathlib
import sys

d = pathlib.Path(sys.argv[1])
expected = int(sys.argv[2])
names = ["fast", "normal", "slow"]
missing = [n for n in names if not (d / f"{n}-result.json").exists()]
if missing:
    print(f"verify_run: FAILED — missing results: {missing}")
    sys.exit(1)

results = {n: json.loads((d / f"{n}-result.json").read_text()) for n in names}
ok_count = all(len(r["grants"]) == expected for r in results.values())
ok_mono = True
for r in results.values():
    g = r["grants"]
    for i in range(1, len(g)):
        if g[i] < g[i-1]:
            ok_mono = False
            break

print(f"verify_run: federates={len(names)} cycles={expected}")
for n in names:
    print(f"  {n} (la={results[n]['lookahead']}): grants={results[n]['grants']}")
print(f"verify_run: each federate got {expected} grants  : {'PASS' if ok_count else 'FAIL'}")
print(f"verify_run: per-federate non-decreasing          : {'PASS' if ok_mono else 'FAIL'}")
sys.exit(0 if (ok_count and ok_mono) else 1)
PYEOF
