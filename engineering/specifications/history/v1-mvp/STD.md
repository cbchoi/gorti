# STD Historical Summary: v1 MVP

Status: Historical summary; non-normative

Reconstructed: 2026-07-17 from repository history and surviving specification
content; not a release date

Context: Summarizes the documented v1 MVP stage

Authority: The [current specifications](../../current/) define the formal
engineering baseline.

Acceptance required unit tests for FOM parsing and encoding, integration tests
for federation and object services, deterministic ping-pong and timed examples,
identical replay output, and matching Go/Python encoding vectors. Negative
tests covered malformed FOM input, duplicate federation creation, invalid
membership, and a blocked time advance.
