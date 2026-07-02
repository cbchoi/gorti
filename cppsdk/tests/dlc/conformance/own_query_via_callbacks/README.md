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

## gorti parity status (M35, parity-CD)

Verdict: **PARTIAL 11/14** (carrier FULL 4/4, querier 7/10; goldens
are spec-derived, Pitch capture pending). Captured run:
`gorti-captured.{querier,carrier}.log`.

Missing events (three):
- `QUERIER: QUERY_OWNERSHIP attr=HLAprivilegeToDelete`
- `QUERIER: ATTRIBUTE_IS_NOT_OWNED attr=UnownedAttr`
- `QUERIER: ATTRIBUTE_IS_OWNED_BY_RTI attr=HLAprivilegeToDelete`

Three distinct root causes:

1. **Implicit privilege attribute absent.** gorti's FOM repo only
   knows FOM-declared attributes; the spec's implicit HLAobjectRoot
   privilege attribute is never synthesized, so
   `getAttributeHandle(HLAprivilegeToDelete)` throws RTIinternalError
   ("attribute not found"). The querier now tolerates this (skips the
   third query) instead of aborting; both §7.17 query-3 lines are
   therefore unreachable, and §7.18 `attributeIsOwnedByRTI` is never
   exercisable against gorti at all (the DLC synthesis in
   RTIambassadorImpl.cpp maps query answers only to
   informAttributeOwnership / attributeIsNotOwned).

2. **Blanket initial-ownership seeding.** `UnownedAttr` (declared in
   the FOM, published by nobody) answers `informAttributeOwnership`
   with the carrier as owner instead of `attributeIsNotOwned`. Cause:
   on registration, rtid's OnRegister hook
   (rti/cmd/rtid/main.go → ownership.RegisterInitialOwnership) seeds
   the registrant as owner of the hardcoded cut-1 probe range
   `fanoutAttrProbe = [1..8]` (rti/internal/object/registry.go), not
   the registrant's *published* attribute set. Per §7.17/§6.8 the
   registering federate owns only published attributes (plus the
   privilege attribute); gorti over-claims ownership of every
   low-numbered attribute handle.

3. **Synchronous callback synthesis (ordering).** The two answers
   that do arrive are correct in content for OwnedAttr
   (informAttributeOwnership, owner=carrier) but are delivered
   synchronously *inside* the §7.17 call (parity-CA synthesis on
   fed_amb_), so each answer precedes the fixture's own
   QUERY_OWNERSHIP line; the golden models async delivery (all three
   queries, then all three answers). Content-presence matching still
   counts these lines; the interleave is a documented
   delivery-mechanics divergence (arguably spec-legal under
   HLA_IMMEDIATE, but it will never byte-match an async-RTI capture).

Missing impl: (1) implicit MIM/privilege attribute synthesis in the
FOM repo + an RTI-owned answer path (query owner==RTI →
attributeIsOwnedByRTI), (2) publication-scoped initial ownership
seeding (replace fanoutAttrProbe blanket with the registrant's
published set), (3) optional: queue §7.18 answers through the
callback queue instead of invoking fed_amb_ inline.
