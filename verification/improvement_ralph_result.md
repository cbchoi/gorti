# improvement.md Ralph AI Loop 결과

실행일: 2026-07-12
대상: `improvement.md`
Loop: bounded Plan-Do-Review-Reflect, 최대 3회

## 최종 판정

**Accept - iteration 1, 8/8**

병렬 검토 입력은 Python SDK/gRPC 경로, Go `rtid`/TM/event-log 경로, Pitch 비교 방법론, 개선 우선순위와 acceptance gate의 네 영역으로 나눠 수행했다. 네 검토 모두 기존 수치를 production RTI core 비교로 쓰지 말고 P0 측정 계약을 먼저 만들어야 한다는 결론에 동의했다.

| 단계 | 점수 | 확인 결과 |
|---|---:|---|
| Plan | 2/2 | 측정 가능한 가설, production workload, 측정 계약, 위험, 롤백 존재 |
| Do | 2/2 | P0-P3 순서, 계층별 decomposition, raw/profile/decision 산출물 존재 |
| Review | 2/2 | 통계, delivery/drop accounting, semantic, resource gate 존재 |
| Reflect | 2/2 | Accept/Rework/Reject, 자동 거부, backlog 갱신 규칙 존재 |

## Review

계획은 stale baseline과 single-run 수치를 release gate로 사용하지 않으며, sent-only throughput과 hidden drop을 자동 거부한다. semantic parity, event-log determinism, callback ordering을 성능보다 우선하는 점도 적절하다.

남은 제한은 다음과 같다.

- P0 production benchmark와 raw sample schema는 아직 구현 전이다.
- Pitch parity threshold는 provisional이며 제품 책임자 승인이 필요하다.
- 현재 3-run logging A/B 결과는 우선순위 근거일 뿐 acceptance evidence가 아니다.
- bundled `rtid.exe`와 inspected source의 commit provenance를 다음 측정부터 기록해야 한다.

## Reflect

문서 자체는 실행 계획으로 승인한다. 다음 backlog의 첫 항목은 P0 baseline이며, P0 review gate를 통과하기 전에는 event log, outbox, TM 구조 변경의 성능 주장을 승인하지 않는다.

제품 변경별 Ralph loop는 동일한 0-2 점수 규칙을 다시 적용한다. semantic gate 실패, 재현 불가, p99 악화, synthetic-only 개선이 확인되면 즉시 Reject하고 feature flag 또는 기존 경로로 롤백한다.

Machine-readable 실행 기록: `verification/out/improvement-ralph/summary.json`

## P0/P1-A 실행 재검토

브랜치 `perf/improvement-p0`에서 production benchmark contract와 callback signal 변경을 적용한 뒤 Ralph loop를 다시 실행했다.

- Plan 2/2, Do 2/2, Review 2/2, Reflect 2/2, 총 8/8 Accept
- verification tests 56 passed
- Ruff 및 PowerShell parser 통과
- benchmark accounting: expected 6, delivered 6, rejected 0, dropped 0
- before/after semantic transcript SHA-256 byte-identical
- 3-run 방향성 probe에서 completed batch mean -78.39%, p95 -87.67%

Reflect 판정은 **P1-A Provisional Accept**다. 변경 효과와 semantic 보존은 확인했지만 5 warm-up + 20 measured run, randomized order, bootstrap 95% CI가 아직 없으므로 release-level Accept는 P0 multi-run gate 뒤로 보류한다.

재실행 machine-readable 기록: `verification/out/improvement-ralph-p0/summary.json`

후속 P0 baseline은 5 warm-up + 40 measured runs로 확대했다. expected/delivered `8000/8000`, dropped 0이며 주요 run-level median bootstrap CI 폭은 4.31-7.70%로 10% gate를 통과했다. gorti baseline은 Accept하고, P1-A change ratio는 before arm의 충분한 raw run이 없으므로 Provisional Accept를 유지한다.

Baseline analysis: `verification/out/p0-baseline-20/analysis-40.json`

## Async/grant-boundary rerun

The updated plan was evaluated again after the async transport and outbox
experiments.

