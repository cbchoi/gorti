# IEEE 1516-2010 Requirement Review

These files decompose reviewed source statements and record implementation,
test oracles and remaining facets. They are not a complete normative inventory
or a conformance certificate. The original twenty-row inventory remains a
historical seed; it is not overwritten by this review.

The assessed product is the RTI with its selected standard Java mapping and
the Go/Python product interfaces. `applicability` refers to this assessment,
not to whether a statement is normative for an arbitrary federate or model.
Federate application memory, simulation logic and author-supplied SOM/FOM
documentation are not RTI implementations. Required model admission,
ownership enforcement and RTI callback/restore behavior remain in scope.

| File | Scope |
|---|---|
| `framework.json` | Framework responsibility and ownership rules |
| `fm.json` | Federation, synchronization, save and restore |
| `declaration-object.json` | Declaration and object services |
| `ownership-ddm.json` | Ownership and data distribution |
| `time-mapping.json` | Time services, representations and arithmetic |
| `support-mom.json` | Support, MOM and conformance-claim scope |
| `java-annex.json` | Selected Java API, codecs and normative resources |
| `omt.json` | Relevant model structure, FDD and merging rules |

## Record Meaning

- `source-reviewed`: the stated source fragment was inspected; it does not
  mean every requirement in the containing clause was found or satisfied.
- `reviewed_units`: direct clause bodies with a declared decomposition.
  Descendants, referenced contracts, tables and state charts remain separate.
- `tests[].oracle`: the inspected assertion and its boundary, not just a
  suggestive test name. A stub or unit test is not labelled as a live federation.
- `open_items`: unlinked conditions, exceptions, ambiguity or missing evidence.
  These notes are not counts of missing features or reproduced defects.
- `coverage_complete: false`: retained until source and semantic review close.

The reconciliation tool checks source hashes, consecutive extracted pages,
safe paths, literal test declarations and local execution records. Its census
includes candidate headings, tables, figures and normative-annex boundaries.
It does not interpret every obligation or promote tests to conformance.

The public result preserves actual executed invocation identities. A status of
`passing-records-only-completeness-unassessed` means the matching imported
invocations passed. It does not prove every parameter variant was selected,
the whole method was executed, the source was unchanged since that campaign,
or the full requirement was covered. Skips and failures are not credited.

## Reconciliation

The [2026-10-04 implementation and linkage increment](../ieee1516-2010-applicable-fixes-20261004.md)
connects all 639 currently reviewed applicable records without changing their
applicability. Some links are diagnostics or conditional architecture/claim
checks; they are identified in the report and are not passing conformance
oracles. The earlier unlinked backlog below is a historical checkpoint.

The [2026-10-03 applicable-requirement increment](../ieee1516-2010-applicable-requirements-20261003.md)
adds assertion-level mappings and new boundary tests. Its
[machine-readable backlog](../ieee1516-2010-applicable-requirements-20261003.json)
lists remaining unlinked applicable IDs and retained failing links. A linked
requirement is not a completed requirement; the increment remains incomplete.

Use Python 3.11 or newer. Supplied PDFs, extracted page records and raw execution
artifacts stay outside Git. Paths below are caller-owned local inputs:

```text
python scripts/ieee2010_requirements_review.py
  --sources <private-source-directory>
  --pdf-dir <supplied-pdf-directory>
  --go-results <go-test-jsonl>
  --python-results <pytest-junit-xml>
  --java-results <fresh-surefire-report-directory>
  --supplemental-junit <focused-final-junit-xml>
  --supplemental-go-results <focused-final-go-test-jsonl>
  --require-applicable-oracles
  --output engineering/verification/<new-versioned-checkpoint>.json
```

The command is shown over multiple lines for readability; pass the arguments
on one line or use the line-continuation convention of the selected shell.
Both supplemental options can be repeated. Select the final campaigns for the
checkpoint, including their failures. Preserve superseded red runs separately;
do not imply that they passed. A new failing or skipped variant prevents credit
even when an imported historical invocation passed. Preserve earlier checkpoints
instead of overwriting them with changed source slices.

`--require-applicable-oracles` fails when any applicable record lacks a validated
test declaration and oracle, or when the applicable inventory is empty. The
reported `applicable_oracle_linkage` covers only these reviewed records. It does
not turn a linked requirement into a satisfied requirement, hide failing or
skipped executions, resolve applicability, or establish an exhaustive standard
inventory.

Review the [completion check](../ieee1516-2010-completion-check-20261002.md)
for the latest reproduced defects and bounded passing evidence. The preceding
[dated assessment](../ieee1516-2010-requirements-review-20261002.md) remains the
earlier source-review checkpoint.
