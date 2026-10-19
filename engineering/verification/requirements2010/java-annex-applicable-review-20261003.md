# Java Annex Applicable Requirement Review

Date: 2026-10-03. Scope: `S1516.1`, selected Java binding, assertion-level links only.

## Plan

Review the 37 applicable Java rows lacking literal test links. Read assertion bodies
and their fixtures, distinguish imported API declarations from implementation
behavior, and retain unresolved service/callback facets. Own only `java-annex.json`,
this review, and the uniquely named new Java test class. No production or other
slice changes, Git operations, network downloads, or live runtime campaign.

## Do

- Added specific test links to 32 previously unlinked applicable rows. Updated
  two already-linked rows with additional primitive-codec and retained-memory
  assertions: 34 rows changed, with requirement IDs and source locators preserved.
- Newly linked IDs are `JAVA2010-NULL-AMBASSADOR`, `JAVA2010-ENCODING-COMPLEX`,
  and `JAVA2010-SERVICE-MAPPING-01` through `34`, except already-linked `11`,
  `24`, `26`, and `28`. Row-local oracles identify exactly what is asserted.
- Added `JavaAnnexApplicable20261003Test` with three methods:
  `nullAmbassadorInvokesEveryCallbackAsNoOp`,
  `synchronizationRegistrationCallbacksPreserveLabelAndBothFailureReasons`, and
  `singleAndMultipleReservationCallbacksPreserveRequestedNames`.
- These tests were written before execution. Existing production passed them;
  this is a new characterization result, not an invented RED/GREEN correction.
- Executed eleven selected unit/loopback suites offline from a source copy under
  `D:/workspace/gorti/applicable-requirements-20261003-r1/java/work`:
  **204 executions, zero failures, errors, or skips**, including three new cases.

## Review

The decisive existing oracles check independent primitive byte vectors and
decode/set values; nested complex encoding, array size and variant selection;
distinct declaration RPC types/options; optional save time; participant transition
RPC names; synthesized save/restore reason enums; discovery overload selection;
timestamp/retraction return mapping; per-attribute regional groups; and all three
ownership-report callback forms. Recording-channel and loopback assertions are
not real-production service effects. Synthesized callback events are not evidence
that the server generated, routed, scheduled or admitted those events correctly.

Specific limits remain explicit: positive default synchronization achievement is
not asserted; empty connect settings are not exercised; additional FOM URL-array
merge semantics are not covered; discovery's optional-producer test records the
chosen overload but does not assert all argument values; and timed nonregulating
send/delete tests do not assert wire timestamp presence. The slice does not claim
these missing facets merely because the containing suite passed.

Source: supplied `15161_2010-2004_redline.pdf`, SHA-256
`789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4`.
Rechecked active-candidate content on one-based PDF pages 344-350 for
12.11.3.1/12.11.3.2 and 12.11.4.1-34. Visually inspected rendered pages 349-350:
the deleted older reflection overload prose and Initialize RTI text remain
excluded. No raw PDF text was copied into the repository and no reviewed unit
was added. The retained helper-table/archive inconsistency remains unresolved.

The existing external comparison records were read, not rerun:
`D:/workspace/gorti/internal-java-assessment-20261002/external-source-comparison.json`
and `api-signature-comparison.json`. They record 204 API classes, 202 byte-identical
sources and two adapted discovery helpers, with identical public/protected
signatures. The incorporated Java ZIP hash is
`66485e053b6183df2df94e35de7c609677f2ccaef0830edde014aad066e41890`.
Current/imported source correspondence is recorded in this run's external
manifest; historical signatures are not promoted to current semantic execution.

## Reflect

Applicable unlinked rows decreased from 37 to five: `JAVA2010-API-EXACT-PACKAGE`,
`JAVA2010-FDD-SCHEMA-VALIDITY`, `JAVA2010-VALID-FDD-ADMISSION`,
`JAVA2010-MIM-TABLE-PRECEDENCE`, and `JAVA2010-SELECTED-BINDING-SCOPE`.
These need independent API/provenance assertions, schema-grounded Java admission,
MIM/table discrepancy resolution, or a complete service/callback assessment;
generic signature compilation is not a substitute. All remaining facets stay
open and `coverage_complete` stays false. No production defect was reproduced
in this bounded selected run; no full Java or IEEE conformance claim follows.

## Execution

Java 11.0.31, Maven 3.9.16, offline repository `D:/workspace/gorti/java-m2`.
The exact arguments, source hashes, JUnit paths and counts are retained in
`D:/workspace/gorti/applicable-requirements-20261003-r1/java/evidence-manifest.json`
and `run-config.json`. Core command:

```powershell
mvn.cmd -o -B -ntp -f D:/workspace/gorti/applicable-requirements-20261003-r1/java/work/javasdk/pom.xml -Dmaven.repo.local=D:/workspace/gorti/java-m2 -Dtest=JavaAnnexApplicable20261003Test,CompoundEncodingTest,PrimitiveEncodingTest,FederationTimeSelectionTest,SynchronizationServicesTest,SaveRestoreServicesTest,ExchangeServicesTest,ExchangeCallbacksTest,DdmServicesTest,OwnershipServicesTest,ProviderTest -Dsurefire.failIfNoSpecifiedTests=false test
```

The subprocess finished successfully. Only in-process/loopback fixtures were used;
no `rtid` server was started. Production/live and external-unadapted-loader scope
remain unexecuted in this increment. Existing unrelated dirty files were preserved.
