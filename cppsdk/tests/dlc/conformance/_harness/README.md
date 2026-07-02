# DLC conformance harness — gorti-only + parity-mode utilities

Shared scaffolding for the 27 DLC conformance fixtures under
`cppsdk/tests/dlc/conformance/<fixture>/`. Each fixture is one federate
scenario covering §4-§11 of IEEE 1516.1-2010. The driver runs the federate
against rtid (gorti-only mode, always-on) and optionally against Pitch CRC
(parity mode, gated on `PRTI_HOME`).

## Layout

The C++ headers are owned by Agent C (TASK-348); the Python/shell parity-mode
scaffolding is owned by Agent E (TASK-351..353). Together they form the
complete `_harness/`:

| File | Owner | Role |
|---|---|---|
| `rtid_runner.h` | C | RAII wrapper around `bin/rtid`: picks a free port, forks the daemon, kills in dtor. Exposes `crcAddress()` for `RTIambassador::connect()` localSettings. |
| `log_diff.h` | C | C++ canonical-log diffing: `normalizeHandles()` replaces `handle=<int>` with `handle=<H>` per `docs/DLC_COMPLIANCE_PROGRAM.md §5.3.1` rule 1; `bucketSortRO()` sorts RO events within LBTS buckets per rule 2; TSO events stay strict per rule 3. |
| `golden_loader.h` | C | Loads `expected.*.log` golden files, strips `#`-comments (used for spec citations and `// TBD-pitch-capture` markers), provides `diffAgainstGolden()` returning a unified-diff-ish summary. |
| `normalize.py` | E | Python re-implementation of the same canonicalization rules — used by parity-mode for cross-RTI comparison. **Must stay in sync with `log_diff.h`.** |
| `pitch_build.sh` | E | Compiles a fixture's federate source against Pitch headers (`$PRTI_HOME/api/cpp/HLA_1516-2010/`) when `PRTI_HOME` is set. |
| `pitch_run.sh` | E | Starts the Pitch CRC + runs the Pitch-built fixture binary; captures the canonical log for diff against the gorti round. |
| `run_fixture.sh` | ED (M37) | Deterministic multi-federate driver: reads `<fixture>/driver.conf` (roles, launch order, wait-for gates, args/env), serializes on `/tmp/gorti-rtid-8989.lock`, runs a fresh rtid, and prints per-role FULL/PARTIAL verdicts (canonical capture vs comment-stripped golden via `normalize.py`). `--capture` refreshes `gorti-captured.<role>.log`. |

## Why header-only on the C++ side

Per `docs/M31_DISPATCH_PLAN.md §2` non-goals: no impl code lands in M31. The
harness is utility code (no spec API surface) so the small header-only form
is acceptable; M32+ may promote to a static library if compile times suffer.

## Pitch version pin

| Vendor | Version | Build |
|---|---|---|
| Pitch pRTI Free | 5.5.10 | 9905 |

`PRTI_VERSION` env var (optional) asserts the installed Pitch matches the pin;
`pitch_build.sh` warns if it does not. Newer Pitch releases may change
golden-output details and break the parity diff. See
`docs/DLC_COMPLIANCE_PROGRAM.md §5.3` for the pin rationale.

## Coordination

Two implementations of the canonicalization rules (one C++, one Python) MUST
stay in sync. A future task (M32+) may unify them, but for M31 we ship both:
- C++ `log_diff.h` is used by gorti-only test drivers (gtest-based).
- Python `normalize.py` is used by parity-mode bash drivers when diffing
  gorti log vs Pitch log (the 2026-06-30 smoke pattern).

## Fixtures using this harness

All 27 fixtures across Agents C + D:
- C owns: `fm_*` (5), `dm_*` (2), `om_*` (7), `own_*` (4) — 18 total
- D owns: `tm_*` (4), `ddm_*` (2), `mom_*` (1), `threading_*` (1), `xlang_*` (1) — 9 total
