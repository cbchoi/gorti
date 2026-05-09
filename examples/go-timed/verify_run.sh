#!/usr/bin/env bash
# Cross-result verifier — runs after all three *_run.sh complete.
set -euo pipefail
_HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_run_common.sh
source "${_HERE}/_run_common.sh"

python3 - "${RESULT_DIR}" "${CYCLES}" <<'PYEOF'
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

# Invariant 1: each federate received `expected` grants.
ok_counts = all(len(r["grants"]) == expected for r in results.values())

# Invariant 2: per-federate grant times non-decreasing.
ok_mono = True
for r in results.values():
    g = r["grants"]
    for i in range(1, len(g)):
        if g[i] < g[i-1]:
            ok_mono = False
            break

# Invariant 3: per-cycle min grant non-decreasing across cycles.
mins = [min(results[n]["grants"][c] for n in names) for c in range(expected)]
ok_lbts = all(mins[i] >= mins[i-1] for i in range(1, expected))

print(f"verify_run: federates={len(names)} cycles={expected}")
for n in names:
    print(f"  {n} (la={results[n]['lookahead']}, {results[n]['primitive']}): "
          f"grants={results[n]['grants']}")
print(f"verify_run: each federate got {expected} grants               : {'PASS' if ok_counts else 'FAIL'}")
print(f"verify_run: per-federate grants non-decreasing                : {'PASS' if ok_mono else 'FAIL'}")
print(f"verify_run: per-cycle min grant non-decreasing (LBTS-ish)     : {'PASS' if ok_lbts else 'FAIL'}")

sys.exit(0 if (ok_counts and ok_mono and ok_lbts) else 1)
PYEOF