- Iteration 1: Plan 2/2, Do 2/2, Review 2/2, Reflect 2/2
- Total: 8/8, **Accept**
- Accepted implementation: grant-boundary recipient flush
- Opt-in only: asynchronous TAR admission
- Continued work: ordered OM batch RPC and per-federation grant evaluator lock

The decision is backed by 10 balanced A/B pairs per transport experiment,
complete delivery accounting, byte-identical semantic transcripts, and exact
binary/workload provenance. Machine-readable result:
`verification/out/improvement-ralph-async/summary.json`.

## Native Go final rerun

The loop was rerun after the native Go SDK benchmark, 20-run analysis, Pitch
comparison, and ordered OM batch rejection were added to `improvement.md`.

- Iteration 1: Plan 2/2, Do 2/2, Review 2/2, Reflect 2/2
- Total: 8/8, **Accept**
- Accounting evidence: expected/delivered 4,000/4,000, rejected 0, dropped 0
- Performance decision: accept Pitch-class overall result; retain the
  interaction-send admission gap and scale testing as explicit follow-up
- Rejected experiment: ordered OM batch RPC, removed after a 9.1% completed
  delivery regression

Machine-readable result:
`verification/out/improvement-ralph-go-final/summary.json`.

## Claim-grade fair-comparison rerun

The loop was rerun after replacing the unmatched Pitch comparison with the
five-warm-up, twenty-measured-pair AB/BA session and after documenting the TAR
and synchronization race fixes.

- Iteration 1: Plan 2/2, Do 2/2, Review 2/2, Reflect 2/2
- Total: 8/8, **Accept** for the improvement document and decision process
- Semantic/accounting gate: pass, 4,000/4,000 deliveries per implementation
- Performance decision: completed-delivery parity accepted; interaction-send
  admission and inconsistent p99 tails remain **Rework**
- Withdrawn claim: the unmatched 73.8% completed-delivery advantage

Machine-readable result:
`verification/out/improvement-ralph-fair-final/summary.json`.

## Persistent interaction-stream rerun

The loop was rerun after the persistent interaction transport, safety review,
fair lifecycle correction, strict alternating schedule, provenance fixes, and
final v11 comparison were added to `improvement.md`.

- Document/process gate: Plan 2/2, Do 2/2, Review 2/2, Reflect 2/2
- Semantic/accounting gate: pass; 4,000/4,000 deliveries per implementation
- Accepted: synchronous persistent stream, conservative fallbacks, lifecycle
  and provenance corrections, O(1) publication check, shared parameter map
- Performance decision: completed delivery accepted within the provisional
  practical margin; interaction remains **Rework** at
  `1.290x (1.209..1.312)`, and TAR p95 remains **Rework** at
  `3.352x (2.486..4.514)`
- Prohibited claim: no uniform Pitch win and no tail-superiority claim

Machine-readable result:
`verification/out/improvement-ralph-interaction-final/summary.json`.

## 2026-07-13 current-state plan rerun

The loop was rerun after six independent analyses and three adversarial plan
reviews were incorporated into section 21 of `improvement.md`.

- Iteration 1: Plan 2/2, Do 2/2, Review 2/2, Reflect 2/2
- Total: 8/8, **Accept** for document completeness and decision discipline
- Current performance decision: **Rework**
- Co-primary failures: interaction median upper CI `1.312 > 1.25`; TAR p95
  upper CI `4.514 > 1.50`
- P0 before tuning: reliable partial-fanout/backpressure and stream ordering,
  complete resign/destroy cleanup, server-side FOM/generation validation,
  bounded authenticated streams, and Pitch process/log attestation
- Execution order: P0 correctness/provenance, stage attribution, interaction
  admission, TAR tail, lifecycle/scale, then a preregistered paired claim run
- Async/pipelined throughput remains a separate paired protocol and cannot be
  used to improve the synchronous caller-API claim

The Ralph acceptance is not a performance acceptance. It confirms that the
plan has measurable hypotheses, rollback rules, semantic/resource/statistical
gates, and an explicit Rework decision.

Machine-readable result:
`verification/out/improvement-ralph-v13-plan/summary.json`.

## 2026-07-13 Phase 0 correctness implementation rerun

The loop was rerun after implementing and adversarially reviewing the first
Phase 0 correctness slice documented in section 22 of `improvement.md`.

