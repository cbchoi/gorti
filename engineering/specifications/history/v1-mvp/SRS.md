# SRS Historical Summary: v1 MVP

Status: Historical summary; non-normative

Reconstructed: 2026-07-17 from repository history and surviving specification
content; not a release date

Context: Summarizes the documented v1 MVP stage

Authority: The [current specifications](../../current/) define the formal
engineering baseline.

The reconstructed record describes requirements for a single-node Go RTI that
could create a federation, join and resign federates, publish and subscribe,
register and discover objects, exchange receive-order attributes and
interactions, and advance logical time through a basic next-event request. It
required deterministic FOM handles, HLA basic-data encoding, event logging, and
a Python federate API with a DEVS bridge.

The documented stage did not require complete Time Management, ownership
negotiation, DDM, save/restore, MOM, a C++ SDK, production authentication, or
clustered deployment. Acceptance centered on a deterministic end-to-end
federation, cross-language encoding vectors, and replay equality.
