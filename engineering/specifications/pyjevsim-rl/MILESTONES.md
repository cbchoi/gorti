# pyjevsim RL Framework Milestones

Document ID: `PLAN-PYJEVSIM-RL`  
Version: 0.1-draft  
Status: Planning baseline candidate  
Updated: 2026-07-19

## 1. 수행 규칙

모든 milestone과 task는 `traceability.yaml`의 고유 ID로 실행한다. 각 milestone은
아래 Plan-Do-Review-Reflect(PDRR) gate를 순서대로 통과한다.

| 단계 | 필수 기록 | 종료 조건 |
|---|---|---|
| Plan | 범위, 요구사항, 위험, 담당, 입력, 시험, rollback | 승인된 task 목록과 acceptance gate |
| Do | 변경, 결정, 실행 log, artifact digest | task 산출물 생성 및 self-check |
| Review | peer review, 자동 시험, finding, deviation | blocking finding 0, 증거 링크 완결 |
| Reflect | 결과/예측 차이, 근본 원인, 개선, 후속 change | 개선 항목이 닫히거나 새 task로 추적 |

상태 흐름은 `planned -> ready -> doing -> review -> reflect -> done`이다. 선행조건,
요구사항, 시험 또는 증거가 없는 task는 `done`이 될 수 없다. 실패한 Review는 Do로,
새 범위가 발견된 Reflect는 Plan 또는 change control로 돌아간다.

## 2. 로드맵

### MS-RL-00 — 품질 기준선과 비교 근거

목표: 구현 전에 SRS/SDD/IDD/STD와 추적성, 위험, DEXSim 비교 방법을 승인한다.

| Task | 작업 | 주요 산출물 | Gate test |
|---|---|---|---|
| TASK-RL-001 | 요구사항 및 용어 기준선 | SRS | TEST-RL-DOC-001 |
| TASK-RL-002 | 아키텍처와 로컬→federation 진화 설계 | SDD | TEST-RL-DOC-002 |
| TASK-RL-003 | plugin/Gym/distributed interface 계약 | IDD | TEST-RL-DOC-003 |
| TASK-RL-004 | 시험 전략과 acceptance procedure | STD | TEST-RL-DOC-004 |
| TASK-RL-005 | 양방향 trace 및 품질 gate 자동 점검 | traceability + validator | TEST-RL-TRACE-001 |
| TASK-RL-006 | DEXSim 근거 수집과 동일 기준 비교 | versioned comparison report | TEST-RL-DEX-001 |

종료: 필수 문서가 peer review되고 모든 requirement/task/test가 orphan 없이 연결된다.

### MS-RL-01 — Model SDK와 단일 환경 vertical slice

목표: 제3자 pyjevsim 모델 하나를 plugin/factory로 로드하여 Gym 계약으로 실행한다.

| Task | 작업 | 주요 산출물 | Gate test |
|---|---|---|---|
| TASK-RL-010 | plugin manifest, discovery, version validation | model SDK | TEST-RL-MDL-001 |
| TASK-RL-011 | factory lifecycle과 typed error 격리 | model factory | TEST-RL-MDL-002 |
| TASK-RL-012 | observation/action/reward/termination binding | environment binding | TEST-RL-MDL-003 |
| TASK-RL-013 | Gym reset/step/close와 episode state machine | Gym-compatible env | TEST-RL-GYM-001 |
| TASK-RL-014 | 다중 agent extension 계약 | multi-agent adapter | TEST-RL-GYM-002 |
| TASK-RL-015 | 제3자 개발자 template와 conformance kit | example plugin + guide | TEST-RL-EXT-001 |

종료: clean checkout에서 예제 모델 등록, 학습 smoke, 오류 격리 및 API contract 시험이
통과한다.

### MS-RL-02 — SysExecutor 의미와 결정성

목표: RL step이 pyjevsim event semantics를 보존하고 동일 입력을 재현한다.