- Iteration 1: Plan 2/2, Do 2/2, Review 2/2, Reflect 2/2
- Total: 8/8, **Accept** for document completeness and decision discipline
- Focused Go packages: pass
- Python common/fair verification: 58 passed; Python transport: 7 passed
- Two-process gorti smoke: 5/5 persistent stream ACKs, zero fallback/reset,
  orderly dual resign, federation destroy, and a nonempty event log
- Implementation release decision: **Rework**
- Remaining blockers: atomic all-recipient fanout reservation,
  TSO-before-grant failure recovery, authoritative federation
  membership/generation validation, complete generation-scoped teardown,
  ACK-boundary stream drain, clean race/stress evidence, and Pitch process/log
  provenance

The evaluator's Accept result does not override the implementation Rework
decision. No new Pitch parity or performance claim was made by this correctness
iteration.

Machine-readable result:
`verification/out/improvement-ralph-p0-correctness/summary.json`.

## 2026-07-13 Phase 0 atomicity and lifecycle closure rerun

The loop was rerun after section 23 implemented and race-tested exact fanout
reservation, TSO evaluator reservation, operation-lease generation fencing,
complete runtime teardown, and real wire ACK drain.

- Document/process gate: Plan 2/2, Do 2/2, Review 2/2, Reflect 2/2
- Requested runtime correctness slice: **Accept**
- Full tests in seven changed Go packages under `-race`: pass
- Selected P0 race repetitions: 20/20; restart epoch/lifecycle: 10/10
- Python verification: 7 SDK transport + 36 common + 22 fair-comparison passed
- Wider Phase 0 release and Pitch performance claim: **Rework**

Machine-readable result:
`verification/out/improvement-ralph-p0-atomic-lifecycle/summary.json`.

## 2026-07-13 Phase 0 generation and provenance closure rerun

The loop was rerun after section 24 closed generation-token wire coverage,
generation-scoped event/save persistence, lifecycle publication gates, durable
cross-process restart epochs, and Pitch claim-grade provenance.

- Iteration 1: Plan 2/2, Do 2/2, Review 2/2, Reflect 2/2
- Total: 8/8, **Accept** for the requested correctness and provenance closure
- Explicit repository-wide Go run: all 57 discovered packages passed
- Final race run: federation, event log, savepoint, object, time, gRPC, Go SDK,
  and `rtid` passed together
- Verification: 30 fair contracts, 5 Pitch evidence tests, and 8 Ralph tests
  passed
- Python SDK broad run: 856 passed, 16 skipped, 6 known optional/scenario
  failures outside this generation/provenance change
- Pitch performance objective: **Rework**; isolated CRC startup did not open the
  configured port, so the verifier emitted no attested claim-grade result

The evaluator's Accept result does not establish Pitch parity. A performance
decision requires the preregistered five-warm-up plus twenty-measured AB/BA run
with complete process, JAR, command-line, FOM, event-log, and server-log
attestation.

Machine-readable result:
`verification/out/improvement-ralph-p0-provenance/summary.json`.

## 2026-07-13 persistent Pitch claim-grade comparison rerun

The loop was rerun after completing the persistent-session Pitch comparison
documented in section 25 of `improvement.md`.

- Iteration 1: Plan 2/2, Do 2/2, Review 2/2, Reflect 2/2
- Total: 8/8, **Accept** for the comparison process and decision discipline
- Schedule: 5 paired warm-ups and 20 measured AB/BA pairs (AB 10, BA 10)
- Workload: 100 operations per arm with identical FOM, seed, choreography,
  measurement boundaries, callback handling, and file logging
- Semantics and accounting: pass; gorti 4000/4000 and Pitch 4000/4000
  completed deliveries with no drops, rejects, duplicates, or invalid events
- Completed-delivery latency: **Accept**; paired gorti/Pitch ratios were 0.8709
  median, 0.1123 p95, and 0.4350 p99, with every 95% interval below 1.0
- Uniform performance objective: **Rework**; `sendInteraction` ratios were
  1.2182 median and 1.5311 p99, with both 95% intervals above 1.0

Machine-readable result:
`verification/out/improvement-ralph-pitch-v14/summary.json`.
