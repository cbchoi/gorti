# SDD Historical Summary: v1 MVP

Status: Historical summary; non-normative

Reconstructed: 2026-07-17 from repository history and surviving specification
content; not a release date

Context: Summarizes the documented v1 MVP stage

Authority: The [current specifications](../../current/) define the formal
engineering baseline.

The reconstructed v1 architecture record describes a standalone gRPC server,
federation and object service managers, a FOM/encoding library, an append-only
event log, and a Python SDK. State was scoped to one server process and keyed by
federation. Service managers used deterministic handle allocation and
per-federation serialization. Time Management implemented a minimal
logical-time state and next-event grant calculation.

This stage established the lasting architectural rule that SDKs are clients
and the server owns authoritative federation state.
