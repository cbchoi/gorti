# own_query_via_callbacks

`queryAttributeOwnership(object, attr)` is VOID; result arrives via one of
three §7.18 callbacks.

The service returns void, and the result is asynchronous through one of
the three §7.18 callbacks: `informAttributeOwnership`,
`attributeIsNotOwned`, or `attributeIsOwnedByRTI`.

The querier issues three queries on a single `car-query` object, each
hitting a different §7.18 branch:

| Attribute | Owner state | §7.18 callback |
|---|---|---|
| `OwnedAttr` | Carrier publishes + registered owner | `informAttributeOwnership` |
| `UnownedAttr` | FOM declares it, nobody publishes | `attributeIsNotOwned` |
| `HLAprivilegeToDelete` | Implicit attribute; owned by the REGISTRANT per §6.8 | `informAttributeOwnership` (owner=carrier) |

## Spec citations per event in goldens

The traceability lint enforces these citations:

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

## Golden semantics

Reasoning, per IEEE 1516.1-2010:

1. §6.8 (registerObjectInstance), ownership postcondition: the
   registering joined federate "shall own the instance attribute
   HLAprivilegeToDeleteObject" of each object instance it registers
   (publication of any object class implicitly publishes
   HLAprivilegeToDeleteObject, so the registrant is eligible to own
   it). In this fixture the CARRIER registers `car-query`, so the
   carrier — a joined federate, not the RTI — owns the privilege
   attribute for the entire run (it never divests, and resigns only
   after the querier's queries complete).
2. §7.17 (queryAttributeOwnership) is void; the answer arrives as
   exactly one of the three §7.18 callbacks, selected by the actual
   owner: a joined federate → `informAttributeOwnership(attr, owner)`;
   nobody → `attributeIsNotOwned`; the RTI → `attributeIsOwnedByRTI`.
3. `attributeIsOwnedByRTI` therefore requires the RTI itself to be
   the owner. The RTI owns instance attributes of the MOM object
   instances it registers (§6.8 in conjunction with §10 MOM
   semantics); it does not own the privilege attribute of a
   federate-registered instance. No divestiture-to-RTI occurs in this
   scenario. Hence the OWNED_BY_RTI expectation had no spec-legal
   trigger here.
4. Consequence: the third query's correct answer is
   `informAttributeOwnership(HLAprivilegeToDeleteObject,
   owner=carrier)` — exactly what gorti emits
   (`INFORM_OWNERSHIP attr=HLAprivilegeToDelete owner=<H>`).

The golden therefore expects
`QUERIER: INFORM_OWNERSHIP attr=HLAprivilegeToDelete owner=<H>`.
The fixture witnesses two of the three §7.18 flavors; a genuine
`attributeIsOwnedByRTI` witness needs an RTI-owned (MOM) instance
attribute and belongs in a MOM-side fixture, not here.

## Status

**SPEC-FULL 14/14 strict** (carrier 4/4, querier 10/10;
`_harness/run_fixture.sh own_query_via_callbacks`). The DLC defers
synthesized ownership-query callbacks to the evoke queue, so
each informAttributeOwnership / attributeIsNotOwned lands after the
fixture's own QUERY print, matching the golden
(INFORM_OWNERSHIP owner=carrier for HLAprivilegeToDelete per §6.8).
No residual.
