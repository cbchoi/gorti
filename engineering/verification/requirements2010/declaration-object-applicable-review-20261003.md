# Applicable declaration/object oracle review, 2026-10-03

## Plan
Review the 43 applicable DM/OM records lacking tests, inspect actual assertion bodies, and attach only bounded behavioral oracles. Use new uniquely named tests where reliable missing facets can be exercised. No production or existing-test edits, no Git operations, no whole-conformance verdict.

## Do
Source is S1516.1, supplied redline PDF SHA256 `789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4`.
Projection SHA256 `b80dc6d69a075f5c18c3e2b4b2b5b7741cba5fb3048082215d6ff72102033cbf` is a candidate exact-red exclusion, not a complete normative reading. New admission/history oracles use prose on PDF pages 84, 114-115, 117-118 and 121. No new visual-chart assessment or reviewed_units added.

Declaration/object has 92 rows, 88 applicable. Applicable-unlinked decreased **43 -> 3**, while applicable-linked increased **45 -> 85**. Forty rows received inspected bounded oracles; no new requirement IDs, no verified status, coverage_complete remains false. Exact test paths, literal declarations, assertions and remaining conditions are in each row and the external summary.json.

New test files:
- `rti/internal/hla/object/applicable_r1_declaration_review_test.go`: six declarations, real parsed FOM/declaration/object/ownership/delivery fixture.
- `rti/internal/hla/declaration/applicable_r1_registration_advice_test.go`: one declaration, real manager/queue active/passive and advice-switch edges.
- `rti/internal/hla/ddm/applicable_r1_time_independence_test.go`: live name controls plus separate DDM time/bounds test; opt-in real endpoint, not fake services.

## Review: retained failures
- **DM2010-IMPLICIT-DELETE-PRIVILEGE**, `TestApplicableR1ImplicitDeletePublicationLifetime`, S1516.1 5.1.2(f), PDF 84: initial explicit publication and restarted lifetime lack implicit privilege in PublishedObjectAttributes. This snapshot is actually used by registration; it is not an explicit-only alternate API. `declaration/manager.go` stores explicit sets without implicit/history tracking. `object/registry.go:484` independently appends privilege to initial ownership unconditionally; deletion ownership is not the conditional publication rule. Java forwards supplied publications unchanged. Explicit-unpublication/registration ownership consequences still need a live follow-up.
- **OM2010-RESERVE-SINGLE-INPUT**, `TestApplicableR1IllegalSingleReservationHasNoEffects`, 6.2 PDF 114-115: HLA-prefixed name is accepted and produces state/callback effects.
- **OM2010-RESERVE-MULTIPLE-INPUT**, `TestApplicableR1IllegalMultipleReservationIsAtomic`, 6.5 PDF 117-118: empty and mixed valid/HLA batches are accepted, violating admission/no-effects assertions.
- **OM2010-REGISTER-NAME-ADMISSION**, `TestApplicableR1ExplicitRegistrationRequiresReservation`, 6.8.3/6.8.5 PDF 121-122: an unreserved explicit name returns nil error and creates state. Historical `TestSpec_M26_RegisterWithoutReserve_AutoReserves` expects this behavior; that compatibility expectation does not weaken the standard-service obligation.

`TestApplicableR1LiveObjectNameAdmissionBoundaries` confirms these are not merely primitive fixture concerns. Against the pinned server, valid reservation/release controls pass before all four negative subtests: single-HLA, multiple-mixed-HLA, multiple-empty and unreserved-registration. Every invalid operation returns nil error; unreserved registration returns object handle 4. This raw product RPC run does not assert exact standard-Java exception mapping.

Owning production boundaries for main: `rti/internal/hla/object/reservation_handlers.go`, `reservation.go`, `registry.go`; gRPC `object.go` directly invokes them. Java `ExchangeServices.java` guards empty multi-name sets locally, so raw empty acceptance does not demonstrate Java empty-set acceptance. It does not guard HLA-prefixed members or successful prior reservation for nonempty explicit names. No production redesign begun.

Fresh passing new oracles cover additive per-attribute active/passive subscription state, exact single/multiple reservation outcome correlation and release/retry, and stop-registration only after final active matching interest with the switch enabled. They do not close unrelated row facets.

## Evidence
Root: `D:/workspace/gorti/applicable-requirements-20261003-r1/declaration/`.
Counts below are terminal test events; nested events are separate, never summed into unique top-level cases.

| Evidence | Top pass/fail/skip | Nested pass/fail/skip | Package pass/fail |
| --- | --- | --- | --- |
| new-oracles.jsonl | 2/5/0 | 2/2/0 | 0/2 |
| review-oracles.jsonl | 44/4/0 | 43/2/0 | 6/1 |
| live-name-boundaries.jsonl | 0/0/0 | 0/0/0 | 0/1 build failure |
| live-name-boundaries-r2.jsonl | 0/1/0 | 0/4/0 | 0/1 |
| passive-routing.jsonl | 1/0/0 | 0/0/0 | 1/0 |

