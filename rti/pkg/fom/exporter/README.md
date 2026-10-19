# Retained-Model FDD Export

`Marshal(*model.FOM) ([]byte, error)` is a pure, deterministic serializer.
`composition.Repository.CurrentFDD(ctx, federation)` captures the currently
committed immutable handle and calls it. Neither reads files, concatenates
modules, sends data to federates, nor exposes prepared join/restore candidates.
MOM integration is owned separately by the application server.

## Retained Coverage

- Qualified object and interaction hierarchies, including repeated local names.
- Object/interaction sharing; local attributes and parameters remain local, so
  inherited declarations are not copied into descendants.
- Attribute types, update type, ownership, sharing, dimension references,
  transportation, and order; interaction dimensions, transportation, and order.
- Object/interaction/attribute/parameter semantics and attribute update
  conditions, including explicitly empty descriptions and preserved whitespace.
- All six modeled datatype kinds, including basic encoding information, simple
  units/resolution/accuracy, repeated enumerator values, array cardinality,
  ordered record fields and variant alternatives. Semantics are retained on
  simple, enumerated, array, fixed-record and variant-record types, record
  fields and variant alternatives; the XSD has no semantics element on basic
  representations or enumerators.
- Dimensions, optional datatype references, upper bounds, normalization text
  and default-range declarations (the XSD element is `value`). Both embedded
  MIM dimensions retain their datatype and `Excluded` default declarations.
- Exact decimal update rates and their empty/absent semantics, all ten boolean switches,
  and explicit automatic resign actions.
- Retained transportation names, supplied `Yes`/`No` reliability values and
  optional semantics, including unused declarations and empty/absent descriptions.
  Export remains limited to `HLAreliable` and `HLAbestEffort`; unknown identities
  fail explicitly. Declarations are never derived from transport references.

The accompanying fixes retain singular IEEE `<value>` and repeated values in
both parsers, object-class sharing, and all boolean switch declarations.
Legacy `<values>` remains accepted and is emitted as a singular value entry.
Exact rate text is canonicalized numerically, not rounded through float64.

## Metadata Contract

`model.Dimension.DataType` and `DefaultValue` are additive exported strings.
`NormalizationKey` retains its existing name but now preserves XML string
content, including surrounding whitespace. Empty strings mean absent for
these fields; explicitly empty XML declarations are invalid and rejected.
`DefaultValue` retains the schema vocabulary (`Excluded`, a digit sequence,
`[digits..digits)` or `[digits)`), without range arithmetic or invented defaults.
The parsers additionally retain the literal `NA` used by existing production
FOM inputs as a legacy compatibility value. The supplied official XSD does not
admit `NA`: export returns `ErrUnsupported` rather than omitting it, changing it
to `Excluded`, or claiming a schema-valid default. This does not change runtime
DDM range behavior or authorize arbitrary invalid default values.

New `Semantics` fields and `Attribute.UpdateCondition` use the comparable value
type `model.OptionalString{Value string, Present bool}`. Its zero value means
absent; `Present: true, Value: ""` means explicitly empty. XML string content is
not trimmed. All fields are exported for JSON or other declaration-based
snapshot DTOs, and existing class/dimension defensive copies retain them.
The datatype implementations retain their existing immutable-by-convention
pointer ownership contract.

`model.Transportation{Name, Reliable, Semantics}` is a comparable value;
`Semantics` uses `OptionalString`. `FOM.Transportations()` returns a defensive
copy, and `WithTransportations()` copies and sorts its input. Constructors that
rebuild an existing model must carry this metadata explicitly. Reliability is
retained as supplied, not inferred from a name or used to select a runtime
implementation. Unknown names can be retained as metadata by the parser but
remain unsupported by this exporter. Retention adds no transport support or
runtime reliability guarantee.

`UpdateRate.Semantics` remains a string for source and legacy-JSON compatibility.
The additive `SemanticsPresent` boolean records an explicit empty element;
`SemanticsDeclaration()` also treats any legacy nonempty string as present.
`NewUpdateRate(name, decimal, "")` retains its absent-metadata meaning; callers
set `SemanticsPresent` to true to describe an explicitly empty declaration.
JSON retains this flag and adds `ExactRate`, an exact decimal string, when the
source decimal is available. Fresh decoding checks its agreement with the
legacy numeric `Rate` and rejects invalid or inconsistent exact metadata without
partially assigning the receiver. Old JSON without `ExactRate` remains accepted;
previously discarded decimal precision and empty-element presence cannot be
recovered from those snapshots.

Repeated dimension definitions must agree on all modeled fields. Repeated
class containers fill absent semantics and reject differing supplied semantics,
including empty versus nonempty text; MIM pass-through declarations follow the
same descriptive rule. Repeated attributes/parameters retain the existing
exact-definition check, so absent versus explicitly empty metadata conflicts.
Datatype conflicts, now including these descriptive fields, are rejected by
export and the application's live-definition validation; the parser itself is
not a complete datatype-merge or schema validator. These are conservative
engineering compatibility rules, not a full normative modular-FOM merge policy.
Repeated transportation declarations require equal supplied reliability, and
repeated named update rates require equal maximum rates. IEEE 1516.1-2010
4.1.4.1 limits RTI combination to FDD; descriptive differences alone do not
reject either merge. Both retain the first supplied description, including
explicit empty text, and fill an absent description from a later declaration.
This retention choice is an implementation policy, not a prescribed normative
winner. It does not change the broader authoring-tool merge obligations.

