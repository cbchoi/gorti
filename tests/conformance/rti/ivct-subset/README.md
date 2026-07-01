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

## Current status — M35 SCAFFOLD

Every test body is `pytest.skip("stub — impl in follow-on")`. The
purpose of the scaffold is:

1. **Structural precedent** for a Python conformance suite that lives
   outside `pysdk/tests/spec/` (which is spec-traceability, not
   IVCT-inspired).
2. **Directory + naming convention** so follow-on milestones can add
   new `tc_*.py` files without re-litigating layout.
3. **CI wiring** by the same `pytest tests/conformance/rti/ivct-subset`
   invocation the DoD acceptance gate will use.

## Running

```bash
# Skipped everywhere until follow-on implements the bodies.
pytest tests/conformance/rti/ivct-subset -v
```

## Follow-on work

- Impl each `tc_*.py` body. See individual file docstrings for the
  IVCT-inspired assertion catalog per theme.
- Add `conftest.py` with a shared `rtid_url` fixture that spawns rtid
  the same way `pysdk/tests/spec/m28/test_pitch_typed_smoke.py` does.
- Expand from 5 initial files to a 30-test subset once the base bodies
  are green (matches `RTI_CONFORMANCE_AUDIT.md` §6 recommendation).
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
