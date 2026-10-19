# IEEE 1516-2010 Internal Java Binding And Runtime Audit

Date: 2026-10-02. Repository: `D:/workspace/gorti/gorti`. Read-time HEAD:
`518cb67f00858fcbec9ed86145d5b2360cfb2241`; extensively dirty, including an
untracked `javasdk/` tree. HEAD does not identify the assessed working-tree inputs.

## Verdict

**Full Java semantic conformance and release-wide IEEE 1516-2010 conformance are
not established.** This campaign establishes external official-API source and
signature provenance, independent provider/client compilation, actual factory
loading with its documented Java 11 adaptation, and substantial real-server
behavior. It also reproduces three current behavioral defects, one obsolete
test oracle, and a connection/restart reliability observation.

The 172-signature `MAPPED` inventory is not a semantic coverage percentage.
The main coordinator's readiness result of 43 metadata messages and zero commands
started is not a Java runtime campaign or 43 production defects. No readiness,
freeze-state, or smoke-test gate was executed here. The separately reported
Windows Go totals are not included in the Java counts below.

All execution/build outputs and raw evidence are outside Git under
`D:/workspace/gorti/internal-java-assessment-20261002/` (called `E` below).
The only repository write is this report. No production source, test, manifest,
or API declaration was edited, and no commit or history change was made.

## Reproduced Findings

### J1: Callback Switches Ignore Save And Restore Guards