First DDM red was a caller/region-owner mismatch, corrected without weakening the bounds/time oracle; see the separate ownership-DDM review. The first live-name attempt had a test response-field compile error; no behavioral tests executed in that attempt. Both first artifacts are retained, not overwritten or counted green.

Runtime `D:/workspace/gorti/full-consistency-20261003-r1/rtid.exe`, SHA256 `fc2bd04a5e64970e2dd0f6cb37f98741a257a578e273cae6458d4fd9602d75e8`. Go1.26.5 windows/amd64, count=1, p=2, offline caches; official XSD/Python configured for real DDM setup. Exact commands, hashes, configuration, source correspondence and artifacts are in external `summary.json`. This is selected-oracle execution, not all-package/full-standard execution. No Java/Python/race campaign here.

Owned servers 323276, 29244, 160080, 118992 stopped; all four absence-checked. No owned process remains.

## Reflect: genuine gaps
Still unlinked:
- **DM2010-MOM-SERVICE-REPORT-SUBSCRIPTION**: existing test enables reporting after subscription; it does not test a new subscription while reporting was already enabled.
- **OM2010-ORPHAN-OBJECTS**: unowned-attribute discovery tests do not establish a truly orphaned instance with no remaining owner.
- **OM2010-BEST-EFFORT-RATE-REDUCTION**: named-rate parsing/advice is not an actual delivered update interval/drop oracle.

Every newly linked row retains untested conditions: binding-level errors, lifecycle/checkpoint timing, complete sets/classes/hierarchies, exact callback metadata or other row-specific facets. Queue/stub fixtures are explicitly limited. Failing TDD obligation tests are intentionally not excluded from default Go selection, renamed away or weakened. Main owns any subsequent correction.

## Newly linked IDs
- `DM2010-FDD-HIERARCHY`
- `DM2010-IMPLICIT-DELETE-PRIVILEGE`
- `DM2010-OBJECT-CLASS-KNOWLEDGE`
- `DM2010-OBJECT-OWNERSHIP-ROUTING`
- `DM2010-INTERACTION-DECLARATION-STATE`
- `DM2010-INTERACTION-PROMOTION`
- `DM2010-PUBLISH-INTERACTION`
- `DM2010-UNPUBLISH-INTERACTION`
- `DM2010-SUBSCRIBE-OBJECT-ADDITIVE`
- `DM2010-SUBSCRIBE-OBJECT-DISCOVERY`
- `DM2010-SUBSCRIBE-INTERACTION`
- `DM2010-UNSUBSCRIBE-INTERACTION`
- `DM2010-STOP-REGISTRATION-CALLBACK`
- `OM2010-DISCOVERY-KNOWLEDGE`
- `OM2010-IN-OUT-SCOPE-REFLECTS`
- `OM2010-ATTRIBUTE-RELEVANCE-CONTROL`
- `OM2010-INTERACTION-RECEIVED-CLASS`
- `OM2010-AUTO-PROVIDE-SWITCH`
- `OM2010-RELIABLE-RATE-AND-ADVICE`
- `OM2010-RESERVE-SINGLE-INPUT`
- `OM2010-RESERVE-SINGLE-CALLBACK`
- `OM2010-RELEASE-SINGLE-NAME`
- `OM2010-RESERVE-MULTIPLE-INPUT`
- `OM2010-RESERVE-MULTIPLE-CALLBACK`
- `OM2010-REGISTER-NAME-ADMISSION`
- `OM2010-DISCOVER-CALLBACK`
- `OM2010-UPDATE-INPUT-ADMISSION`
- `OM2010-UPDATE-SUBSCRIPTION-WINDOW`
- `OM2010-REFLECT-OPTIONAL-METADATA`
- `OM2010-REFLECT-RECIPIENT-ADMISSION`
- `OM2010-SEND-INTERACTION-INPUTS`
- `OM2010-SEND-INTERACTION-DELIVERY-WINDOW`
- `OM2010-RECEIVE-INTERACTION-ADMISSION`
- `OM2010-REMOVE-ADMISSION-KNOWLEDGE`
- `OM2010-LOCAL-DELETE`
- `OM2010-OUT-OF-SCOPE-CALLBACK`
- `OM2010-REQUEST-UPDATE-TAG-TIME`
- `OM2010-PROVIDE-UPDATE-CALLBACK`
- `OM2010-UPDATES-ON-CALLBACK`
- `OM2010-UPDATES-OFF-CALLBACK`
