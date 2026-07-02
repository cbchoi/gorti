# xlang_python_cpp_pubsub — Python publisher + C++ DLC subscriber

**Spec:** IEEE 1516.1-2010 §6 (Object Management) + Annex B (encoding). Verifies that gorti's pysdk (M28 typed-handle path at `pysdk/rti1516e/standard.py`) and cppsdk DLC surface produce identical wire behavior against the same rtid.

**Owns catalogue rows (cross-language enforcement):** 4.19 (`discoverObjectInstance` 2-overload set — wstring name regardless of pub-side language), 4.20 (`reflectAttributeValues` 3-overload set), 11.3 (`updateAttributeValues` mandatory tag), 14.2 (BasicDataElements — `HLAfloat64BE` wire format consistency).

**Gorti-only.** Pitch Free 5.5.10 ships no Python binding (see catalogue §7.4 of `docs/M31_DISPATCH_PLAN.md`), so this fixture has no parity leg.

## Scenario

1. Test driver spawns rtid.
2. C++ DLC subscriber (`federate_subscriber.cpp`) joins as `cpp-sub`, subscribes `Vehicle.Position` (active).
3. Python publisher (`python_pub.py`) joins as `py-pub`, publishes `Vehicle.Position`, registers `car-1`, emits 3 RO updates with values `10.0 / 20.0 / 30.0` encoded as **HLAfloat64BE** (big-endian IEEE 754 double per Annex B), then resigns with `CANCEL_THEN_DELETE_THEN_DIVEST`.
4. C++ subscriber sees DISCOVER → 3 REFLECTs with byte-identical values → REMOVE → exits.

## Why this fixture exists

The two SDKs share the rtid wire protocol but use completely different code paths:

| Surface | Python side (pysdk) | C++ side (cppsdk DLC) |
|---|---|---|
| Ambassador | `Rti1516eAmbassador` (Layer 2 over Layer 1 asyncio) | `rti1516e::RTIambassador*` (factory-vended) |
| Handle | `ObjectClassHandle` typed wrapper (M28) | `rti1516e::ObjectClassHandle` class (catalogue 7.1) |
| HLAfloat64BE | `struct.pack(">d", value)` | `rti1516e::HLAfloat64BE` class with `encode()/decode()` (catalogue 14.2) |
| Name | `str` | `std::wstring` (DLC) |

The fixture is the runtime witness that all four columns map to the same gRPC payload bytes. If any one diverges — e.g. cppsdk DLC accidentally encodes HLAfloat64BE as little-endian — the subscriber's REFLECT lines won't match the publisher's emitted values.

## Build prerequisites

- `bin/rtid` (gorti RTI binary)
- Python 3.11+ with pysdk installed (`pip install -e pysdk`)
- cppsdk DLC subscriber binary built and on disk (`conf_xlang_python_cpp_pubsub_federate_subscriber`)
- `PYTHONPATH` includes `<repo>/pysdk` (test driver sets this)

## Files

- `python_pub.py` — Python publisher (uses pysdk M28)
- `federate_subscriber.cpp` — C++ DLC subscriber
- `federation.fom.xml`
- `expected.python_pub.log`
- `expected.cpp_sub.log`
- `test_xlang_python_cpp_pubsub.cpp`

## parity-CF verdict (M35 wave 2)

**PARTIAL 16/17** (python_pub **8/8**, cpp_sub **8/9**) — and the
fixture's core claim, cross-language byte-identity, is **fully proven**:
all three pysdk-emitted `struct.pack(">d", v)` HLAfloat64BE payloads
were decoded byte-identically by the DLC `rti1516e::HLAfloat64BE`
decoder (`REFLECT Position=10.000000 / 20.000000 / 30.000000` verbatim),
plus DISCOVER carried the reserved name `car-1` across the str→wstring
boundary. Catalogue rows 4.19 / 4.20 / 14.2 witnessed end-to-end.

Sole missing event: `SUB: REMOVE`. Root cause traced (server never
emits it in this scenario — not a DLC decode gap):

- pysdk Layer 2 `resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST")`
  **discards the action** (`del action  # accepted for API compat`,
  `pysdk/rti1516e/standard.py` resignFederationExecution) and Layer 1
  always puts `RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES` on the
  wire (`pysdk/rti1516e/_transport.py:423`).
- With no delete-objects resign semantics requested, rtid performs no
  instance deletion on resign, so no remove event exists for the C++
  subscriber to receive. (Consistent with the M34 om_helloworld_pubsub
  capture, which also shows no REMOVE after publisher resign.)
- Secondary (masked) gap, inherited from parity-CC's om_delete_object_tso
  finding: even when the server emits remove=12, the DLC-side
  removeObjectInstance bridge converter is absent — so the REMOVE line
  would still be missing until both gaps close.

Run/environment notes (for reproduction):
- Interpreter: `~/.local/bin/python3.11` with
  `PYTHONPATH=<worktree>/pysdk:<repo>/.venv/lib/python3.11/site-packages`
  (the repo `.venv/bin/python` symlink now resolves to system 3.12 and
  cannot see its own 3.11 site-packages — venv broken by an OS upgrade).
- `pysdk/rti1516e/_generated` (rti.v1 proto stubs) must exist; in a
  worktree, symlink it from the main checkout (gitignored, like
  `cppsdk/_generated`).
- `python_pub.py` fix: dropped the `tag=b""` kwarg — pysdk M28
  `updateAttributeValues(object_handle, values, timestamp=None)` has no
  tag parameter. The §6.10 mandatory user-supplied tag is a pysdk
  surface divergence (catalogue 17.1); the golden locks value bytes,
  not the tag, so the fixture stays valid.
- `federate_subscriber.cpp`: pump switched to the suite-standard
  evoke-drain `evokeMultipleCallbacks(0.05, 0.1)` with early exit on
  REMOVE (golden unchanged).

## M36 DD re-verdict (2026-07-02)

**SPEC-FULL — 17/17 lines** (python_pub **8/8**, cpp_sub **9/9**), up
from PARTIAL 16/17 (missing `SUB: REMOVE`). Run: worktree rtid, Python
publisher on the worktree pysdk (venv site-packages +
`~/.local/bin/python3.11` — repo venv symlink broken post-OS-upgrade),
C++ subscriber built from the worktree (DA C++ layer merged).
Canonicalized with `_harness/normalize.py`; inline `#` citations
stripped from goldens before diff.

The missing REMOVE required BOTH halves that landed in M36:
- **pysdk (DD-1)**: `standard.py resignFederationExecution` no longer
  discards the action (`del action`); the IEEE §4.10 designator is
  threaded through the federate context manager and mapped to the
  `rti.v1.ResignAction` wire enum in `_transport.py`
  (`resign_action_to_proto`). `CANCEL_THEN_DELETE_THEN_DIVEST` now
  reaches the server, whose M24 resign dispatch deletes the
  publisher's `car-1` instance.
- **cppsdk (DA-2, merged)**: `removeObjectInstance` wire→callback
  delivery so the subscriber actually observes the delete.

Residual: none at fixture scope. The §6.10 mandatory-tag pysdk surface
divergence noted by parity-CF stands (golden locks value bytes, not
the tag).