`AmbassadorServices.enableCallbacks` and `disableCallbacks` only assign a local
boolean at [lines 490 and 493](../../javasdk/client/src/main/java/org/gorti/rti/AmbassadorServices.java#L490).
After actual `initiateFederateSave` plus `federateSaveBegun`, both calls return
success rather than `SaveInProgress`. After actual restore initiation, both
return success rather than `RestoreInProgress`.

Four independently authored public-API tests reproduced all four conditions:
**0 passed, 4 failed, 0 skipped/aborted**, execution exit 1. This is missing
precondition/exception enforcement, not an entirely inert callback switch.
The calls can mutate callback availability while the protected operation is active.

Normative basis: IEEE 1516.1-2010 10.43.3(c-d), 10.43.5(a-b),
10.44.3(c-d), and 10.44.5(a-b). Supplied redline physical PDF pages 284-285 were
rendered and visually inspected; these conditions are current text, not deleted
redline passages. The external Java declarations also retain these exceptions.
Evidence: `E/CallbackGateContractTest.java`, `callback-gate-contracts.log`,
`callback-gate-summary.json`, and `callback-spec-284.png` / `callback-spec-285.png`.

### J2: Idle Restore Completion Returns The Wrong Exception

The existing `LiveSaveRestoreTest.abortAndParticipantFailuresDeliverTheExactStandardReason`
fails at [line 219](../../javasdk/client/src/test/java/org/gorti/rti/LiveSaveRestoreTest.java#L219):
`federateRestoreComplete()` without a requested restore returns `RTIinternalError`
with `Java runtime restore state has not been validated`, rather than
`RestoreNotRequested`.

[SaveRestoreServices.java:98](../../javasdk/client/src/main/java/org/gorti/rti/SaveRestoreServices.java#L98)
checks SDK restoration readiness before asking the server; the check at
[AmbassadorServices.java:863](../../javasdk/client/src/main/java/org/gorti/rti/AmbassadorServices.java#L863)
therefore masks the operation's idle-state error. The official API locates this
service at 4.28 and declares `RestoreNotRequested`.

Reproduced in both the ordinary full live run and the separately compiled
external-API rerun. Do not credit the abort/failure-reason assertions later in
this test: execution stops at this early assertion. Other save/restore tests
passed, but that is not evidence for the unreachable assertions.

Evidence: `E/live-reports/TEST-org.gorti.rti.LiveSaveRestoreTest.xml`,
`external-api-rechecks.log`, and `recheck-summary.json`.

### J3: Empty FOM Is Admitted Without The Default MIM

The [normative reviewer](ieee1516-2010-internal-normative-audit-20261002.md)
identified an empty-module ingestion shortcut and explicitly required a public
Java reproduction. This campaign used the standard factory/ambassador API with
`createFederationExecution(name, new URL[0])`, no explicit MIM, and the pinned
server below. Creation and join both succeeded. A subsequent
`getObjectClassHandle("HLAobjectRoot.HLAmanager.HLAfederate")` raised
`hla.rti1516e.exceptions.NameNotFound`.

The control used one valid local FOM and no explicit MIM. Creation and join
succeeded, and the identical standard-MIM class lookup returned `ObjectClass(1)`.
Both executions resigned, destroyed their own federation, and disconnected.

The two-case campaign produced **1 passed, 1 failed, 0 skipped/aborted**, exit 1.
It establishes actual missing-FOM admission and absent default-MIM declarations
in that admitted execution, not merely a source hypothesis. The negative oracle
uses the reviewer's IEEE 1516.1-2010 4.1.4 / 4.5 basis, physical pages 48 / 52.
It does **not** invent a required exception identity for an empty array or claim
that a valid-FOM/default-MIM path is broken.

Source anchors: [AmbassadorServices.java:171](../../javasdk/client/src/main/java/org/gorti/rti/AmbassadorServices.java#L171)
rejects null but not an empty array; the composition shortcut is at
[fom_repository.go:67](../../rti/internal/application/composition/fom_repository.go#L67).
Evidence: `E/EmptyFomContractTest.java`, `empty-fom-contracts.log`,
`empty-fom-summary.json`, and `empty-fom-contracts-command.json`.

### J4: A Time-Selection Test Still Uses A Float-Only Oracle

[LiveFederationTimeSelectionTest.java:80](../../javasdk/client/src/test/java/org/gorti/rti/LiveFederationTimeSelectionTest.java#L80)
includes `HLAinteger64Time` among unsupported selectors. The current server
correctly accepts it, so that test fails in both ordinary and external-API runs.
This is a stale test oracle, **not** a reproduced Integer64 production defect.
The subsequent `VendorExactTime` iteration is never reached and remains
unverified by this particular test.

The seven `LiveInteger64ProductionTest` executions pass; independent public
clients also select Integer64 and receive an exact grant at `9007199254740993`.
README rows claiming production Integer64 activation/time are absent are stale.
No test or README was repaired within this read-only audit.

### J5: Connection Timeout Observations Require Retention

The full run's first Float64 successful-outcome restart case failed because its
replacement JVM could not complete the initial `ListFederations` connection RPC
within the configured deadline. An initial adapted-discovery probe similarly
loaded factories successfully but timed out on its next loopback connection.
These are recorded runtime failures, not source-only observations.

The complete six-case restart suite passed on a fresh external-API rerun. The
adapted-discovery retry passed all 54 checks. Those later passes do not erase
the two original failed executions or prove that their cause was environmental;
the timeout root cause was not isolated in this bounded campaign.

Evidence: `E/live.log`, `child-evidence/HLAfloat64Time-true/restore-child.log`,
`external-api-adapted-discovery-runtime.log`, `adapted-discovery-runtime-retry.log`,
and `recheck-child-evidence/`.

## Official API And Runtime Provenance

The authoritative [IEEE 1516.1-2010 page](https://standards.ieee.org/ieee/1516.1/3745/)
still links the [supplemental download](https://standards.ieee.org/wp-content/uploads/import/download/1516.1-2010_downloads.zip).
The historical temporary archive path is absent. A retained external copy at
`D:/workspace/gorti/ieee2010-source-intake-20260921/1516.1-2010_downloads-restored.zip`
was independently hashed and copied into `E`. A preliminary network retrieval
attempt failed under local socket restrictions; no runtime dependency was downloaded.

| Artifact/check | Actual result |
|---|---|
| External outer ZIP SHA-256 | `e6343f40b066d51b7f435c9aae6788be24f5d585e370d972c032ab0b503e27c1` |
| Nested Java ZIP SHA-256 | `66485e053b6183df2df94e35de7c609677f2ccaef0830edde014aad066e41890` |
| All imported Java sources versus external archive | 204 files; 202 byte-identical; exactly two differences; no extra imported Java sources |
| Two differences | `RtiFactoryFactory.java` and `LogicalTimeFactoryFactory.java`, the documented `ServiceLoader` adaptations |
| Unmodified external API compile / JAR creation | Both exit 0 on Java 11 |
| Public/protected declared signatures, constructors, and fields across 204 API classes | Original/imported reflection dumps identical; not a comparison of private implementation or complete normative semantics |
| Full provider/examples compile against external original API | Exit 0; local imported API excluded from compilation classpath |
| Existing test sources and independent probes compile against external API/runtime | Exit 0 |
| Generator/inventory check using external original API | Exit 0; 172 `MAPPED`, zero `PENDING` signatures |
| Original discovery helpers on Java 11 | Two expected `IllegalArgumentException` ImageIO-SPI rejections reproduced; 2 negative compatibility checks passed, not successful original-helper discovery |
| Original API with direct JDK `ServiceLoader` | 50 probe checks passed; actual Float64/Integer64 server lifecycle, exceptions, factories, and callbacks; exit 0 |
| Original API plus exactly two helper-class overlay | Initial execution failed at connection after discovery; fresh retry 54 checks passed, exit 0 |

Runtime origins in probe logs show `RTIambassador` loaded from
`E/external-original/api.jar`; only the two discovery helpers come from
`E/loader-overlay/classes` in the adapted profile. The factory, ambassador,
encoders, and time implementations come from the independently compiled
`E/external-provider/classes`. The external runtime classpath contains neither
the repository API JAR/classes nor the ordinary Maven client classes. No vendor
RTI API/implementation JAR is substituted. Exact classpaths are retained in the
command JSON and `external-provider-dependencies.json`.

Named/default/enumerated RTI discovery and Float64/Integer64 logical-time
factory discovery actually execute in the adapted profile. Both public time
selections are joined and queried, all eight handle and seven collection
factories are requested, an independent integer32BE vector is checked, and real
regulation/TAG callbacks run on the evoke thread. Compilation alone is not the
runtime result. Original helpers remain incompatible with this JDK; the working
adapted profile is explicitly not byte-identical original-helper execution.

## Runtime And Exact Suite Results

Environment: Windows 11 amd64; Temurin Java `11.0.31`; Maven `3.9.16`;
gRPC Java `1.84.0`; Protobuf `3.25.9`; installed dependencies from
`D:/workspace/gorti/java-m2`, Maven offline mode. Java builds ran in a copied
`E/work/javasdk` reactor, with copied `proto` inputs and fresh build outputs.

Server: `D:/workspace/gorti/rtid-sdk-gap-fix-20261001.exe`, SHA-256
`6c9cf2c18c06236a909e900d9d70e58db6fa31e1d6708e81400e44f5ab17d979`,
rechecked unchanged before/after the ordinary campaign. The coordinator allowed
retaining this candidate after building a newer October 2 executable. No Java
result here is claimed as execution against that newer executable.
Servers use separate available loopback ports, state/save directories under `E`,
and plaintext gRPC. Owned server processes were stopped; test-owned replacement
JVM/server lifetimes are bounded by the existing tests.

| Execution | Reported/found | Passed | Failed | Errors | Skipped | Exit |
|---|---:|---:|---:|---:|---:|---:|
| Full offline Maven `verify`, no live properties | 491 | 447 | 0 | 0 | 44 | 0 |
| Full Maven `verify`, endpoint/FOM/server-binary properties supplied | 505 | 499 | 3 | 0 | 3 | 1 |
| External-original-API plus helper overlay: 16 selected existing suites | 206 | 205 | 1 | 0 | 0 | 1 |
| External-API save/restore and restart rerun | 11 | 10 | 1 | 0 | 0 | 1 |
| Independent callback-state contract probes | 4 | 0 | 4 | 0 | 0 | 1 |
| Independent empty-FOM/default-MIM probe and valid control | 2 | 1 | 1 | 0 | 0 | 1 |

These are separate executions with overlaps, not additive unique coverage.
Standalone probe assertion counts (50 / 54 / 2) are not JUnit test counts.
JUnit external executions had zero aborted tests and zero failed containers.
Maven totals were independently calculated from all 53 retained Surefire XML
suites, not inferred from a successful wrapper exit. The orchestration scripts
can themselves complete normally after recording failed subprocesses.

The no-endpoint run skips 41 live method placeholders and three central fixture
methods. Enabling parameterized live cases changes the reported denominator.
The full live run has **55 actual live executions: 52 passed, 3 failed, zero
live skips**; the other **447 unit/in-process tests passed**, with three central
fixture methods skipped. That is why 491 and 505 are not directly comparable
unique-test totals.

| Live suite | Executions | Passed | Failed |
|---|---:|---:|---:|
| `LiveCheckpointProcessTest` | 1 | 1 | 0 |
| `LiveControlOutcomeRestoreTest` | 6 | 5 | 1 |
| `LiveDdmTest` | 3 | 3 | 0 |
| `LiveExchangeTest` | 1 | 1 | 0 |
| `LiveFederationTest` | 1 | 1 | 0 |
| `LiveFederationTimeSelectionTest` | 5 | 4 | 1 |
| `LiveInteger64ProductionTest` | 7 | 7 | 0 |
| `LiveOwnershipTest` | 2 | 2 | 0 |
| `LivePortableRuntimeRestoreTest` | 2 | 2 | 0 |
| `LiveRestoreLookupTest` | 1 | 1 | 0 |
| `LiveRestoreReceiptTest` | 2 | 2 | 0 |
| `LiveRetractionTest` | 4 | 4 | 0 |
| `LiveSaveRestoreTest` | 5 | 4 | 1 |
| `LiveSupportTest` | 6 | 6 | 0 |
| `LiveTimeArithmeticTest` | 2 | 2 | 0 |
| `LiveTransportationTest` | 7 | 7 | 0 |

The external selection comprises `ProviderTest`; five handle/time suites;
four encoding suites; `ExchangeCallbacksTest`; `LiveFederationTest`,
`LiveFederationTimeSelectionTest`, `LiveInteger64ProductionTest`,
`LiveExchangeTest`; and `CentralRuntimeFixtureExchangeTest`.
That is 186 unit tests, six central-fixture parameter executions, and 14 live
executions (13 passed, one stale-oracle failure). Exact IDs and selection are in
`external-api-selected-suites.log` and `external-suite-selection.txt`.
The six central checks consume copied, retained Go-translated fixtures; they are
not a new cross-binding live campaign. The rerun is five save/restore cases
(4 passed / 1 failed) and six restart cases (all passed).

## Mapped Services, Stubs, And Coverage Boundaries

All 172 facade methods forward to typed service implementations; no generated
pending-service throw or blanket successful no-op was found in the inspected
facade. The external interface has 45 encoder-factory overloads and 60 federate
callback signatures. These inventories do not establish all normative branches.

Intentional local operations are distinguished from omitted semantics:
`getHLAversion` returns an identification string, not a conformance certificate;
factory getters return local factories after membership checks; callback switches
actually change delivery state but omit J1's guards. Empty targeted attribute
removal uses explicit wire presence, not a synthetic clear-all fallback.
Internal no-op extension hooks in `ServiceContext` are overridden by the actual
ambassador. Default `Callback.retire`, scalar validation hooks, and convenience
`NullFederateAmbassador` callback bodies are not independent implemented services.
Unknown wire callback variants fail explicitly at `AmbassadorServices.java:596`;
their failure is not proof that every official callback is implemented.

Observed live paths include all five advance primitives, peer-blocked time
coordination, exact Integer64 timestamps/ordering, ownership, regional routing,
transportation confirmation entry, advisory switches, retraction, timed/immediate
save, restored identity/knowledge, replacement JVMs, and server restart outcomes.
Current Integer64, durable restoration, or DDM work must not be declared absent
solely because an older README/report describes a previous increment.

Remaining assessment work includes complete service-specific input/state/error
and unchanged-post-state oracles; every overload and callback trigger; ownership,
DDM, declaration, and time cross-state interactions; complete MOM accounting and
MIM behavior; full time/encoding endpoint, epsilon, arithmetic, alignment, and
caller-data contracts; and corrections/applicability/obligation-level traceability.
The new failures require correction and focused retesting before their containing
contracts can be credited. Existing test count or mapping cannot close these.

Only `HLA_EVOKED` is implemented; `HLA_IMMEDIATE` is explicitly rejected by a
tested standard exception. The normative review confirms that implementing at
least one callback model is sufficient: IMMEDIATE is **not** automatically a
missing mandatory implementation. File-only FOM/MIM URLs, lack of DDS/reconnection,
other JDKs/operating systems, and TLS execution are profile limits or unverified
claims, not newly reproduced violations. Plaintext same-host results do not prove
TLS, independent-host, arbitrary cross-language, performance, or other-vendor
replacement conformance. No full OMT-tool or arbitrary user-federate correctness
claim is assessed by this Java campaign.

## Reproduction And Evidence

Exact Maven arguments, timestamps, status, classpaths, and subprocess commands
are retained in `E/*-command.json` / `E/*-exit.json`. Core commands were:

```powershell
& D:/workspace/gorti/internal-java-assessment-20261002/run-assessment.ps1
python D:/workspace/gorti/internal-java-assessment-20261002/external-api-audit.py
python D:/workspace/gorti/internal-java-assessment-20261002/external-runtime-audit.py
python D:/workspace/gorti/internal-java-assessment-20261002/run-external-suites.py
python D:/workspace/gorti/internal-java-assessment-20261002/run-rechecks.py
python D:/workspace/gorti/internal-java-assessment-20261002/run-empty-fom.py
python D:/workspace/gorti/internal-java-assessment-20261002/summarize-evidence.py
```

The Maven reactor commands use `-o -B -ntp -f E/work/javasdk/pom.xml
-Dmaven.repo.local=D:/workspace/gorti/java-m2 verify`; live properties add
`gorti.endpoint`, the absolute `gorti.fom`, `gorti.server.binary`,
`gorti.live.evidence`, and an outside-Git `java.io.tmpdir`.
The external inventory command uses Java source-file execution of the copied
`GenerateAmbassador.java`, `--class-path E/external-original/api.jar`, and
the copied SDK path with `--check`; it does not generate or modify repository files.

Evidence includes `unit.log`, `live.log`, both complete report directories,
`suite-results.json`, external source/signature manifests, runtime origin logs,
individual JUnit test IDs, preserved child logs, server command/cleanup records,
`source-hashes.json`, and `evidence-manifest.json`. A first external compilation
attempt failed because Windows backslashes in a javac argument file were not
escaped; its command/argument/log/exit records remain under
`external-provider-compile-path-error*`. The corrected compile passes. This
setup error and the failed network retrieval are not product semantic tests.

This is a bounded internal assessment with explicit failures and uncovered
contracts. It neither asserts release full readiness nor authorizes a public
IEEE/HLA conformance label.
