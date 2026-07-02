# gorti DLC layer — implementation notes

## Error-string sniffing (M37 Agent EC-3) — STOPGAP

`translateBridgeError()` in `RTIambassadorImpl.cpp` maps M17 bridge errors to
the spec `<RTI/Exception.h>` types in two passes:

1. **Prefix match** on the M17 exception class name that
   `M17Bridge.cpp guard()` prepends (`"NotConnected: ..."` etc.) — stable,
   contract-level.
2. **Detail-string sniff** on the server-produced gRPC status message, for
   rejections the M17 client folds into `RTIinternalError`. This pass is a
   STOPGAP pinned to gorti's exact error strings in
   `rti/internal/core/errors.go`:

   | server string (substring matched)                                  | DLC exception                  |
   |--------------------------------------------------------------------|--------------------------------|
   | `interaction class not published`                                   | `InteractionClassNotPublished` |
   | `not published` (any other)                                         | `ObjectClassNotPublished`      |
   | `lookahead must be non-negative`                                    | `InvalidLookahead`             |
   | `requested time is not greater than current logical time`           | `LogicalTimeAlreadyPassed`     |
   | `invalid logical time` or `lookahead` (any other)                   | `InvalidLogicalTime`           |

   If those Go error strings change, this mapping silently degrades back to
   `RTIinternalError` (fixtures downgrade from FULL, nothing crashes). The
   real fix is structured error codes on the wire (grpc status details /
   typed error metadata) consumed by the M17 client's `throwFromStatus` —
   tracked as post-M37 follow-up; the M17 client file is owned by agents
   EA/DA, which is why the sniff lives at the DLC boundary.

## Deferred synthesized callbacks (M37 Agent EC-4)

DLC-synthesized callbacks (§8.3/§8.6 time acks, §7.18 ownership-query
answers, §4.9 federation-execution reports) enqueue on
`DLCRTIambassadorImpl` and drain at the top of `evokeCallback` /
`evokeMultipleCallbacks`, before M17 wire events — matching Pitch's
deliver-after-return ordering. Bridge-delivered (real wire) callbacks are
not queued. gorti's DLC layer has no HLA_IMMEDIATE pump thread; callbacks
only reach the federate via `evoke*`.

## Attribute scope advisory switches (M37 Agent EC-2)

`enable/disableAttributeScopeAdvisorySwitch` are accept-and-record only:
gorti's server emits `AttributesInScope`/`AttributesOutOfScope`
unconditionally whenever DDM region-overlap membership changes
(`rti/internal/object/update.go emitScopeAdvisories`); there is no
per-federate gate on the wire.