| Task | 작업 | 주요 산출물 | Gate test |
|---|---|---|---|
| TASK-RL-020 | pyjevsim version adapter와 executor driver | executor adapter | TEST-RL-EXEC-001 |
| TASK-RL-021 | decision boundary와 transition ordering | boundary strategies | TEST-RL-EXEC-002 |
| TASK-RL-022 | simultaneous/confluent/zero-time 처리 | deterministic selector | TEST-RL-EXEC-003 |
| TASK-RL-023 | seed derivation과 semantic trajectory digest | reproducibility module | TEST-RL-DET-001 |
| TASK-RL-024 | simulator↔logical-time mapping 계약 | time mapping module | TEST-RL-TIME-001 |

종료: 지원 pyjevsim profile별 golden trace와 10회 반복 digest가 일치한다.

### MS-RL-03 — 로컬 병렬 actor 학습

목표: local backend에서 다수 환경과 actor/learner가 bounded resource로 학습한다.

| Task | 작업 | 주요 산출물 | Gate test |
|---|---|---|---|
| TASK-RL-030 | sequential/vector/process backend | local executor | TEST-RL-LOC-001 |
| TASK-RL-031 | worker placement, isolation, restart | worker supervisor | TEST-RL-LOC-002 |
| TASK-RL-032 | bounded queue, backpressure, cancellation | flow control | TEST-RL-LOC-003 |
| TASK-RL-033 | experience schema, ordering, deduplication | experience pipeline | TEST-RL-EXP-001 |
| TASK-RL-034 | pluggable learner/policy publication | learner adapter | TEST-RL-POL-001 |
| TASK-RL-035 | checkpoint/resume와 evaluator 분리 | local recovery/eval | TEST-RL-REC-001 |
| TASK-RL-036 | local scale baseline | performance report | TEST-RL-PERF-001 |

종료: 단일 실행과 병렬 실행의 semantic projection이 일치하고 장애·과부하·resume
시험에서 silent loss 또는 중복 학습이 없다.

### MS-RL-04 — gorti federation vertical slice

목표: coordinator, actor, learner가 gorti federation에 참가해 한 episode와 policy
update를 논리 시간에 맞춰 완료한다.

| Task | 작업 | 주요 산출물 | Gate test |
|---|---|---|---|
| TASK-RL-040 | RL FOM과 role/federate lifecycle | versioned FOM | TEST-RL-FED-001 |
| TASK-RL-041 | synchronization point 기반 phase barrier | federation coordinator | TEST-RL-SYNC-001 |
| TASK-RL-042 | TAR/NER와 step/grant choreography | time coordinator | TEST-RL-TIME-002 |
| TASK-RL-043 | state/action/reward/experience/policy exchange | gorti transport | TEST-RL-FED-002 |
| TASK-RL-044 | generation fencing과 idempotency | message validator | TEST-RL-GEN-001 |
| TASK-RL-045 | local/distributed transport parity | transport contract suite | TEST-RL-FED-003 |

종료: cross-process end-to-end 학습, TSO-before-grant, generation 격리 및 transport
contract 시험이 통과한다.

### MS-RL-05 — DDM, 대용량 데이터와 분산 확장

목표: 환경 shard/agent/cohort별 data routing과 대형 artifact 교환을 확장한다.

| Task | 작업 | 주요 산출물 | Gate test |
|---|---|---|---|
| TASK-RL-050 | DDM dimension/region mapping | routing policy | TEST-RL-DDM-001 |
| TASK-RL-051 | tensor/checkpoint metadata+blob protocol | artifact transport | TEST-RL-BLOB-001 |
| TASK-RL-052 | distributed backpressure와 policy lag control | distributed flow control | TEST-RL-FLOW-001 |
| TASK-RL-053 | federation save/checkpoint consistency | recovery coordinator | TEST-RL-REC-002 |
| TASK-RL-054 | multi-actor/multi-learner scale study | scale report | TEST-RL-PERF-002 |

