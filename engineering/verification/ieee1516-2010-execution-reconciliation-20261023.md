# IEEE 1516-2010 execution evidence reconciliation, 23 October 2026

## Source and execution scope

The assessed RTI and SDK source is commit
`4b11da478f423c0776f6ba6ae580f1670b61f2a9`. No server or SDK behavior was changed
for this reconciliation. The requirements inventory is unchanged.

The complete inventory was joined to the retained 19 October Linux default
Go race JSON, Python JUnit, Java Surefire and IVCT-derived JUnit reports. One
previously absent report-claim guard was executed on 23 October: one pass.
That guard validates reporting behavior, not an HLA service.

All 205 distinct linked test-source files are byte-identical to the preserved
Linux campaign source. The supplemental guard file is also unchanged. This
check identifies the test source; it does not make each linked assertion a
complete interpretation of the corresponding standard clause.

## Results

| Review group | Applied: passing linked invocations only | Applied: includes skips | Not applicable | Unresolved |
|---|---:|---:|---:|---:|
| Framework | 5 | 0 | 11 | 0 |
| Federation, synchronization, save/restore | 355 | 0 | 10 | 16 |
| Declaration and object | 88 | 0 | 3 | 1 |
| Ownership and DDM | 26 | 0 | 2 | 1 |
| Time and mapping | 31 | 0 | 0 | 2 |
| Support and MOM | 43 | 0 | 2 | 2 |
| Java annex | 62 | 3 | 2 | 2 |
| Object model template | 25 | 1 | 18 | 1 |
| Total | 635 | 4 | 48 | 25 |

There are 712 reviewed records and 877 requirement-to-test links. Every
applicable record has an oracle and execution linkage. Of all 877 links,
870 have only passing imported invocations and seven include skips. No
linked execution is missing and no imported linked invocation failed.
Repeated references to one test are not additional independent tests.

The four applicable records with skips are:

- `JAVA2010-API-EXACT-PACKAGE`: separate IEEE Java archive not supplied to the campaign.
- `JAVA2010-FDD-SCHEMA-VALIDITY`: external FDD-schema checks not enabled.
- `JAVA2010-VALID-FDD-ADMISSION`: external FDD-schema checks not enabled.
- `OMTSLICE2010-DIF-VALID-XML`: external FDD-schema oracle not enabled.

One further skipped link belongs to a not-applicable record. The 25 unresolved
records retain their interpretation/decomposition status. Some links check
diagnostics, conditional architecture or reporting boundaries rather than
live service semantics. The original oracle and conditions remain attached
to every exported invocation. These counts are execution linkage, not a
claim that 635 or 639 entire normative requirements have been satisfied.

## Reproduce the reconciliation

Evidence classification is separate from execution status. Explicit assignments
in `requirements2010-evidence-classes-20261023.json` bind 464 named targets to
their source and oracle hashes. The exporter rejects missing or stale entries.

| Primary evidence class | Requirement-test links | Distinct named targets |
|---|---:|---:|
| Service and SDK runtime behavior | 766 | 388 |
| API, value encoding and FOM structure | 110 | 75 |
| Documentation and reporting safeguards | 1 | 1 |

Behavioral targets include unit and SDK tests with controlled dependencies;
they are not all live end-to-end tests. MOM service reporting is behavioral.
The documentation/reporting target is the evidence-tool claim guard.

Applicable records partition into 562 behavior-only, 62 API/structure-only,
14 mixed behavior/API, and one reporting-only record. The first group passes;
the API-only group has 60 passing and two skipped records; the mixed group
has 12 passing and two skipped records. This partition is not a count of
entire standard requirements proved. Original open items remain exported.

Use `scripts/ieee2010_requirements_review.py` with `--sources`, `--pdf-dir`,
`--go-results`, `--python-results`, `--java-results`, and supplemental JUnit
inputs for IVCT and the claim guard. The private IEEE source inputs remain
outside the repository. Pass `--require-applicable-oracles` and an external
`--output` destination.

For portable table regeneration, use the metadata snapshot that contains
the reviewed records and invocation results but omits licensed standard text.
Then run:

```sh
python scripts/summarize_requirement_results.py \
  --reconciled /path/to/reconciled-input.json \
  --classes engineering/verification/requirements2010-evidence-classes-20261023.json \
  --source-root /path/to/preserved-linux-source \
  --artifact-root /path/to/execution-reports \
  --output /path/to/traceability
```

`--source-root` and `--artifact-root` enable optional source and original-report
hash checks. The aggregation itself needs only the two JSON inputs and Python's
standard library, without Git or licensed PDFs.

`requirements-summary.json` gives group totals, evidence classes and exceptions.
`requirement-invocations.csv` gives each requirement, source clause, test
oracle, evidence class, test-source hash, actual invocation identity, result,
and source artifact hash. `requirement-status.csv` retains one row per record,
including unlinked non-applicable/unresolved records and open items. Raw
execution records remain outside Git. Seven focused tests cover the exporter;
they are reporting-tool checks, not additional HLA service evidence.
