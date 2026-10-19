# MIM Table-Precedence Repair, 2026-10-04

## Decision

The supplied IEEE 1516.1-2010 redline, PDF page 298, Table 6, names the
federation's next-save label `HLAAnnexSaveName`. The downloadable Annex G MIM
uses `HLAnextSaveName`. Clause 11.6 (PDF page 292) and Annex G (PDF page 583)
give the Clause 11 tables precedence. The runtime now applies that rule to
this attribute. This is an implementation decision based on the supplied
standard, not a claim that IEEE issued a corrected XML file.

The vendored XML is unchanged. Its SHA-256 remains:

`839f092b4b3e02a1363ec4463efcb1aae7fbfdfb17778b159065f277a3f2b023`

This matches `1516.1-2010_downloads/HLAstandardMIM.xml` in the IEEE archive
reviewed for the preceding service assessment. No standard PDF or extracted
table text is added to Git.

## Implementation

- Go MIM decoding, the public FOM parser and the Python parser normalize only
  the member of `HLAobjectRoot.HLAmanager.HLAfederation`.
- Effective FDD, reverse attribute-name lookup and MOM metric output use
  `HLAAnnexSaveName`. It remains the tenth local attribute, of type
  `HLAunicodeString`; `HLAnextSaveTime` is unchanged.
- Support-service lookup accepts `HLAnextSaveName` as a compatibility alias
  for the same handle. There is no additional published attribute.
- Selected MIM validation accepts either spelling at this declaration and
  still rejects unrelated name, structure, namespace and encoding changes.
- Version-1 FOM snapshot import recognizes the old member name while
  preserving handle assignments, source bytes, digest and revision.
- MOM continues to obtain the value from `NextSaveName` in the save manager.
  The save algorithm, admission rules and callback suppression are unchanged.
- The Java test no longer requires `gorti.sourceDiagnostics=true`. With a
  configured endpoint it runs as an ordinary acceptance test.

Source reporting and effective FDD are deliberately distinct. `StandardMIMBytes`,
selected MIM transport, source digests and MIM source reports retain the
original supplied bytes. Consumers of name-keyed metric maps or local SDK
selectors should use `HLAAnnexSaveName`; reverse lookup no longer returns the
legacy spelling. Unrelated user attributes are not renamed.

## Verification

Before implementation, new Go runtime/snapshot tests and two Python parser
variants failed on the old spelling. After the repair:

| Check | Result |
|---|---|
| Go FOM packages, composition, MOM, server and gRPC | 1,045 top-level tests passed; 2,572 nested pass events; no failed or skipped tests |
| Official FDD XSD checks within that Go run | Executed and passed with the supplied supplementary schema |
| Python parser, MIM selection, handle alignment, Support and live MIM/save/restore | 51 passed; no failures or skips |
| Java MIM, FDD, Support and creation against rebuilt server | 21 passed; no failures or skips |
| Linux race checks for MIM-related tests, repeated twice | 37 top-level tests x 2 = 74 passes; 70 nested pass events; no race reports, failures or skips |
| Python lint on changed Python files | Passed |

The Python live test covers default MIM, the original selected XML and a
selected XML using the table spelling. It checks lookup/reverse lookup,
canonical current FDD, initial and completed-save reflection payloads,
successful save/restore, handle retention and selected-source retention.
Saving federates cannot issue ordinary MOM queries, and reflections are
suppressed during saving. Pending-label values are therefore checked through
the production server's metric provider after real save/abort RPCs, rather
than by weakening service admission to accommodate the test.

Development attempts are retained separately. One Go attempt used an
incomplete FDD fixture; it was replaced with the existing complete fixture.
The exporter metadata oracle was updated to compare the explicitly renamed
declaration while retaining its descriptive text. Initial live tests tried
to query or await a reflection during saving; the final tests respect the
existing protocol. These intermediate failures are not included as passes.

## Evidence

Raw output remains outside the repository:

- `D:/workspace/gorti/mim-table-fix-20261004/go-red.jsonl`
- `D:/workspace/gorti/mim-table-fix-20261004/python-red.xml`
- `D:/workspace/gorti/mim-table-fix-20261004/go-final-r2.jsonl`
- `D:/workspace/gorti/mim-table-fix-20261004/go-race-final.jsonl`
- `D:/workspace/gorti/mim-table-fix-20261004/python-final.xml`
- `D:/workspace/gorti/applicable-fixes-20261004/java/mim-table-precedence/`

The real-server tests used the executable at
`D:/workspace/gorti/mim-table-fix-20261004/rtid.exe`, SHA-256
`14ce07728bfb00fae12fba2c1de84e4451eaa63021f2216f8370379c0c7a2a3f`.
Java records include the selected tests, source hashes, server hash and
owned-server shutdown. Python independent tests also verify the binary hash
and record server shutdown.

`JAVA2010-MIM-TABLE-PRECEDENCE` now links the passing tests. This closes the
identified Table 6 save-label discrepancy, not the wider Clause 11 table
inventory or whole-RTI conformance assessment.
