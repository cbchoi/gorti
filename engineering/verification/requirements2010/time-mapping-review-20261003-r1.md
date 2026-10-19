# Time/mapping applicable-requirement review

## Plan

Review the seven applicable rows without test links in `time-mapping.json`, retaining their S1516.1 source conditions and distinguishing partial assertions from whole-requirement verification. No production code, other slices, source-unit census, or Git state is changed.

## Do

The source is the supplied S1516.1 redline PDF, SHA-256 `789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4`. The private page projection is a candidate extraction, not a resolved edition. Existing source-reviewed statements remain unchanged; no new reviewed units are claimed.

| ID | Source clause / PDF pages | Added evidence and boundary |
| --- | --- | --- |
| TM-PREFERRED-ORDER | 8.1.2 / 182 | `TestPreferredOrderProfilesAndOverrides`: stateful injected-FOM preferences and overrides determine six message paths, sent/received order, timestamp/tag and retraction behavior; deletion uses its privilege attribute. Parsed FDD/inheritance, class immutability and independent instances/publishers remain open. |
| TM-REGULATION-LIFECYCLE | 8.1.4 / 184-185 | `twoSessionsObserveActualEnableCallbacksAndPeerDependentGrants`: live Java callback counts/times, duplicate-enable exception and typed lookahead values. It does not independently establish send eligibility before callback entry, disable, invalid lookahead or all enable-service errors. |
| TM-LEGAL-ORDER-TYPES | 8.1.11 / 192 | `allBasicFomOrderAndTransportationLookupsUseRpc` checks selected name/code conversions against scripted RPC; `TestOrderStateRoundTripAndValidation` checks persisted orders, rejection of 99 with unchanged state and destruction cleanup. Complete live Java name/handle directions and declared error classification remain open. |
| TM-TARA-QUEUED-EQUALITY | 8.9 / 201-203 | New `TestApplicableTimeSupportTARAQueuedAndFutureEquality` observes buffered RO and reverse-arriving TSO 5/4 delivered before TAG 5; later equality is accepted and held until another same-time TARA. Component outbox evidence, not public Java callback-entry or complete exception coverage. |
| TM-TAG-CLOSURE | 8.13 / 208-209 | The same new test independently observes postgrant equality without time mutation and its later delivery. Only available-mode equality is linked; strict closure, below-grant exclusion, other modes and page-209 callback conditions remain open. |
| TM-QUERY-ACTUAL-LOOKAHEAD | 8.20 / 214-215 | Live Java returns typed HLAfloat64Interval 0.5 and then 1 after immediate increase. Delayed reduction's actual-versus-requested distinction, Integer64 and negative states remain open. |
| MAP-LANGUAGE-TRANSPARENCY | 12.1 / 323 | Remains unlinked: historical translated fixtures and separate language runs are not simultaneous cross-language participant exchange. No requirement to certify arbitrary federate models or every Annex binding is introduced. |

The new TARA test is a production time-manager/outbox component test. It neither asserts a mandatory tie order for equal TSO nor runs the assigned server executable.

## Review

Fresh execution records are outside Git at `D:/workspace/gorti/applicable-requirements-20261003-r1/time-support/`:

- `focused-go.jsonl`: 14 top-level tests and 300 subtests passed; zero failed or skipped. Includes all selected positive and negative variants across time, object, MOM, selected-MIM and server RPC packages.
- `order-state-go.jsonl`: one additional top-level test passed; zero failed or skipped.
- Combined fresh scope: 15 top-level tests and 300 subtests passed, zero failures/skips. These are component/in-process RPC tests, not a new executable or language-API campaign.
- `evidence-summary.json` records literal invocation IDs, commands, hashes, and Java source correspondence.

Reused Java evidence is from `D:/workspace/gorti/full-consistency-20261003-r1/java/work/javasdk/client/target/surefire-reports/`. The linked LiveFederationTest and ExchangeServicesTest declarations passed, and their current source hashes equal the executed build copies. That original campaign was **not PASS**: 537 passed and three JVM-child timeout failures out of 540 invocations, with zero errors/skips. A separate unchanged six-case restart retry passed; it does not erase the initial failures. The live server pin was `fc2bd04a5e64970e2dd0f6cb37f98741a257a578e273cae6458d4fd9602d75e8`; only LiveFederationTest here is live-server evidence, not the scripted ExchangeServicesTest.

## Reflect

Six rows gain narrowly scoped links; one remains unlinked. All 33 rows, source statements, applicability, reviewed-unit inventory and `coverage_complete: false` are retained. Every linked row retains meaningful remaining facets. No requirement is promoted to verified, no failures are excluded, and no production defect is inferred solely from an absent oracle. No server or persistent process was started for this increment; test-owned resources completed with their commands.
