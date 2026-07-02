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

## gorti parity status (M36, agent-DC)

Carrier **FULL 4/4**; querier **PARTIAL 7/10 strict, 9/10 by content —
every golden event now has a counterpart** (M35: events genuinely
missing). Captured run: `gorti-captured.{carrier,querier}.log`
(canonicalized), against the M36-DC rtid.

M36 closed two of the three M35 gaps:

1. DC-3 — ownership seeding narrowed to PUBLISHED attributes.
   `rti/internal/object/registry.go` no longer seeds the registrant as
   owner of the blanket [1..8] probe range; only attributes the
   registrant publishes (plus the implicit privilege attribute) are
   seeded. `QUERIER: ATTRIBUTE_IS_NOT_OWNED attr=UnownedAttr` now
   fires as the golden expects (§7.18).
2. DC-4 — implicit HLAprivilegeToDeleteObject resolves.
   `rti/cmd/rtid/foms.go` LookupAttribute walks the inheritance chain
   (MIM declares the attribute on HLAobjectRoot) and aliases the
   fixture's HLA 1.3-lineage spelling "HLAprivilegeToDelete".
   `getAttributeHandle` succeeds and the third
   `QUERIER: QUERY_OWNERSHIP attr=HLAprivilegeToDelete` line appears.

Remaining divergences (exactly two kinds):

- ORDERING (3 line swaps): each answer callback prints BEFORE the
  fixture's own `QUERY_OWNERSHIP` line. The DLC emulates the void
  §7.17 call by invoking the M17 RPC synchronously and firing the
  §7.18 callback inside `queryAttributeOwnership`
  (cppsdk/src/dlc/RTIambassadorImpl.cpp:1036-1049) — the callback
  therefore precedes the caller's own print. Client-side artifact,
  not addressable from the server; DA-owned if the callback is to be
  deferred to the next evoke.
- CONTENT (1 line): golden expects
  `ATTRIBUTE_IS_OWNED_BY_RTI attr=HLAprivilegeToDelete`; gorti answers
  `INFORM_OWNERSHIP attr=HLAprivilegeToDelete owner=<H>` (the
  registrant). GOLDEN-REVIEW FLAG: per IEEE 1516.1-2010 §6.8 the
  registering federate "shall own the instance attribute
  HLAprivilegeToDeleteObject" of the instance it registers —
  attributeIsOwnedByRTI applies to RTI-owned (MOM) instance
  attributes. The spec-derived golden's OWNED_BY_RTI expectation looks
  incorrect for a federate-registered instance; gorti's answer follows
  §6.8. Additionally the DLC cannot synthesize attributeIsOwnedByRTI
  from the M17 QueryOwnership result shape (owned + concrete owner
  handle). Left for orchestrator adjudication rather than editing the
  golden here.
