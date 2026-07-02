# IVCT-derived conformance subset (M35 scaffold)

Python-native conformance tests **inspired by** the SISO IVCT test catalog
(https://github.com/IVCT-team), re-implemented against gorti's Python SDK
(`pysdk/rti1516e`). This is **path (c)** from
`docs/RTI_CONFORMANCE_AUDIT.md` §6.

## Honest scoping

This suite is **NOT** IVCT. It is a parallel Python suite informed by
IVCT. Claiming "gorti passes IVCT" while these tests are the only
integration would be misleading. Legitimate paths to IVCT compliance are
tracked in `docs/RTI_CONFORMANCE_AUDIT.md` §6 options (a) and (b) —
either build a Java 1516.1 SDK for gorti (option a), or write a
Java↔gRPC federate-protocol bridge and run IVCT verbatim (option b).
Both are post-M35 milestones.

## Why re-implementing helps

- IVCT is Java and calls into a Java 1516.1 SDK; gorti has no Java
  SDK yet — nothing for IVCT to bind to.
- The IVCT test cases themselves are useful as **specifications**:
  they document exactly which §-level behavior a conformant RTI must
  exhibit. Reading them and re-implementing the assertions against
  `Rti1516eAmbassador` gives us a functional conformance surface without
  waiting for the Java SDK track to close.

## Layout

Each test file is a pytest module named `tc_<theme>.py`. The five
initial files mirror the IVCT federation-management, object-management,
time-management, and ownership-management test themes:

| File | IVCT analogue | Spec sections | Theme |
|---|---|---|---|
| `tc_create_join_resign.py` | TC-001 | §4.5-4.8 | Federation lifecycle |
| `tc_sync_point.py` | TC-002 | §4.11-4.15 | Synchronization points |
| `tc_object_pub_sub.py` | TC-005 | §5.6 + §6.6-6.11 | Object pub/sub round-trip |
| `tc_time_regulation.py` | TC-010 | §8.1-8.13 | Time regulation / constrained |
| `tc_ownership_divest.py` | TC-015 | §7.2-7.9 | Ownership divest/acquire |

IVCT TC numbers are gorti's approximation — the exact SISO mapping is
frozen at the time the M35+ implementer commits real bodies, from a
fresh read of the current IVCT `TestSuites/` catalog. Renumbering is
fine; the theme coverage is what matters.

## Current status — ALL GREEN (M39)

All 35 test bodies are implemented and pass as HARD assertions:
**35 pass + 0 xfail**. The M35-era xfails closed in three waves:

- M38 GA: §8.10 NER grants at next-TSO-message time (was: at LBTS).
- M38 GB: §7.2 old-owner update rejected AttributeNotOwned (per-instance
  ownership gate).
- M39 HA: pysdk translates the M37 wire events (§4.12 registration
  acks, §7.10 `ownership_unavailable`, §7.11
  `ownership_release_requested`) and exposes the M37 request surface
  (`two_phase`, `if_available`, `confirmDivestiture`,
  `synchronizationPointAchieved(successfully=)`,
  create/destroyFederationExecution with typed Annex-C exceptions) —
  the ownership flows now drive pysdk Layer 2 end-to-end instead of
  raw stubs.

| File | Pass | xfail |
|---|---|---|
| `tc_create_join_resign.py` | 6 | 0 |
| `tc_sync_point.py` | 6 | 0 |
| `tc_object_pub_sub.py` | 9 | 0 |
| `tc_time_regulation.py` | 6 | 0 |
| `tc_ownership_divest.py` | 8 | 0 |

Infrastructure:

- `conftest.py` — session-scoped `rtid_url` fixture spawning
  `$REPO/bin/rtid` (builds nothing) on kernel-picked ports with metrics
  relocated + admin disabled; per-test unique `federation_name`
  (≤32 bytes — gorti's eventlog rejects longer names).
- `_driver.py` — `Recorder` ambassador (thread-safe callback capture +
  deadline waits) and raw `rti.v1` FederationService stub helpers for
  the wire-level negative-path assertions in tc_create_join_resign.py
  (kept intentionally SDK-independent; the pysdk-level halves live in
  `pysdk/tests/spec/m39/test_m39_layer2_api.py`).
- `federation.fom.xml` — suite FOM. NB: pysdk's strict FOM parser
  rejects the four `*Advisory` switch elements that the cppsdk DLC
  fixtures carry; they are omitted here.

## Running

```bash
go build -o bin/rtid ./rti/cmd/rtid   # once per checkout / rtid change
python3.11 -m pytest tests/conformance/rti/ivct-subset -v
# If grpcio/protobuf are not installed for python3.11, reuse the repo venv:
PYTHONPATH=$PWD/.venv/lib/python3.11/site-packages \
  python3.11 -m pytest tests/conformance/rti/ivct-subset -v
```

The suite requires up-to-date generated Python stubs at
`pysdk/rti1516e/_generated` (`buf generate`, or
`python -m grpc_tools.protoc -Iproto --python_out=... --grpc_python_out=...`);
stale pre-M37 stubs fail the ownership tests on the missing
`if_available` / `two_phase` request fields.

CI: the `ivct` stage in `scripts/ci-gates.sh` (after `sweep`) runs this
suite in `.github/workflows/conformance.yml`.

## Follow-on work

- Expand toward the 30-test subset themes not yet covered (DDM, MOM,
  save/restore) per `RTI_CONFORMANCE_AUDIT.md` §6.
- Track long-term Java-based IVCT integration as a separate, deferrable
  milestone (paths a/b).

## References

- `docs/RTI_CONFORMANCE_AUDIT.md` — §6 for the honest-scoping context
  that drove path (c) over paths (a) and (b).
- `docs/DLC_COMPLIANCE_PROGRAM.md` — M31-M35 program.
- `pysdk/rti1516e/standard.py` — `Rti1516eAmbassador` surface these
  tests will drive.
- `pysdk/tests/spec/m28/test_pitch_typed_smoke.py` — reference pattern
  for spawning rtid + driving an ambassador against it.
