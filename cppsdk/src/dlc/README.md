# gorti DLC layer — implementation notes

## Spec-exception channel (M39 Agent HB) — PRIMARY

Every gorti RPC failure whose cause has an unambiguous IEEE 1516.1-2010
Annex C identity carries the exception class name as **trailing gRPC
metadata**. This is the machine-readable contract for ALL SDKs (cppsdk,
pysdk, and third-party clients) — string sniffing is legacy fallback only
(next section).

### The contract (for third-party SDK authors)

* **Key:** `rti-spec-exception` (gRPC *trailing* metadata; populated on
  unary and streaming RPCs alike — read it after the call finishes, e.g.
  `ClientContext::GetServerTrailingMetadata()` in grpc++, or the
  `trailing_metadata()` of the failed call in grpc-python).
* **Value:** the Annex C exception class name, UpperCamelCase exactly as
  enumerated in `cppsdk/include/RTI/Exception.h` — e.g.
  `ObjectClassNotPublished`, `AttributeNotOwned`, `InvalidLogicalTime`,
  `FederateNotExecutionMember`, `FederationExecutionAlreadyExists`,
  `SynchronizationPointLabelNotAnnounced`.
* **Absent key = no unambiguous identity.** Either an internal error, or
  a server error whose spec exception depends on which service was
  invoked. Clients MUST fall back to whatever legacy behavior they had
  (for gorti's own SDKs: the deprecated sniffs below; the safe floor is
  `RTIinternalError`).
* The gRPC status code / message are unchanged by this channel — the
  trailer is additive, so pre-M39 clients are unaffected.

Server-side source of truth: the sentinel→name table in
`rti/internal/transport/grpc/spec_exception.go`, attached at the
`errToStatus` choke point every handler error flows through. New
exception coverage belongs THERE, not in client-side sniffs.

### cppsdk pipeline

`throwFromStatus` (M17 client, `src/RtiAmbassador.cpp`) reads the trailer
FIRST: a name with a matching m17 typed class throws that class; any
other name rides the `m17::SpecException` carrier. `M17Bridge.cpp
guard()` re-emits the name as the `"<AnnexCName>: ..."` message prefix,
and `translateBridgeError()` (`src/dlc/BridgeErrorTranslation.{h,cpp}`,
unit-tested in `tests/dlc/conformance/_runtime/`) matches the prefix
against the **full 121-class Annex C X-list** and throws the precise
`<RTI/Exception.h>` type.

## Error-string sniffing (M37 Agent EC-3) — DEPRECATED legacy fallback

`translateBridgeError()` maps M17 bridge errors to the spec
`<RTI/Exception.h>` types in these passes (in order):

1. **Annex C prefix match** on the class name that `M17Bridge.cpp
   guard()` prepends (`"InvalidLogicalTime: ..."` etc.) — the primary
   channel above lands here; stable, contract-level.
2. **Detail-string sniff** on the server-produced gRPC status message,
   for rejections a pre-M39 M17 client folds into `RTIinternalError`.
   DEPRECATED: only exercised when the server did not send the
   `rti-spec-exception` trailer (pre-M39 rtid, third-party RTIs). Pinned
   to gorti's exact error strings in `rti/internal/core/errors.go`:

   | server string (substring matched)                                  | DLC exception                  |
   |--------------------------------------------------------------------|--------------------------------|
   | `interaction class not published`                                   | `InteractionClassNotPublished` |
   | `not published` (any other)                                         | `ObjectClassNotPublished`      |
   | `lookahead must be non-negative`                                    | `InvalidLookahead`             |
   | `requested time is not greater than current logical time`           | `LogicalTimeAlreadyPassed`     |
   | `invalid logical time` or `lookahead` (any other)                   | `InvalidLogicalTime`           |

   If those Go error strings change, this legacy pass silently degrades
   back to `RTIinternalError` (nothing crashes) — acceptable, because
   M39+ servers never rely on it. Do NOT extend this table; add the
   mapping to `spec_exception.go` instead.

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