종료: DDM delivery set이 reference oracle과 일치하고, artifact 무결성과 scale evidence가
delivery completeness를 포함한다.

### MS-RL-06 — Federation 중심 운영

목표: federation lifecycle을 run의 권위 상태로 삼아 준비, 진행, 복구, 종료를 관리한다.

| Task | 작업 | 주요 산출물 | Gate test |
|---|---|---|---|
| TASK-RL-060 | 선언적 manifest와 preflight | manifest schema/validator | TEST-RL-OPS-001 |
| TASK-RL-061 | plan/run/status/cancel/resume/evaluate/replay/verify | runner/control API | TEST-RL-OPS-002 |
| TASK-RL-062 | MOM/callback 기반 상태 reconciliation | federation controller | TEST-RL-OPS-003 |
| TASK-RL-063 | heartbeat, timeout, retry, degraded/recovering state | resilience controller | TEST-RL-FAULT-001 |
| TASK-RL-064 | structured telemetry와 correlation | observability package | TEST-RL-OBS-001 |
| TASK-RL-065 | artifact retention과 provenance | artifact catalog | TEST-RL-ART-001 |

종료: participant crash, late join, cancel, stale callback 및 resume 시나리오가 정의된
상태 전이와 cleanup으로 종료된다.

### MS-RL-07 — 보안, 호환성 및 출시 준비

목표: 제3자 plugin과 network deployment의 신뢰 경계를 통제하고 지원 가능한 release를
만든다.

| Task | 작업 | 주요 산출물 | Gate test |
|---|---|---|---|
| TASK-RL-070 | TLS, identity, authorization integration | security configuration | TEST-RL-SEC-001 |
| TASK-RL-071 | plugin trust, input validation, secret redaction | plugin security controls | TEST-RL-SEC-002 |
| TASK-RL-072 | artifact integrity와 fail-closed policy | integrity verifier | TEST-RL-SEC-003 |
| TASK-RL-073 | public extension compatibility/deprecation | compatibility policy | TEST-RL-COMPAT-001 |
| TASK-RL-074 | Linux/macOS/Windows matrix | portability evidence | TEST-RL-PORT-001 |
| TASK-RL-075 | release gates, operations/runbook, known limits | release candidate | TEST-RL-REL-001 |

종료: security/compatibility/portability gate와 release checklist가 통과하고 알려진 제한이
공개된다.

### MS-RL-08 — 지속 검증과 개선

목표: 실제 모델 개발자의 onboarding과 운영 자료로 요구사항과 품질 체계를 개선한다.

| Task | 작업 | 주요 산출물 | Gate test |
|---|---|---|---|
| TASK-RL-080 | 독립 모델 개발자 onboarding trial | usability report | TEST-RL-USER-001 |
| TASK-RL-081 | DEXSim 비교 갱신과 동등 benchmark | comparison evidence | TEST-RL-DEX-002 |
| TASK-RL-082 | defect/risk/feedback trend review | CAPA/change backlog | TEST-RL-QUAL-001 |
| TASK-RL-083 | 모든 milestone/task replay audit | audit report | TEST-RL-TRACE-002 |

종료: 독립 개발자가 core 변경 없이 모델을 등록·학습·평가하고, audit가 문서/증거
추적의 완결성을 확인한다.

## 3. 전체 프로그램 acceptance

- 모든 milestone의 PDRR 기록과 gate test가 존재한다.
- `traceability.yaml` validator가 requirement/task/test orphan, 중복 ID, 알려지지 않은
  참조를 0건으로 보고한다.
- local 및 gorti distributed backend가 동일 plugin으로 end-to-end 학습·평가·재생된다.
- 결정성, 장애, 보안, 성능 시험 결과가 STD 수용 기준을 충족한다.
- 독립 모델 개발자 trial이 core framework 변경 없이 성공한다.
- DEXSim 비교는 고정된 출처와 동등 조건을 사용하며 미확인 주장을 표시한다.
