# threading_callback_reentry — ambassador re-entry inside a callback must throw

**Spec:** IEEE 1516.1-2010 §10 (Support Services) — RTI services are not re-entrant from callback context. **`docs/DLC_DIVERGENCE_CATALOGUE.md §17.2`** — `CallNotAllowedFromWithinCallback` is the spec-mandated exception class for this case.

**Owns catalogue rows:** **17.2** (`CallNotAllowedFromWithinCallback` exception class + the runtime check that fires it). Also exercises 4.20 (reflect callback shape) and 11.3 (`updateAttributeValues` with mandatory tag).

## Why this fixture is critical

This is the **only fixture in the M31 suite** that exercises catalogue row 17.2. Other fixtures lock the exception class via `static_assert` lockfile tests, but only this one watches the RTI runtime actually throw it.

Without a runtime witness, an implementation could ship the exception class as a static type and never raise it — passing every lockfile while violating the spec. The fixture catches that drift.

## Scenario

- **publisher** registers `car-1`, sends one RO `Position=42.0` update, resigns.
- **subscriber** subscribes `Vehicle.Position`. Inside its `reflectAttributeValues` callback, it calls `amb->updateAttributeValues(theObject, theAttributeValues, theUserSuppliedTag)` — a re-entry attempt.

Expected RTI behavior (per §10): the ambassador throws `rti1516e::CallNotAllowedFromWithinCallback`. The subscriber catches it, logs `CAUGHT exception=CallNotAllowedFromWithinCallback`, and continues normally to resign.

## What the golden enforces

| Subscriber golden line | Spec sentence |
|---|---|
| `REFLECT Position=...` | §6.11 — callback fires |
| `ATTEMPT_REENTRY service=updateAttributeValues` | (test harness witness — federate is about to call amb) |
| `CAUGHT exception=CallNotAllowedFromWithinCallback` | §10 + catalogue 17.2 — exact spec exception type |
| `RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST` | §4.10 — re-entry guard does not put ambassador into bad state; subsequent (non-reentered) call succeeds |

A run producing `CAUGHT exception=UNEXPECTED(...)` is a bug. A run with no `CAUGHT` line at all (the re-entry call returned successfully) is a worse bug.

## Files

- `federate_publisher.cpp`
- `federate_subscriber.cpp` (the actual subject of the test)
- `federation.fom.xml`
- `expected.publisher.log`
- `expected.subscriber.log`
- `test_threading_callback_reentry.cpp`

## parity-CF verdict (M35 wave 2)

**BLOCKED(no re-entrancy guard — FR-DLC-14 / catalogue 17.2 unimplemented).**

Run: gorti rtid @ 127.0.0.1:8989, fresh instance, both federates from this
directory, canonicalized via `_harness/normalize.py`.

- Publisher: **7/7** lines byte-identical to the spec-derived golden.
- Subscriber: **6/7** — the only divergent line is the fixture's raison
  d'être, `SUB: CAUGHT exception=CallNotAllowedFromWithinCallback`.

Observed behavior of the re-entry probe (precise, reproducible across runs):

1. The re-entered `updateAttributeValues` does **not** deadlock and does
   **not** throw `CallNotAllowedFromWithinCallback`. No client-side
   callback-context check exists anywhere in `cppsdk/src/dlc/**` — the
   exception class is declared (`cppsdk/src/dlc/Exception.cpp:48`) but
   never raised at runtime.
2. The call is transmitted to rtid **from inside the callback context**
   and completes a full gRPC round-trip (the update path is a unary RPC,
   so the M17 header's stream-mutex deadlock warning does not bite here).
3. rtid rejects it server-side for an unrelated reason — the subscriber
   does not own `Position` — and the DLC surfaces that as
   `RTIinternalError: updateAttributeValues: attribute not owned by
   federate [op=updateAttributeValues]` (captured as
   `CAUGHT exception=UNEXPECTED(...)` + `ERROR no_exception_raised`).
4. The ambassador is not corrupted: the subsequent non-re-entered
   `resignFederationExecution` succeeds (`SUB: RESIGN` matches golden).

Implication: had the subscriber owned the attribute, the re-entered call
would have **silently succeeded** — the "worse bug" case named above.
The missing piece is a DLC-side in-callback flag set around callback
dispatch in the evoke path, checked at every ambassador entry point.
Implementing it is out of parity scope; this verdict documents the gap.

Fixture-code delta for capture: publisher reservation wait loop switched
from `evokeCallback(0.1)` to the suite-standard evoke-drain
`evokeMultipleCallbacks(0.05, 0.1)` (no behavioral change to the golden).

## M36 agent-DA verdict — SPEC-FULL 7/7 both sides

FR-DLC-14 (catalogue 17.2) is implemented. `gorti::dlc::CallbackScope`
(a save/restore RAII over `thread_local bool tls_in_callback`,
`cppsdk/src/dlc/FederateAmbassadorBridge.{h,cpp}`) marks the callback
context around every DLCFederateAmbassadorBridge dispatch; the DLC
ambassador's `bridge()`/`bridgeR()` helpers
(`cppsdk/src/dlc/RTIambassadorImpl.cpp`) throw the spec-mandated
`CallNotAllowedFromWithinCallback` before any wire traffic when
re-entered. §10.4-exempt services (`evokeCallback`,
`evokeMultipleCallbacks`, `enableCallbacks`, `disableCallbacks`) route
through unguarded forms, so evoke-drain loops inside callbacks stay
legal.

Captured run (`gorti-captured.{publisher,subscriber}.log`): the golden's
witness line

    SUB: CAUGHT exception=CallNotAllowedFromWithinCallback

now matches exactly (M35 capture showed the re-entered call reaching
rtid and failing for the unrelated attribute-ownership reason). The
subsequent post-callback `resignFederationExecution` still succeeds —
the guard clears on dispatch return. Unit witnesses:
`_runtime/test_callback_bridge.cpp` BridgeReentrancy tests (flag set
during dispatch, cleared after; nested save/restore).
