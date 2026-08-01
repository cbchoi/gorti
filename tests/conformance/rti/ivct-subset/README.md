# IVCT-derived conformance subset

This directory contains Python-native RTI conformance tests derived from and
inspired by scenarios in the [SISO IVCT project](https://github.com/IVCTool).
The tests drive gorti's `pysdk/rti1516e` API against a live `rtid` process.

This is not the official Java IVCT suite, has not been certified by SISO, and
does not provide formal IVCT certification. Passing it means only that gorti
passes the scenarios implemented here.

## Current coverage

The suite contains 35 tests:

| File | Tests | Coverage |
|---|---:|---|
| `tc_create_join_resign.py` | 6 | Create, join, resign, destroy, handle allocation, and invalid lifecycle calls |
| `tc_sync_point.py` | 6 | Registration, announcement, tags, completion, duplicate labels, and participant subsets |
| `tc_object_pub_sub.py` | 9 | Publication, registration, discovery, updates, removal, resign cleanup, and name reservation |
| `tc_time_regulation.py` | 6 | Regulation, constrained mode, lookahead, time advance grants, next-event delivery, and regulator blocking |
| `tc_ownership_divest.py` | 8 | Divestiture, acquisition, unavailable/release callbacks, ownership queries, and update authorization |

The suite is intentionally narrower than either the upstream IVCT catalog or
the acceptance surface in `engineering/specifications/current/STD.md`. It does
not run the upstream Java harness and does not cover interaction services, DDM,
MOM, save/restore, interoperability with an external RTI, or the full
declaration, failure, ordering, and accounting matrix required by the STD.
Those areas may have executable evidence elsewhere in the repository, but are
not claims made by this subset.

One object-management test conditionally reports `xfail` when an RTI does not
provide discovery catch-up for an object registered before a late subscription.
There are no other skip or expected-failure paths in these 35 tests.

## Layout

- `tc_*.py` contains the executable scenarios. `pytest.ini` enables this
  IVCT-style filename convention.
- `conftest.py` starts one existing `bin/rtid` for the test session and creates
  a unique federation name for each test.
- `_driver.py` provides the recording ambassador, callback waits, and the few
  low-level gRPC helpers used for wire-level negative cases.
- `federation.fom.xml` is the suite FOM.

## Prerequisites

Run from the repository root with:

- Python 3.11 and `pytest`, `pytest-asyncio`, `grpcio`, and `protobuf`;
- current generated Python stubs under `pysdk/rti1516e/_generated` (install
  `pysdk[dev]` and run `python -m rti1516e._proto` after protocol changes); and
- a current `bin/rtid` built with
  `go build -o bin/rtid ./rti/cmd/rtid`.

Run the suite directly:

```bash
python -m pytest tests/conformance/rti/ivct-subset -v
```

With the prerequisites already prepared, the repository gate wrapper is:

```bash
scripts/ci-gates.sh ivct
```

## CI

`.github/workflows/conformance.yml` is the manually dispatched Conformance
workflow. It installs the Python 3.11 dependencies, generates stubs, builds
`rtid`, and invokes the `ivct` stage in `scripts/ci-gates.sh` after the other
conformance gates.

## References

- `engineering/specifications/current/STD.md` defines the current test levels,
  required scenarios, executable evidence, and acceptance limits.
- `tests/conformance/rti/ivct-subset/tc_*.py` is the executable evidence
  described by this README.
- `scripts/ci-gates.sh` is the executable local and CI gate definition.
- `pysdk/rti1516e/standard.py` is the Python RTI ambassador surface under test.
- `cppsdk/tests/dlc/conformance/` contains related C++ executable conformance
  fixtures; it is separate from this IVCT-derived subset.
