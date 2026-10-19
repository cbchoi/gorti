# Python HLA Campaign

## Current CI entry point

Run `python scripts/validate_sdks.py python` from the repository root after
installing `pysdk[dev]` and generating the protocol bindings. The same command
is used by manual CI and release preflight. It builds current `rtid` source,
enables live tests, selects the HLA files in `hla_campaign.json`, and writes
JUnit output outside the repository. Legacy C++ interoperability, RL/PPO and
standalone model applications are explicitly excluded by category.

The inventory was updated on 18 October 2026. New files require classification;
the runner fails if a file is missing from the inventory. This does not update
the historical exact-node-ID baseline or replace a requirements evidence review.

The order-completion tests run without a private copy of the IEEE source PDF.
Set `GORTI_IEEE1516_1_SOURCE_PDF` to verify a local copy against the reviewed
document hash when recording evidence. An explicitly supplied missing or
mismatched document fails validation; an omitted document is recorded as
`verified_this_run: false`. Service behavior assertions run in either case.

The sections below describe the older `verify_python_hla.py` evidence campaign.
Its exact-node-ID baseline and legacy source/publisher identity records must be
reviewed before using it for a new full campaign. It is not the CI entry point.

## Inventory

`hla_campaign.json` classifies every `test_*.py` file under `pysdk/tests`.
All historical HLA roots are selected: m4, m5, m12, m14, m17, m21, m22,
m23, m24, m25, m26, m27, m28, m36 and m39. Top-level HLA tests include
live services, ownership, TLS, callback delivery, host metadata, and the gate
and fixture regressions. The previous bridge/HLA time and order boundary tests
remain selected. No selection is based on whether a test passes.

RL/PPO applications, standalone pyjevsim bridge/model applications, and the
cross-package static mypy/ruff gate are separately classified, not silently
omitted or counted as passing. The environment report lists each excluded path
and its classification. New or removed test files require inventory review.

`hla_expected_nodeids.txt` is the checked-in, reviewable collection baseline.
Collection refresh does not update this file or certify the campaign. Review
the candidate IDs, then update the baseline as a source change. Unexpected,
missing or duplicate IDs fail the campaign.

## Reproduce

Use a dedicated environment, not a global Python installation. The recorded
Windows campaign uses `D:/workspace/gorti/n04-venv/Scripts/python.exe` with
Python 3.14. The exact required dependency versions, including cryptography
and its certificate-generation prerequisites, are in `hla_requirements.txt`.
For a new environment, install that file with its environment's Python:

```powershell
& $python -m pip install -r pysdk/tests/hla_requirements.txt
$env:GOCACHE = 'D:/workspace/gorti/.gocache'
$env:GOMODCACHE = 'D:/workspace/gorti/.tools/gomodcache'
& $python -B scripts/verify_python_hla.py --artifacts D:/workspace/gorti/hla-run-UNIQUE
```

Run from the repository root. The evidence directory must be new and outside
the repository. To inspect collection without running tests:

```powershell
& $python -B scripts/verify_python_hla.py --collect-only --artifacts D:/workspace/gorti/hla-collect-UNIQUE
```

The runner enables live tests, disables inherited pytest filtering and plugin
autoload, clears external/prebuilt RTID overrides, and isolates temporary
files. Server fixtures build current source and use loopback ports, an ephemeral
admin listener and external save/log locations. No protocol generation or
production SDK installation is performed. The C++ interoperability test accepts
`GORTI_CPP_PUBLISHER` pointing to an externally built publisher for the current
platform; its prerequisite is not manufactured or downloaded by this runner.

For a WSL campaign, use a dedicated Linux Python environment with the same
dependency pins, Linux Go/cache paths, and a Linux publisher path. A Windows
virtual environment or publisher executable is not a substitute. The runner
uses Python-stream output capture because pytest's anonymous file-descriptor
capture can fail when temporary files reside on WSL's Windows-mounted drive.
Subprocess output remains in the external campaign and participant logs.

## Evidence And Gate

- `environment.json`: interpreter, packages, dependency mismatches, Go version,
  optional publisher path/hash, and full inclusion/exclusion inventory.
- `source-before.json`, `source-after.json`: relevant source and fixture hashes.
  Includes C++ SDK headers/sources and the publisher example. Source drift
  during a run prevents a clean result, including anticipated parallel edits.
- `cpp-publisher-before.json`, `cpp-publisher-after.json`: the selected publisher
  path and binary hash (null when absent). A rebuild during the run fails the gate.
- `test-environment.json`: controlled campaign settings, without ambient secrets.
- `pytest.log`, `junit.xml`, `pytest.json`: raw output, JUnit, exact collection,
  per-phase outcomes, traceback/skip reasons, and pytest exit status.
- `candidate-nodeids.txt`: collection output for review, never an automatic waiver.
- `current-test.txt`: active or most recently started case for progress diagnosis.
- `summary.json`: expected/collected ID comparison and final gate decision.

Exit 0 requires every expected case to complete setup, call and teardown
successfully, without collection errors, skips, xfail/xpass, missing results,
dependency mismatches or source drift. An unavailable C++ publisher remains a
visible failure of this gate. A nonzero run can still provide useful failure
evidence; it is not a full pass.

The four historical M21 IDs now execute live subprocess scenarios. The Go
federate is built from `fixtures/time_peer/main.go`; Python federates use
`fixtures/time_peer.py`. They verify both Go/Python TSO directions, mixed
TAR/TARA/NMR/NMRA/FQR requests, and ten cycles through the existing HLAFederate
adapter with the repository's GrantDrivenModel. This adapter test does not
claim coverage of an installed upstream pyjevsim engine. Every participant
reports a distinct process ID and federation handle, and JSON command/callback
logs are retained outside the repository.

M6 also repaired the authorized missing OwnershipAcquisitionPending exception
class/export, discovered by the server-to-SDK mapping test. The callback oracle
explicitly classifies local_validation_barrier as an internal wire marker and
tests that it never reaches HLA callback delivery; all other wire variants
remain subject to exhaustive translation checks.

Do not weaken assertions, waive skips, reuse stale binaries or shorten the
inventory for a green result. Raw evidence stays outside Git. After focused
verification, wait for the coordinated source freeze before the final campaign.
