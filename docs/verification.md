# Verification results

gorti maintains requirement-derived test cases for conformance assessment and
regression testing. The tests connect reviewed IEEE 1516-2010 clauses to
inputs, expected observations, implementation locations, and execution reports.
See [test-case design](test-cases.md) for a worked example.

## Latest retained service campaign

The [19 October 2026 Linux campaign](https://github.com/cbchoi/gorti/blob/main/engineering/verification/linux-publication-validation-20261019.md)
assessed commit `4b11da478f423c0776f6ba6ae580f1670b61f2a9`.
It ran on Ubuntu 24.04 under WSL 2 with sources on the native Linux filesystem,
Go 1.22.2, Python 3.12.3, Temurin JDK 11.0.32.1, and Maven 3.9.16.

| Selection | Passed | Failed/errors | Skipped | Counting unit |
|---|---:|---:|---:|---|
| Go default build with race detector | 3,533 | 0 | 19 | Top-level tests in 80 packages |
| Go asynchronous build with race detector | 972 | 0 | 0 | Top-level tests in three packages |
| Python SDK and selected HLA services | 2,127 | 0 | 0 | Reported tests |
| Java SDK and live services | 581 | 0 | 7 | Reported invocations |
| Internal IVCT-derived subset | 35 | 0 | 0 | Reported cases |

The default Go run also contains 7,020 passing subtests; the asynchronous
selection contains 2,357. Subtests, overlapping selections, and repeated
runs are not additional independent requirements.

Sixteen default Go skips are asynchronous-only cases exercised in the tagged
run; three require FDD-schema resources. Java skips comprise one comparison
against an external IEEE API archive, three FDD-schema checks, and three
cross-binding fixtures. Static/protocol checks, lint, the strict documentation
build, and ten repeated determinism runs also passed. These are recorded local
results, not a claim that hosted CI executed the same campaign.

## Requirement-to-result reconciliation

The [23 October reconciliation](https://github.com/cbchoi/gorti/blob/main/engineering/verification/ieee1516-2010-execution-reconciliation-20261023.md)
checked all 205 linked test-source files against the retained campaign source.
They matched byte for byte. It added one focused reporting-guard execution;
it did not repeat the complete service campaign.

| Status | Records |
|---|---:|
| Applicable, passing linked invocations only | 635 |
| Applicable, linked results include skips | 4 |
| Not applicable | 48 |
| Unresolved | 25 |
| Total reviewed records | 712 |

All 639 applicable records have linked results, with no linked failures.
Of 877 requirement-test links, 870 contain only passing imported invocations
and seven contain skips; one skipped link belongs to a not-applicable record.

The four applicable records with skips are
`JAVA2010-API-EXACT-PACKAGE`, `JAVA2010-FDD-SCHEMA-VALIDITY`,
`JAVA2010-VALID-FDD-ADMISSION`, and `OMTSLICE2010-DIF-VALID-XML`.
Their missing resources are the external API archive or FDD schemas.

The unresolved records retain questions about source interpretation,
callback-exception responsibilities, custom-time conditions, and the
decomposition of state charts and tables. These counts describe a reviewed
inventory and its linked executions, not a percentage of the standard proved.

## What the evidence measures

| Primary class | Requirement-test links | Distinct named targets |
|---|---:|---:|
| Service/SDK behavior | 766 | 388 |
| API/structure | 110 | 75 |
| Documentation/reporting | 1 | 1 |
| Total | 877 | 464 |

Behavioral evidence includes controlled unit/SDK tests and live service tests.
API/structure checks cover interface shape, encodings, time/handle helpers,
and FOM/MIM/XML parsing. The reporting guard tests the report tool, not an
RTI service. MOM reporting remains a service case.

The contribution is an inspectable collection of test cases, oracles, and
traceability records that can be reused and extended. It is not an independent
conformance certificate. The [detailed guide](https://github.com/cbchoi/gorti/blob/main/engineering/verification/ieee1516-2010-test-case-guide.md)
provides record groups, representative cases, source identities, and
classification rules.

## Service and cross-language coverage

Internal tests exercise federation, declaration, object, time, ownership, DDM,
synchronization, save/restore, MOM, Support, FOM handling, and encodings.
Live cases include dynamic FOM joins, Float64 and Integer64 time, timed saves,
callback recovery, SDK state restoration, and session controls.

Go/Python cases check timestamped delivery and grants under controlled peer
bounds. Java/Python cases check opaque parameter bytes, tags, order,
transport, producer identity, and evoked dispatch. The
[model integration example](model-integration.md) adds a reusable simulation
workflow with an independent trajectory reference.

Cross-RTI scenarios separately compare FM, DM, OM, and selected TM behavior.
Their validators compare complete canonical service records after checking
payloads, callbacks, and times; equal event counts alone are insufficient.
Portico performance trials do not establish that Portico passed gorti's
internal requirement inventory.

## Run checks for your checkout

From the repository root, after [installing the required SDK tools](installation.md):

```text
go test ./...
python -m pytest pysdk/tests verification/common verification/gorti
mvn -f javasdk/pom.xml verify
```

These are developer entry points, not a command set that reproduces all counts
above. Live endpoints, fixtures, build tags, race settings, and selection
filters must match the recorded campaign to reproduce its scope. Use the
[Linux campaign commands](https://github.com/cbchoi/gorti/blob/main/engineering/verification/linux-publication-validation-20261019.md)
and SDK guides for those settings.

[Reproducibility](reproducibility.md) separates fresh executions from
aggregation of archived results. The
[STD](https://github.com/cbchoi/gorti/blob/main/engineering/specifications/current/STD.md)
records test design and acceptance criteria.
