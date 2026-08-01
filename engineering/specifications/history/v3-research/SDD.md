# SDD Historical Summary: v3 Research and Reach

Status: Historical summary; non-normative

Reconstructed: 2026-07-17 from repository history and surviving specification
content; not a release date

Context: Summarizes the documented v3 Research and Reach stage

Authority: The [current specifications](../../current/) define the formal
engineering baseline.

The design added registries for selectable service implementations, C++ and
expanded Python adapters, structured manager snapshots, authentication hooks,
and richer operations telemetry. Alternative time or transport implementations
were composed behind existing interfaces and gated by deterministic comparison.

The single-node server remained authoritative. Experimental distributed and
DDS paths were isolated from the default runtime and could not weaken callback
ordering or failure visibility.