## Unsupported Inputs

Errors wrap `ErrUnsupported` and return no partial document:

- Nil/unavailable models, missing HLA roots, duplicate, orphaned, ambiguous,
  inconsistent or cyclic class identities.
- Any absent required boolean switch. A FOM containing only four advisory
  switches is insufficient. The embedded standard MIM has no switches section;
  it cannot supply the other six. Absent values are never assumed false.
- Attributes/interactions with absent or unknown transportation/order metadata.
  Runtime fallback behavior is not used to fabricate FDD declarations.
- Referenced transport identities without retained declarations, even for the
  two known names; invalid reliability values and duplicate declarations.
- Unknown or duplicate switches, invalid resign actions, conflicting datatype
  definitions, unknown type/dimension references, duplicate declarations and
  unsupported enum/attribute metadata values.
- Nonfinite/mutated update rates, invalid XML text/identifiers, and conflicting
  legacy/singular enumerator values.
- Retained legacy dimension `NA` defaults, which are accepted for parser
  compatibility but are outside the supplied official FDD XSD vocabulary.

## Fidelity Boundary

This is **not** a lossless source-document exporter or a full conformance claim.
The current model does not retain model identification, time declarations,
service utilization, tags, synchronizations, notes, foreign extensions,
common XML attributes such as notes/idtag, or all absent-versus-empty distinctions
in older string-valued fields (including basic representation interpretation/
encoding) and optional section containers.
The exporter cannot detect or reconstruct metadata already discarded upstream.
Retaining dimension defaults does not implement or verify runtime DDM use of
those defaults. Closing these remaining gaps and reviewing applicability are
still necessary before claiming R1 complete or complete FDD export. Required
missing fields that *are* represented are rejected above.

Export is intentionally stricter than the earlier reference-derived behavior.
The XSD's `transportationRef` keyref requires a declaration for every referenced
identity: replacing missing declarations with an empty container is not valid,
and constructing `Yes`/`No` declarations from names fabricates metadata.
Reference-only source modules remain parser/runtime-compatible; export needs
the actual selected MIM declarations to have been merged into the retained
model. The original XML fixtures remain unchanged. Successful legacy descriptive
export tests now explicitly supply decoded standard-MIM declarations, and
model-reconstruction tests carry their original declarations. A separate
regression requires `ErrUnsupported` for the unaugmented reference-only model.
This changes the export fidelity contract, not general parser or service support.

## Local Verification

Focused tests cover exact retention, semantic roundtrip, independent XML
decoding, deterministic concurrent export, and committed create/additive join/
restore behavior. The external oracle uses the locally supplied official
`IEEE1516-FDD-2010.xsd`; it never downloads a schema or a licensed fulltext.

Set `GORTI_FDD_XSD` to that XSD and `GORTI_FDD_PYTHON` to a Python environment
with `xmlschema==4.3.2`, `elementpath==5.1.4`, and `lxml==6.1.3`, then run:

```text
go test ./rti/pkg/fom/model ./rti/pkg/fom/parser ./rti/pkg/fom/mim ./rti/pkg/fom/exporter ./rti/internal/application/composition
```

The independent oracle validates standalone and MIM-merged output, inspects
semantic values with XPath, and rejects four deliberately corrupted variants.
It skips explicitly if its environment is not configured. `xmlschema` is used
for XSD validation because libxml2's decimal implementation rejects the valid
28-place exact-decimal test value; lxml remains the independent XPath reader.
`GORTI_FDD_OUTPUT_DIR`, when set to an existing directory outside the repository,
also retains the generated test documents for inspection.

`TestDescriptiveMetadataOfficialSchemaOracle` additionally checks standalone
and MIM-merged `descriptive.xml` exports against the same local official XSD.
Independent XPath vectors cover every new field category and empty/absent
descriptions; five semantic mutations detect loss that can remain schema-valid,
and four schema mutations check vocabulary, default values, normalization and
element order. `TestDescriptiveMetadataRoundTripAndSnapshot` checks semantic
round-trip, fresh JSON decoding of exported declaration fields, model-only
reconstruction and MIM merge. These package tests do not replace production
dynamic-join/savebundle/restore integration tests.

`TestTransportRateOfficialSchemaOracle` validates independently authored
`transport_rates.xml`, its export, and a fresh JSON/model reconstruction. Exact
XPath vectors check unused declarations, supplied reliability, descriptive
whitespace, absent/empty semantics and decimal precision. Ten schema-valid
semantic mutations and four invalid-schema mutations test oracle sensitivity,
including missing referenced declarations. Raw evidence stays outside the
repository. These bounded results do not close R1 or establish full conformance.
The three supplied 2010 redlines are now reviewed in
[the source-based assessment](../../../../engineering/verification/ieee1516-2010-normative-review-20260921.md).
The [Python-first behavioral correction](../../../../engineering/verification/ieee1516-2010-python-behavior-20260921.md)
adds exact positivity checks at checked model, merge, parser and export boundaries.
Zero and negative maximum rates are rejected as required by OMT 4.11.2 and
6.2.11, even when the XML passes the schema. The independent oracle now includes
three schema-valid nonpositive mutations. Runtime float64 range limits remain
separate from exact-decimal positivity and equality.
