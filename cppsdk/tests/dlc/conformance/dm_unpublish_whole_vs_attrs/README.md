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

The fixture requires both DLC service forms:
`unpublishObjectClassAttributes` removes a selected subset, while
`unpublishObjectClass` removes the complete class publication.

## Spec citations per event in goldens

The traceability lint enforces these citations:

- `FED: CONNECT` — §4.2 connect
- `FED: JOIN` — §4.9 joinFederationExecution
- `FED: PUBLISH` — §5.3 publishObjectClassAttributes
- `FED: UNPUBLISH_ATTRS` — §5.7 unpublishObjectClassAttributes (subset form)
- `FED: REGISTER` — §6.8 registerObjectInstance
- `FED: UNPUBLISH_CLASS` — §5.3 unpublishObjectClass (whole-class form)
- `FED: REGISTER_FAILED` — §6.8 ObjectClassNotPublished exception path
- `FED: RESIGN` — §4.10 resignFederationExecution

## Status

**SPEC-FULL 9/9** (`_harness/run_fixture.sh dm_unpublish_whole_vs_attrs`).
An empty attribute set drops all of the federate's publications for the
class (§5.3), while subset unpublish leaves other attributes published.
`translateBridgeError` maps the server's "not published" detail to
`ObjectClassNotPublished`, so phase-2 register records REGISTER_FAILED and
the fixture reaches RESIGN.
No residual.
