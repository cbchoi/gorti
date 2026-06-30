# `_harness/` — shared conformance fixture utilities (TASK-348)

Header-only utility code shared by all 27 conformance fixtures under
`cppsdk/tests/dlc/conformance/`. Per `docs/M31_DISPATCH_PLAN.md §2.2`:

| File | Role |
|---|---|
| `rtid_runner.h` | RAII wrapper around `bin/rtid`: picks a free port, forks the daemon, kills it in dtor. Exposes `crcAddress()` for `RTIambassador::connect()` localSettings. |
| `log_diff.h` | Canonical-log diffing: `normalizeHandles()` replaces `handle=<int>` with `handle=<H>` per `docs/DLC_COMPLIANCE_PROGRAM.md §5.3.1` rule 1; `bucketSortRO()` sorts RO events within LBTS buckets per rule 2; TSO events stay strict per rule 3. |
| `golden_loader.h` | Loads `expected.*.log` golden files, strips `#`-comments (used for spec citations and `// TBD-pitch-capture` markers), provides `diffAgainstGolden()` returning a unified-diff-ish summary. |

## Why header-only

Per `docs/M31_DISPATCH_PLAN.md §2 non-goals` no impl code lands in M31.
The harness is utility code (no spec API surface) so a small header-only
form is acceptable; M32+ may promote to a static library if compile times
suffer.

## Coordination

Owned by Agent C per `ralph.md §1`. Agent E's `tests/parity/normalize.py`
(TASK-353) re-implements the same canonicalization in Python for the
cross-RTI parity harness — the two implementations must stay in sync.

## Fixtures using this harness

All 18 fixtures Agent C owns:

- `fm_*` (5 fixtures)
- `dm_*` (2 fixtures)
- `om_*` (7 fixtures)
- `own_*` (4 fixtures)

Plus Agent D's 9 fixtures (`tm_*`, `ddm_*`, `mom_*`, `threading_*`,
`xlang_*`) which also include these headers via
`#include "../_harness/rtid_runner.h"`.

## M31 status

These headers parse standalone with `g++ -std=c++17 -c`. RAII semantics
exercised by every fixture's `test_<name>.cpp` driver — failures here
manifest as link-stage `undefined reference` errors against the still-
unimplemented `rti1516e::*` surface, which is the M31 expected RED.
