# DLC implementation notes

## IEEE exception metadata

When an RPC failure maps unambiguously to an IEEE 1516.1-2010 Annex C
exception, the server writes the class name to trailing gRPC metadata. C++ and
Python SDKs use this metadata instead of parsing the status message.

The metadata contract is:

- **Key:** `rti-spec-exception`
- **Location:** trailing metadata on unary and streaming RPCs
- **Value:** the UpperCamelCase class name from `cppsdk/include/RTI/Exception.h`
- **Missing key:** no unambiguous Annex C mapping is available

Examples include `ObjectClassNotPublished`, `AttributeNotOwned`,
`InvalidLogicalTime`, and `FederateNotExecutionMember`. The gRPC status code
and message do not change when the trailer is present.

Server mappings live in
`rti/internal/transport/grpc/spec_exception.go` and are attached by
`errToStatus`. Add new mappings there rather than extending client-side string
parsers.

### C++ translation path

`throwFromStatus` in `src/RtiAmbassador.cpp` reads the trailer after the RPC
finishes. A known class is thrown directly; other Annex C names travel through
the `m17::SpecException` carrier. `M17Bridge.cpp` prefixes the bridge error
with that name, and `translateBridgeError` in
`src/dlc/BridgeErrorTranslation.{h,cpp}` maps it to one of the 121 exception
types in `RTI/Exception.h`.

Third-party clients should treat an absent or unknown value as an internal
error unless the operation supplies a more specific fallback.

## Legacy message fallback

`translateBridgeError` still recognizes a small set of server status strings
for compatibility with servers that do not send the metadata trailer:

| Server message substring | DLC exception |
|---|---|
| `interaction class not published` | `InteractionClassNotPublished` |
| `not published` | `ObjectClassNotPublished` |
| `lookahead must be non-negative` | `InvalidLookahead` |
| `requested time is not greater than current logical time` | `LogicalTimeAlreadyPassed` |
| `invalid logical time` or another `lookahead` message | `InvalidLogicalTime` |

This fallback depends on the strings in `rti/internal/core/errors.go` and may
degrade to `RTIinternalError` when those strings change. New exception support
belongs in the metadata mapping, not this table.

## Synthesized callbacks

Callbacks synthesized by the DLC layer, including time-management
acknowledgements, ownership-query answers, and federation-execution reports,
are queued on `DLCRTIambassadorImpl`. They are drained at the start of
`evokeCallback` or `evokeMultipleCallbacks`, before callbacks delivered by the
bridge. This preserves delivery after the initiating service call returns.

The DLC layer has no immediate callback pump thread. Federates receive
callbacks only through the `evoke*` methods.

## Attribute scope advisory switches

`enableAttributeScopeAdvisorySwitch` and
`disableAttributeScopeAdvisorySwitch` record the requested state but do not
gate callbacks. The server emits `AttributesInScope` and
`AttributesOutOfScope` whenever DDM region overlap changes.
