# IDD Historical Summary: v1 MVP

Status: Historical summary; non-normative

Reconstructed: 2026-07-17 from repository history and surviving specification
content; not a release date

Context: Summarizes the documented v1 MVP stage

Authority: The [current specifications](../../current/) define the formal
engineering baseline.

The reconstructed v1 interface record describes protobuf/gRPC lifecycle,
declaration, object, basic time, and callback services; Go internal manager
interfaces; a Python asynchronous connection API; FOM XML input; and common HLA
encoding vectors. Callback delivery used one stream per joined federate. Public
handles were numeric and scoped to the joined federation.

Later confirmed streams, C++ interfaces, advanced time calls, and extended
service groups were not part of the v1 stage.
