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

## gorti parity status (M35, parity-CC)

PARTIAL 8/9 vs spec-derived golden. Captured run: `gorti-captured.federate.log`.

Divergent event (exactly one):

- golden: `FED: REGISTER_FAILED reason=ObjectClassNotPublished`
- gorti:  `FED: REGISTER class=Vehicle name=car-phase2 handle=<H>`

The DLC side is correct — `DLCRTIambassadorImpl::unpublishObjectClass`
delegates whole-class unpublish as
`m17_->unpublishObjectClassAttributes(class, {})` (empty set = whole
class). The gap is server-side: gorti's `registerObjectInstance` does
not enforce publication state, so it never raises
`ObjectClassNotPublished` (§6.8 exception set) after the whole-class
unpublish. Phase 1 (subset unpublish; register still legal because
Velocity remains published) matches the golden.

Missing impl: publication-state check in the gorti register path
(rti/internal/object registry) raising ObjectClassNotPublished.
