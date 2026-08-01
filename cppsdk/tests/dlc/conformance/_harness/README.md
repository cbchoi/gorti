# DLC conformance harness — gorti and parity utilities

Shared utilities for the 27 DLC conformance fixtures under
`cppsdk/tests/dlc/conformance/<fixture>/`. Each fixture is one federate
scenario covering §4-§11 of IEEE 1516.1-2010. The driver runs the federate
against rtid (gorti-only mode, always-on) and optionally against a reference RTI server
(parity mode, gated on explicit local include and library paths).

## Layout

| File | Role |
|---|---|
| `rtid_runner.h` | RAII wrapper around `bin/rtid`: picks a free port, forks the daemon, kills in dtor. Exposes `gortiAddress()` for `RTIambassador::connect()` localSettings. |
| `log_diff.h` | C++ canonical-log diffing: `normalizeHandles()` replaces `handle=<int>` with `handle=<H>`; `bucketSortRO()` sorts RO events within LBTS buckets; TSO events remain strict. |
| `golden_loader.h` | Loads `expected.*.log` golden files, strips `#`-comments used for spec citations, and provides `diffAgainstGolden()` returning a unified-diff-ish summary. |
| `normalize.py` | Python re-implementation of the same canonicalization rules, used by parity-mode for cross-RTI comparison. **Must stay in sync with `log_diff.h`.** |
| `commercial_rti_build.sh` | Compiles a fixture against the licensed IEEE 1516.1-2010 headers and libraries selected by `REFERENCE_RTI_INCLUDE_DIR` and `REFERENCE_RTI_LIBRARY_DIR`. |
| `commercial_rti_run.sh` | Runs the compiled fixture against an online reference RTI, optionally starting a locally configured server executable. |
| `run_fixture.sh` | Deterministic multi-federate driver: reads `<fixture>/driver.conf` (roles, launch order, wait-for gates, args/env), serializes on `/tmp/gorti-rtid-8080.lock`, runs a fresh rtid, and prints per-role FULL/PARTIAL verdicts (canonical capture vs comment-stripped golden via `normalize.py`). `--capture` refreshes `gorti-captured.<role>.log`. |

## Header-only C++ support

The harness is utility code with no spec API surface, so the small
header-only form is acceptable. It may be promoted to a static library if
compile times suffer.

## Reference RTI configuration

The optional parity adapter uses the reference RTI selected by
`REFERENCE_RTI_INCLUDE_DIR` and `REFERENCE_RTI_LIBRARY_DIR`. Provider and
build identifiers are kept local. A runtime update may change observable
details and requires a fresh parity review.

## Coordination

Two implementations of the canonicalization rules (one C++, one Python) MUST
stay in sync:
- C++ `log_diff.h` is used by gorti-only test drivers (gtest-based).
- Python `normalize.py` is used by parity-mode bash drivers when diffing
  gorti log vs reference RTI log.

## Fixtures using this harness

All 27 fixtures use this harness:
- `fm_*` (5), `dm_*` (2), `om_*` (7), `own_*` (4) — 18 total
- `tm_*` (4), `ddm_*` (2), `mom_*` (1), `threading_*` (1), `xlang_*` (1) — 9 total
