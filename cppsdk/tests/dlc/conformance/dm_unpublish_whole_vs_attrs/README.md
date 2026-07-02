# dm_unpublish_whole_vs_attrs

Single federate exercises both unpublish forms and proves they produce
distinct manager-side state:

- **Phase 1:** publish `[Position, Velocity]`, then call
  `unpublishObjectClassAttributes(class, {Position})` (§5.7 — subset
  form). Velocity remains published → `registerObjectInstance` still
  succeeds.
- **Phase 2:** re-publish `[Position, Velocity]`, then call
  `unpublishObjectClass(class)` (§5.3 — whole-class form). All
  attributes drop → `registerObjectInstance` fails with
  `ObjectClassNotPublished`.

Locks divergence catalogue row 11.10 MAJOR: gorti M17 only has the
attribute-subset form (`unpublishObjectClassAttributes`); the whole-
class form (`unpublishObjectClass`) is added per the DLC surface.

M31 dispatch plan §2.2 fixture #7.

## Spec citations per event in goldens

Per TASK-362 traceability lint:

- `FED: CONNECT` — §4.2 connect
- `FED: JOIN` — §4.9 joinFederationExecution
- `FED: PUBLISH` — §5.3 publishObjectClassAttributes
- `FED: UNPUBLISH_ATTRS` — §5.7 unpublishObjectClassAttributes (subset form)
- `FED: REGISTER` — §6.8 registerObjectInstance
- `FED: UNPUBLISH_CLASS` — §5.3 unpublishObjectClass (whole-class form — catalogue row 11.10)
- `FED: REGISTER_FAILED` — §6.8 ObjectClassNotPublished exception path
- `FED: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. Goldens are `TBD-pitch-capture` until Agent E TASK-363 clears.

## gorti parity status (M36, agent-DC)

PARTIAL 7/9 strict vs spec-derived golden. Captured run:
`gorti-captured.federate.log`, against the M36-DC rtid.

**The M35 server-side gap is CLOSED.** Root cause was NOT a missing
register check (rti/internal/object registry.go already rejected
per §6.8) but that the whole-class unpublish never took effect:
the DLC delegates `unpublishObjectClass` as
`unpublishObjectClassAttributes(class, {})` and
`rti/internal/declaration/manager.go` treated the empty attribute set
as a no-op. M36 DC-2: an empty set now drops ALL of the federate's
attribute publications for the class (§5.3 semantics), after which the
phase-2 `registerObjectInstance` is correctly rejected — the wire
carries `FAILED_PRECONDITION` with the documented sentinel
`object class not published by federate`
(core.ErrObjectClassNotPublished, idd.md §1.1.4). Phase 1 (subset
unpublish; register still legal because Velocity remains published)
matches the golden as before.

Remaining divergence is CLIENT-side exception mapping (cppsdk —
outside DC ownership, flagged for agent DA):
`cppsdk/src/RtiAmbassador.cpp` throwFromStatus has no "not published"
detail-sniff, so the generic `FAILED_PRECONDITION` arm throws
`FederateNotExecutionMember`; the fixture's
`catch (ObjectClassNotPublished)` misses, the outer handler prints
`FED: ERROR FederateNotExecutionMember: registerObjectInstance: object
class not published by federate` on stderr and exits before RESIGN.
Hence the strict score temporarily drops 8/9 → 7/9 (REGISTER_FAILED
and RESIGN lines missing) even though the RTI behavior is now
spec-correct — the old 8/9 counted the WRONG `FED: REGISTER ...
car-phase2` success line as a near-miss.

Missing impl (residual, DA): one detail-sniff line in throwFromStatus
(`"not published"` → ObjectClassNotPublished) plus the equivalent
propagation through M17Bridge guard()/translateBridgeError so the DLC
surfaces the §6.8 exception type.
