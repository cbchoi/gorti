# Pending Local Notifications v1

Optional private GRS1 state field `pending_local_notifications` is an array under `callback_recovery:"server-v2"`. Omit it when unused. Empty arrays are valid; null is not. It has no server ledger, receipt, Entry/Outcome or activation authority.

```json
{"local_id":"abababababababababababababababababababababababababababababababab","order":1,"scope":"member","kind":"informAttributeOwnership","payload":{"object_handle":10,"attribute_handle":20,"owner_federate":30064771080}}
```

Every record has exactly `local_id`, `order`, `scope`, `kind`, `payload`. IDs are unique lowerhex64 local correlation identifiers; they are not server invocation identities. Order is a positive exact uint64 JSON number, strictly increasing in array order; gaps are allowed. Same-payload records with different IDs preserve multiplicity. Scope is `member` except the connection-scoped execution list. DTOs contain only immutable results of known completed RPCs and a still-uninvoked user notification.

## Exact Kind Payloads

All required handles below are exact uint64 JSON numbers, never strings or floats.

| Kind | Payload |
| --- | --- |
| `informAttributeOwnership` | `{object_handle, attribute_handle, owner_federate}` |
| `attributeIsNotOwned` | `{object_handle, attribute_handle}` |
| `attributeIsOwnedByRTI` | `{object_handle, attribute_handle}` |
| `confirmAttributeOwnershipAcquisitionCancellation` | `{object_handle, attribute_handles:[uint64]}` |
| `confirmAttributeTransportationTypeChange` | `{object_handle, attribute_handles:[uint64], transport_type:1\|2}` |
| `confirmInteractionTransportationTypeChange` | `{interaction_class_handle, transport_type:1\|2}` |
| `reportAttributeTransportationType` | `{object_handle, attribute_handle, transport_type:1\|2}` |
| `reportInteractionTransportationType` | `{federate_handle, interaction_class_handle, transport_type:1\|2}` |
| `federationSaveStatusResponse` | `{statuses:[{federate_handle,status:1..4}], optional state:1..4, optional active_label:string}` |
| `federationRestoreStatusResponse` | `{statuses:[{pre_restore_handle,post_restore_handle,status:1..6}], optional state:1..5, optional active_label:string}` |
| `reportFederationExecutions` | `{executions:[{name:string,logical_time_implementation_name:string,optional mode:0..2,optional federates_joined:uint32,optional federation_generation:uint64}]}`; scope=`connection` |

Numeric status/transport values are the existing wire enum values, not SDK-language enum ordinals. Optional fields are omitted or non-null. Empty strings for active_label are valid. Execution names and factory names must be nonempty. Attribute sets contain unique nonzero values; empty sets are allowed. Status vectors contain unique live/pre handle keys. Arrays cannot be null. Duplicate JSON keys, unknown/case-variant fields, capabilities and arbitrary payloads fail closed.

## Reference Policy

Only `owner_federate`, interaction-report `federate_handle`, save-status `federate_handle`, and restore-status `pre_restore_handle` are generation-bound references. They must be nonzero/source-generation scoped and contribute to the complete SDK graph census before target allocation. Retired source-generation identities are allowed in that census; their inclusion does not claim current membership. Rebind requires the complete validated bijection, not a roster-only projection, and never guesses from names or arithmetic.

Restore-status `post_restore_handle` is a historical saved designator, preserved exactly across generations; zero means absent. Nonzero historical handles must have a nonzero low designator. They do not enter the target allocation census. Other non-federate handles, execution-list generation metadata, payload status/result values, IDs, multiplicity and local order are immutable. No source logical-time-family constraint is imposed on another federation's list metadata.

## Dispatch / Limits / Boundaries

SDKs retain local FIFO and retire records only at explicit user dispatch. Running handlers/unknown results fail export. They do not invoke another RPC, Entry, Outcome, activation, query or lifecycle transition to deliver these DTOs. Pending prepared transportation activation closures and arbitrary closures must fail export. Python direct-change confirmations qualify only after the successful direct RPC has completed; Java prepared closures do not qualify merely because a request was accepted.

There is no original mixed local/server total-order guarantee. Restore terminal precedes recovered local notifications; existing mandatory HLA stream ordering remains intact. Any normative requirement for a mixed dependency requires a separate explicit ordering contract rather than a false total-order claim.

Existing envelope limits apply: 1 MiB state, 8 MiB aggregate source state, nesting64 and collection8192. SDK readers must reject unsupported DTO kinds/fields during staging, not silently drop them. Go's existing return-only query APIs are unchanged and need not emit the optional field.

The central pre-allocation reader profile matches implemented SDK readers: Java accepts the full 11-kind grammar; Python accepts its seven execution-list, transportation and status kinds, not the four ownership/cancellation kinds. The current Go reader rejects any presence of this field, including an empty array, so Go writers and rebound images must omit it. No future capability is inferred from `server-v2` alone.
