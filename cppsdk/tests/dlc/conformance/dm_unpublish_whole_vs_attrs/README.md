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
