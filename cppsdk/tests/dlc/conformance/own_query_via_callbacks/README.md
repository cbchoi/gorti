# own_query_via_callbacks

`queryAttributeOwnership(object, attr)` is VOID; result arrives via one of
three §7.18 callbacks. M31 dispatch plan §2.2 fixture #18.

Enforces divergence catalogue:
- Row 12.6 BLOCKING — `queryAttributeOwnership` returns void per spec;
  gorti M17 returned an `OwnershipQueryResult` struct synchronously.
- Row 4.32 MAJOR — the three §7.18 callbacks (`informAttributeOwnership`,
  `attributeIsNotOwned`, `attributeIsOwnedByRTI`) are absent from gorti M17.

The querier issues three queries on a single `car-query` object, each
hitting a different §7.18 branch:

| Attribute | Owner state | §7.18 callback |
|---|---|---|
| `OwnedAttr` | Carrier publishes + registered owner | `informAttributeOwnership` |
| `UnownedAttr` | FOM declares it, nobody publishes | `attributeIsNotOwned` |
| `HLAprivilegeToDelete` | RTI-managed implicit attribute | `attributeIsOwnedByRTI` |

## Spec citations per event in goldens

Per TASK-362 traceability lint:

### Carrier

- `CARRIER: CONNECT` — §4.2 connect
- `CARRIER: JOIN` — §4.9 joinFederationExecution
- `CARRIER: REGISTER` — §6.8 registerObjectInstance
- `CARRIER: RESIGN` — §4.10 resignFederationExecution

### Querier

- `QUERIER: CONNECT` — §4.2 connect
- `QUERIER: JOIN` — §4.9 joinFederationExecution
- `QUERIER: DISCOVER` — §6.9 discoverObjectInstance
- `QUERIER: QUERY_OWNERSHIP` — §7.17 queryAttributeOwnership
- `QUERIER: INFORM_OWNERSHIP` — §7.18 informAttributeOwnership
- `QUERIER: ATTRIBUTE_IS_NOT_OWNED` — §7.18 attributeIsNotOwned
- `QUERIER: ATTRIBUTE_IS_OWNED_BY_RTI` — §7.18 attributeIsOwnedByRTI
- `QUERIER: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. `TBD-pitch-capture` until Agent E TASK-363 clears.
