# threading_callback_reentry — ambassador re-entry inside a callback must throw

**Spec:** IEEE 1516.1-2010 §10 (Support Services) — RTI services are not
re-entrant from callback context. `CallNotAllowedFromWithinCallback` is the
required exception class for this case.

**API surface:** `CallNotAllowedFromWithinCallback` and the runtime check
that raises it. The fixture also exercises the §6.11 reflect callback
shape and `updateAttributeValues` with its mandatory tag.

## Purpose

Other tests lock the exception class via `static_assert`, but only this
fixture watches the RTI runtime actually throw it.

Without a runtime witness, an implementation could ship the exception class as a static type and never raise it — passing every lockfile while violating the spec. The fixture catches that drift.

## Scenario

- **publisher** registers `car-1`, sends one RO `Position=42.0` update, resigns.
- **subscriber** subscribes `Vehicle.Position`. Inside its `reflectAttributeValues` callback, it calls `amb->updateAttributeValues(theObject, theAttributeValues, theUserSuppliedTag)` — a re-entry attempt.

Expected RTI behavior (per §10): the ambassador throws `rti1516e::CallNotAllowedFromWithinCallback`. The subscriber catches it, logs `CAUGHT exception=CallNotAllowedFromWithinCallback`, and continues normally to resign.

## Expected trace

| Subscriber golden line | Spec sentence |
|---|---|
| `REFLECT Position=...` | §6.11 — callback fires |
| `ATTEMPT_REENTRY service=updateAttributeValues` | (test harness witness — federate is about to call amb) |
| `CAUGHT exception=CallNotAllowedFromWithinCallback` | §10 — exact spec exception type |
| `RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST` | §4.10 — re-entry guard does not put ambassador into bad state; subsequent (non-reentered) call succeeds |

A run producing `CAUGHT exception=UNEXPECTED(...)` is a bug. A run with no `CAUGHT` line at all (the re-entry call returned successfully) is a worse bug.

## Files

- `federate_publisher.cpp`
- `federate_subscriber.cpp` (the actual subject of the test)
- `federation.fom.xml`
- `expected.publisher.log`
- `expected.subscriber.log`
- `test_threading_callback_reentry.cpp`

## Status

**SPEC-FULL 7/7 on both sides.** Run against a fresh gorti rtid at
127.0.0.1:8080 with both federates from this directory, then canonicalize
via `_harness/normalize.py`.

`gorti::dlc::CallbackScope`
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

The committed golden's witness line

    SUB: CAUGHT exception=CallNotAllowedFromWithinCallback

matches exactly. The
subsequent post-callback `resignFederationExecution` still succeeds —
the guard clears on dispatch return. Unit witnesses:
`_runtime/test_callback_bridge.cpp` BridgeReentrancy tests (flag set
during dispatch, cleared after; nested save/restore).
