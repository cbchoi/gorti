# DLC conformance harness — gorti-only + parity-mode utilities

Shared scaffolding for the 27 DLC conformance fixtures under
`cppsdk/tests/dlc/conformance/<fixture>/`. Each fixture is one federate
scenario covering §4-§11 of IEEE 1516.1-2010; the driver runs the
federate against rtid (gorti-only mode) and optionally against Pitch
CRC (parity mode).

Layout (the C++ harness headers are owned by Agent C, the parity-mode
scripts + normalizer are owned by Agent E):

| File | Owner | Purpose |
|---|---|---|
| `rtid_runner.h` | Agent C | RAII wrapper around `bin/rtid` for gorti-only mode |
| `log_diff.h` | Agent C | C++ diff utilities |
| `golden_loader.h` | Agent C | Loads `expected.*.log` golden into memory |
| `normalize.py` | Agent E | Canonicalization per §5.2.1 (handle ints → `<H>`, RO bucket sort, TSO strict) |
| `pitch_build.sh` | Agent E | Compiles a fixture against Pitch headers (parity mode) |
| `pitch_run.sh` | Agent E | Starts Pitch CRC + runs Pitch-built binary (parity mode) |

## Pitch version pin

| Vendor | Version | Build |
|---|---|---|
| Pitch pRTI Free | 5.5.10 | 9905 |

`PRTI_VERSION` env var (optional) asserts the installed Pitch matches the
pin; `pitch_build.sh` warns if it does not. Newer Pitch releases may
change golden-output details and break the parity diff.

## Parity-mode opt-in

The parity leg of each fixture is **opt-in** via `PRTI_HOME`. Without
it set, `ctest -L parity` reports `SKIPPED (PRTI_HOME unset)` per
`docs/DLC_COMPLIANCE_PROGRAM.md §5.3`.

```bash
# Linux:
export PRTI_HOME=~/prti1516e
export PRTI_VERSION=5.5.10
ctest -L parity --output-on-failure
```

The parity leg's purpose is **bake-off evidence**: gorti and Pitch
produce the same canonical subscriber log for the same federate code
running against the same FOM. When they diverge, the tie-breaker is
the IEEE 1516.1-2010 spec text — Pitch is a vendor, not the spec
(see `docs/DLC_COMPLIANCE_PROGRAM.md §5.2.2` for worked examples and
`scripts/check-spec-traceability.sh` for the enforcement lint).

## CI behavior

GitHub Actions does **not** run the parity leg (Pitch license + CI
provisioning concerns). Developers run it locally before pushing
golden-touching changes. See `docs/PITCH_GOLDEN_LICENSING.md` for the
EULA review that gates check-in of Pitch-captured goldens.

## Normalizer reference

`normalize.py` is the single source of truth for canonicalization. It
is consumed by:

- Gorti-only diff: `canonicalize(gorti_log) == load_golden(expected.*.log)`
- Parity diff: `canonicalize(gorti_log) == canonicalize(pitch_log)`

Run standalone to inspect:

```bash
python3 cppsdk/tests/dlc/conformance/_harness/normalize.py \
    /path/to/raw.log -o /path/to/canonical.log
```
