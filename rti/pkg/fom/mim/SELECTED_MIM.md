# Explicit MIM Selection

This is a bounded product profile, not a claim of full IEEE 1516-2010
conformance or a decision about every normatively permitted alternate MIM.
Authorized normative fulltexts were unavailable for this implementation batch.

An explicit selection supplies a nonempty designator and XML bytes. The server
does not resolve the designator as a local path or URL. Empty XML is invalid;
omission remains the historical embedded-MIM behavior. The Go SDK can read an
explicitly authorized local file with `federate.LoadMIMModule`.

`ParseSelected` requires the embedded MIM's ordered element structure,
namespaces, attributes, service/class/member names, datatypes, encodings,
dimensions, transportation declarations and other non-description leaf values.
XML comments and inter-element whitespace may differ. Existing `semantics`
element text may differ and is retained, including empty text and whitespace.
Unknown, duplicate, missing or incompatible content is rejected rather than
discarded or replaced with the embedded source. This structural comparison is
not a general XSD validator or a general-purpose alternative-MIM parser.

The selected bytes and supplied designator remain separate from FOM modules in
the authoritative view and JSON repository snapshot. Explicit source identity
uses domain/role framing and byte lengths. Omitting selection preserves the
historical FOM-only digest. Diagnostic designators do not affect the content
digest, but join expectations compare both the exact designator and XML bytes.

Wire V2 is narrowly reserved for create with `mim_module`, or join with
`expected_mim_module`. Omitted selection/expectation and every other RPC use V1.
Legacy servers reject V2 before mutation. Updated servers reject mismatched
profiles. Join compares its expectation under exclusive lifecycle/operation
admission before preparing additional FOM modules or allocating membership.
There is no retry that silently downgrades explicit selection to V1.

The repository's MOM designator provider returns the exact explicit designator;
omission retains `HLAstandardMIM`. Current FDD exports the selected parsed model,
not an embedded replacement. Lossless export can still fail when the complete
MIM/FOM composition lacks required retained metadata, such as switches absent
from the embedded MIM alone. No missing values are fabricated for export.
