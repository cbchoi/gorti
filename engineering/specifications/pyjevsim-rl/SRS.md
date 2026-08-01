# pyjevsim Reinforcement-Learning Framework
# Software Requirements Specification

Document ID: `SRS-PYJEVSIM-RL`  
Version: 0.1-draft  
Status: Design baseline candidate  
Updated: 2026-07-19  
Language: Korean (normative requirement keywords and identifiers are English)

## 1. 목적과 품질 방침

본 문서는 pyjevsim 모델을 강화학습 환경으로 실행하고, gorti를 분산
미들웨어로 사용하여 다수의 시뮬레이션 인스턴스와 actor/learner를 조정하는
프레임워크의 소프트웨어 요구사항을 정의한다. 현재 지원 기준은 단일 호스트
로컬 실행이며, 외부 계약을 유지한 채 gorti federation 중심의 다중 프로세스·다중
노드 운영으로 이전할 수 있어야 한다.

이 문서 세트는 ISO 9001의 고객 요구사항 명확화, 프로세스 접근, 위험 기반 사고,
검증 가능한 수용 기준, 문서화된 정보와 지속 개선 원칙에 **정렬**한다. 이는 ISO
9001 인증, 적합성 심사 통과 또는 제3자 보증을 주장하지 않는다.

요구사항의 `shall`은 필수, `should`는 권고를 뜻한다. 요구사항 수용에는 대응 설계,
구현, 검토 및 반복 가능한 시험 증거가 모두 필요하다.

## 2. 범위

### 2.1 포함 범위

- 기존 또는 신규 pyjevsim 모델의 plugin/factory 기반 등록과 격리된 인스턴스 생성
- Gymnasium과 호환되는 `reset`/`step` 중심 환경 및 명시적 다중 에이전트 확장
- pyjevsim `SysExecutor`의 이벤트 선택, 시간 전진, 전이 및 출력 의미 보존
- 로컬 순차·벡터·다중 프로세스 시뮬레이션
- gorti federation, 논리 시간, 동기화 지점, publish/subscribe, DDM, generation을
  이용한 분산 actor/learner 학습
- policy 배포, experience 수집, 배치 구성, checkpoint와 재현 가능한 평가
- 모든 정의된 milestone/task를 동일한 실행·증거 모델로 수행하는 개발 워크플로
- 제3자 모델 개발자가 자신의 모델·관측·행동·보상·종료·직렬화를 확장하는 SDK

### 2.2 제외 범위

- 특정 강화학습 알고리즘의 우월성 보장
- 현재 기준선에서의 프로덕션 다중 노드 장애조치 완료 주장
- DEXSim 또는 외부 RTI와의 wire-level 호환성
- ISO 9001 인증 또는 강화학습 결과의 안전성 인증

## 3. 이해관계자와 사용자 결과

| 이해관계자 | 필요한 결과 |
|---|---|
| 모델 개발자 | 기존 pyjevsim 모델을 최소한의 adapter/plugin으로 학습 가능 |
| RL 연구자 | 표준 환경 계약, 알고리즘 교체, 결정적 반복 및 평가 |
| 플랫폼 개발자 | 로컬/분산 실행을 같은 논리 계약으로 구현·시험 |
| 운영자 | federation 생명주기, 용량, 보안, 상태 및 장애 관측 |
| 품질 검토자 | 요구사항-설계-작업-시험-증거의 양방향 추적 |

## 4. 용어와 기준 의미

| 용어 | 의미 |
|---|---|
| environment | 에피소드 생명주기와 `reset`/`step` 계약을 제공하는 RL 경계 |
| simulation instance | 모델 factory가 생성한 상태 독립 pyjevsim 실행 단위 |
| actor | policy로 행동을 선택하고 experience를 생성하는 역할 |
| learner | experience로 policy 파라미터를 갱신하는 역할 |
| experience | 관측, 행동, 보상, 다음 관측, 종료 표지와 provenance의 레코드 |
| policy version | learner가 발행한 불변 policy 스냅샷 식별자 |
| generation | 같은 이름으로 다시 생성된 federation을 구분하는 gorti 세대 식별자 |
| logical time | gorti time management가 조정하는 federation 시간 |
| episode | reset 이후 terminated 또는 truncated까지의 step 연속 구간 |
| DDM | 관심 영역에 따라 전달 대상을 제한하는 Data Distribution Management |
| DEXSim | 비교 대상 분산 시뮬레이션/RL 아키텍처; 비교 결과에는 사용한 버전과 근거 필요 |

## 5. 운영 개념과 경계

### 5.1 pyjevsim 상호작용

모델 개발자는 모델 factory와 환경 binding을 등록한다. factory는 seed와 구성으로
새 모델 또는 구조 모델을 만들고, binding은 관측·행동·보상·종료를 pyjevsim 포트와
상태에 연결한다. 환경은 reset 때 새 실행 상태를 만들고 step 때 행동을 외부 입력으로
주입한 뒤 `SysExecutor`를 결정된 경계까지 진행하여 관측과 보상을 반환한다.

### 5.2 배포 진화

1. 로컬 기준선: 같은 프로세스 또는 worker 프로세스에서 다수 인스턴스를 실행한다.
2. 분산 기준선: actor, learner, coordinator가 gorti federation의 federate로 참여한다.
3. federation 중심 운영: 실행 생성, 역할 참가, 동기화, 시간 전진, data routing,
   generation fencing, 상태 관측을 federation lifecycle이 관리한다.

로컬 전송과 gorti 전송은 동일한 상위 수준 episode, experience, policy 계약을
구현해야 한다. 전송 교체가 사용자 모델 코드를 변경시키면 안 된다.

## 6. 기능 요구사항

### 6.1 모델 plugin과 factory

- **RL-FR-MDL-001** 프레임워크는 import 경로 또는 Python entry point로 모델 plugin을
  탐색하고, 이름과 semantic version으로 선택해야 한다.
- **RL-FR-MDL-002** plugin은 `ModelFactory`를 제공하여 구성, seed, instance ID로
  서로 상태를 공유하지 않는 pyjevsim 모델 인스턴스를 생성해야 한다.
- **RL-FR-MDL-003** plugin은 관측 공간, 행동 공간, reward/termination binding,
  모델·plugin 호환 버전을 실행 전에 검증 가능한 manifest로 선언해야 한다.
- **RL-FR-MDL-004** 프레임워크는 pyjevsim `BehaviorModel`, `StructuralModel` 및 명시적
  adapter를 통한 기존 coupled-model surface를 지원해야 한다.
- **RL-FR-MDL-005** 모델별 관측, 행동, reward, terminated, truncated 변환기는 plugin
  경계에서 교체 가능하고 프레임워크 내부 구현에 의존하지 않아야 한다.
- **RL-FR-MDL-006** plugin 생성·reset·step·close 실패는 instance, episode, step,
  plugin version을 포함하는 typed error로 격리되어야 한다.
- **RL-FR-MDL-007** 프레임워크는 제3자 개발자가 최소 예제, manifest schema,
  conformance test kit로 자신의 모델을 등록하고 학습하는 extension workflow를
  제공해야 한다.

### 6.2 Gym 환경 계약

- **RL-FR-GYM-001** 환경은 Gymnasium 호환 `reset(*, seed, options) ->
  (observation, info)` 계약을 제공해야 한다.
- **RL-FR-GYM-002** `reset`은 이전 episode 상태를 폐기하고 factory로 새 실행 상태를
  만들며 seed, episode ID, instance ID, model version을 `info` 또는 provenance로
  기록해야 한다.
- **RL-FR-GYM-003** 환경은 `step(action) -> (observation, reward, terminated,
  truncated, info)` 계약을 제공해야 한다.
- **RL-FR-GYM-004** 환경은 action을 적용하기 전에 선언된 행동 공간과 현재 episode
  상태에 대해 검증하고, 무효 행동을 안정된 오류로 거부해야 한다.
- **RL-FR-GYM-005** `step`은 정의된 simulation decision boundary까지 정확히 한 번
  진행하고 해당 경계의 observation/reward/termination 결과만 반환해야 한다.
- **RL-FR-GYM-006** terminated 또는 truncated 이후의 `step`은 자동 reset하지 않고
  안정된 오류를 반환해야 한다.
- **RL-FR-GYM-007** 단일 agent 계약은 기본값이어야 하며, 다중 agent plugin은 agent
  ID별 공간, 행동, 관측, 보상 및 종료를 명시적으로 선언해야 한다.

### 6.3 SysExecutor 의미

- **RL-FR-EXEC-001** adapter는 pyjevsim `SysExecutor` 또는 호환 executor를 모델 실행의
  권위 있는 이벤트 스케줄러로 사용해야 한다.
- **RL-FR-EXEC-002** 한 step 내에서 action 입력, 외부 전이, 동시 사건 선택,
  출력 함수, 내부 전이, 시간 갱신의 순서는 선택한 pyjevsim 버전의 의미와 일치해야 한다.
- **RL-FR-EXEC-003** 동시 사건의 bag 처리와 confluent 전이 순서는 선택한 pyjevsim
  `SysExecutor` 프로파일의 의미를 보존하고 기록해야 하며, 프레임워크의 비결정적 worker
  완료 순서로 대체하면 안 된다.
- **RL-FR-EXEC-004** decision boundary는 `next-event`, `fixed-delta`, `quiescence` 또는
  plugin 정의 경계 중 하나로 명시되어야 하고 모호한 wall-clock 경계를 사용하면 안 된다.
- **RL-FR-EXEC-005** confluent/external/internal 전이와 zero-time event cycle을 지원하고,
  설정 가능한 상한 초과 시 진단 가능한 truncation 또는 오류를 생성해야 한다.
- **RL-FR-EXEC-006** simulation time과 gorti logical time의 매핑, 단위, 정밀도,
  lookahead 및 반올림 정책은 실행 manifest에 고정되어야 한다.

### 6.4 로컬 병렬 실행

- **RL-FR-LOC-001** 프레임워크는 동일 환경 계약으로 순차, vector, 다중 프로세스
  local backend를 선택할 수 있어야 한다.
- **RL-FR-LOC-002** local backend는 구성된 worker 수와 environment 수에 따라 instance를
  배치하고 worker 장애의 영향을 해당 instance 집합으로 격리해야 한다.
- **RL-FR-LOC-003** 각 instance는 독립 seed stream, episode counter, model state,
  output buffer를 가져야 한다.
- **RL-FR-LOC-004** 병렬 결과 수집은 worker 완료 순서와 무관하게 `(run, instance,
  episode, step)` 순서 키로 정규화할 수 있어야 한다.
- **RL-FR-LOC-005** local backend는 bounded queue, backpressure, timeout, graceful
  cancellation 및 resource cleanup을 제공해야 한다.
- **RL-FR-LOC-006** 사용자는 local 실행 결과를 동일 seed와 구성으로 단일 instance에서
  재생하여 semantic projection을 비교할 수 있어야 한다.

### 6.5 gorti federation과 동기화

- **RL-FR-FED-001** distributed backend는 학습 run마다 이름과 generation으로 구분되는
  gorti federation execution을 생성하거나 명시적으로 참가해야 한다.
- **RL-FR-FED-002** coordinator, actor, learner, evaluator 역할은 고유 federate identity와
  capability metadata를 가지고 join/resign해야 한다.
- **RL-FR-FED-003** federation generation이 다른 message, callback, experience, policy,
  checkpoint는 stale로 거부되어야 한다.
- **RL-FR-FED-004** 초기화, model-ready, policy-ready, train-start, evaluation 및 shutdown
  barrier는 gorti synchronization point로 구현되어야 한다.
- **RL-FR-FED-005** timestep 또는 event-time 학습은 gorti time regulation/constrained
  서비스와 TAR/NER 계열 전진을 사용하여 TSO event가 grant보다 먼저 관측되게 해야 한다.
- **RL-FR-FED-006** actor는 승인된 logical-time window 밖의 state transition을 commit하면
  안 되며, lookahead 정책은 manifest와 일치해야 한다.
- **RL-FR-FED-007** observation, action, reward, policy metadata, control 및 health data는
  versioned FOM object/interaction으로 매핑되어야 한다.
- **RL-FR-FED-008** 대용량 tensor/checkpoint payload는 FOM에 content ID, schema, size,
  checksum, URI만 게시하는 외부 blob transport를 선택할 수 있어야 하며 무결성을
  확인하기 전에 소비하면 안 된다.
- **RL-FR-FED-009** DDM region은 environment shard, agent, spatial/semantic 관심 또는
  policy cohort별 subscription 범위를 제한할 수 있어야 하며 non-DDM 의미를 바꾸면 안 된다.
- **RL-FR-FED-010** late join, duplicate delivery, retry 및 federate 재시작은 idempotency key와
  run/generation/episode/step/policy 식별자를 이용해 중복 학습 적용을 막아야 한다.
- **RL-FR-FED-011** federation save/restore 또는 framework checkpoint는 model, optimizer,
  replay cursor, RNG, logical time, policy version, generation과 schema version을 일관된
  recovery boundary에 연결해야 한다.
- **RL-FR-FED-012** 현재 local backend에서도 distributed interface와 식별자를 사용할 수
  있어야 하며, 향후 federation 중심 coordinator로 이전할 때 model plugin API를 변경하면
  안 된다.
- **RL-FR-FED-013** federation 중심 운영 모드는 federation 생명주기를 run의 권위 상태로
  사용하고, 참가자 준비·진행·종료 상태를 callback과 MOM/관측 표면으로 판단해야 한다.

### 6.6 actor, learner, policy와 experience

- **RL-FR-RL-001** actor는 policy version을 원자적으로 설치하고 각 experience에 사용한
  policy version을 기록해야 한다.
- **RL-FR-RL-002** experience schema는 최소한 run/generation, actor, instance, episode,
  step, logical time, observation, action, reward, next observation, terminated, truncated,
  policy version 및 schema version을 포함해야 한다.
- **RL-FR-RL-003** experience transport는 at-least-once 전달에서도 learner가 idempotency
  key로 중복 적용을 검출할 수 있게 해야 한다.
- **RL-FR-RL-004** learner는 on-policy의 최대 policy lag 또는 off-policy의 허용 범위를
  manifest로 선언하고 범위 밖 experience를 거부·격리해야 한다.
- **RL-FR-RL-005** experience queue와 replay buffer는 bounded capacity, backpressure,
  sampling seed 및 durable cursor 옵션을 제공해야 한다.
- **RL-FR-RL-006** learner는 framework adapter를 통해 알고리즘 구현과 ML framework를
  교체할 수 있고 pyjevsim 또는 gorti 내부 API에 의존하지 않아야 한다.
- **RL-FR-RL-007** policy artifact는 immutable version, algorithm/model compatibility,
  checksum과 provenance를 포함하고 actor 설치 전 검증되어야 한다.
- **RL-FR-RL-008** evaluator는 학습 seed와 분리된 고정 평가 seed set을 사용하고 training
  experience 경로에 평가 transition을 혼입하면 안 된다.
- **RL-FR-RL-009** 중단 후 재개는 checkpoint와 manifest로부터 이미 적용된 experience와
  발행 policy version을 식별하여 이중 적용이나 version rollback을 막아야 한다.

### 6.7 실행, milestone/task 및 품질 워크플로

- **RL-FR-OPS-001** 사용자는 선언적 run manifest로 model plugin, backend, topology,
  seed, time mapping, RL adapter, resources, security 및 artifact 경로를 지정해야 한다.
- **RL-FR-OPS-002** manifest는 schema validation과 semantic preflight를 통과하기 전에는
  worker 또는 federation을 시작하면 안 된다.
- **RL-FR-OPS-003** runner는 `plan`, `run`, `status`, `cancel`, `resume`, `evaluate`,
  `replay`, `verify` 동작을 제공해야 한다.
- **RL-FR-OPS-004** 각 milestone과 task는 ID, 선행조건, 입력, 산출물, 요구사항, 시험,
  책임 역할, 상태, 승인 기준을 machine-readable traceability record로 표현해야 한다.
- **RL-FR-OPS-005** runner는 전체 milestone, 단일 milestone 또는 단일 task를 선택하여
  선행조건을 확인하고 실행하며 증거 위치와 결과를 기록해야 한다.
- **RL-FR-OPS-006** 구현 작업은 Plan, Do, Review, Reflect 단계를 순서대로 기록하고,
  Review 실패는 Do로, Reflect 개선 항목은 추적 가능한 새 task/change로 환류해야 한다.
- **RL-FR-OPS-007** 완료 상태는 요구사항-설계-인터페이스-task-test-evidence 링크와
  독립 review가 존재할 때만 부여되어야 한다.
- **RL-FR-OPS-008** 구성·FOM·plugin·dataset·policy·checkpoint·시험 결과는 run ID와
  content digest로 연계되고 보존/폐기 정책을 적용받아야 한다.

### 6.8 DEXSim 비교

- **RL-FR-DEX-001** 아키텍처 비교는 pyjevsim+gorti와 DEXSim의 model API, simulator
  ownership, time coordination, data distribution, parallelism, learning topology,
  fault model, reproducibility, deployment 및 extensibility를 동일 기준으로 분석해야 한다.
- **RL-FR-DEX-002** 비교 결과는 DEXSim의 식별 가능한 version/source/date, 사실과 추론의
  구분, 미확인 항목, 재현 절차를 기록하며 성능 우위는 동등 workload와 전달 완전성
  증거 없이는 주장하면 안 된다.

## 7. 비기능 요구사항

### 7.1 결정성과 재현성

- **RL-NFR-DET-001** 같은 code/plugin/FOM/config digest, seed set, 초기 checkpoint와
  message choreography는 같은 semantic trajectory digest를 생성해야 한다.
- **RL-NFR-DET-002** run은 Python, pyjevsim, gorti, RL framework, OS/architecture와 의존성
  버전 및 seed derivation을 기록해야 한다.
- **RL-NFR-DET-003** wall-clock timestamp, process ID, host ID와 비결정적 serialization
  순서는 semantic digest에서 제외되거나 정규화되어야 한다.

### 7.2 성능과 확장성

- **RL-NFR-PERF-001** 성능 증거는 environment-step throughput, simulation logical-time
  rate, policy latency, learner update rate, queue depth와 end-to-end experience latency를
  분리해 보고해야 한다.
- **RL-NFR-PERF-002** local/distributed scale 시험은 worker/actor 수에 따른 speedup,
  efficiency, saturation point와 delivery completeness를 함께 보고해야 한다.
- **RL-NFR-PERF-003** backpressure나 overload 시 silent loss 대신 측정 가능한 지연,
  거부 또는 정책에 따른 명시적 sampling을 사용해야 한다.

### 7.3 신뢰성과 복구

- **RL-NFR-REL-001** worker/federate/learner 장애는 run을 `degraded`, `recovering`,
  `failed` 중 하나로 전이시키고 원인과 영향 범위를 기록해야 한다.
- **RL-NFR-REL-002** checkpoint는 원자적 publish 또는 commit marker를 사용하여 부분
  artifact가 복구 후보로 선택되지 않게 해야 한다.
- **RL-NFR-REL-003** timeout, retry, heartbeat와 quorum 정책은 역할별 구성 가능하고
  무한 대기하지 않아야 한다.
- **RL-NFR-REL-004** 취소와 shutdown은 새 작업을 막고 in-flight 경계를 처리한 뒤
  pyjevsim resource, queue, federate, transport를 닫아야 한다.

### 7.4 보안과 데이터 보호

- **RL-NFR-SEC-001** networked gorti 배포는 TLS와 인증된 federate identity를 지원해야 한다.
- **RL-NFR-SEC-002** run 생성, plugin 등록, policy publish, checkpoint 접근과 취소는
  역할별 최소 권한으로 인가되어야 한다.
- **RL-NFR-SEC-003** plugin과 manifest 입력은 schema/path/size 검증을 거치고 임의 code
  실행 위험과 신뢰 경계를 운영 문서에 명시해야 한다.
- **RL-NFR-SEC-004** log, metric, trace, experience와 artifact에는 credential을 기록하면
  안 되며 민감 observation의 redaction 및 보존 정책을 지원해야 한다.
- **RL-NFR-SEC-005** artifact와 외부 blob은 checksum 및 provenance를 검증하고 위변조
  또는 호환성 실패 시 fail closed 해야 한다.

### 7.5 관측성과 운영성

- **RL-NFR-OBS-001** log/metric/trace는 run, federation, generation, role, federate,
  instance, episode, step, logical time, policy version과 correlation ID를 필요 범위에서
  연계해야 한다.
- **RL-NFR-OBS-002** 운영 상태는 participant readiness, time advance, queue pressure,
  policy lag, throughput, error와 checkpoint age를 노출해야 한다.
- **RL-NFR-OBS-003** health 상태와 학습 metric은 제어/모델 데이터와 별도 schema 및
  rate limit을 사용하여 시뮬레이션 의미를 방해하지 않아야 한다.

### 7.6 이식성, 확장성 및 품질

- **RL-NFR-PORT-001** local backend와 개발 SDK는 문서화된 Python 버전의 Linux,
  macOS, Windows에서 동작해야 한다.
- **RL-NFR-PORT-002** serialization은 명시적 endianness, dtype, shape, unit, schema
  version을 사용하고 언어/호스트 기본값에 의존하지 않아야 한다.
- **RL-NFR-EXT-001** model, environment binding, backend, RL algorithm, serializer,
  artifact store와 metric sink는 versioned public extension point를 가져야 한다.
- **RL-NFR-EXT-002** 호환성 정책은 deprecation 기간, migration guide와 contract test를
  제공하고 plugin API의 silent breaking change를 금지해야 한다.
- **RL-NFR-QUAL-001** SRS, SDD, IDD, STD와 traceability record는 version control되고
  변경 사유, 검토자, 승인 상태와 관련 위험을 기록해야 한다.
- **RL-NFR-QUAL-002** 각 요구사항은 고유 ID, 단일 해석 가능한 shall 문장, 수용 시험과
  owner를 가져야 한다.
- **RL-NFR-QUAL-003** 발견된 defect, risk, review finding과 사용자 feedback은 우선순위,
  처리 결정 및 검증 결과가 있는 change/task로 추적되어야 한다.
- **RL-NFR-QUAL-004** release 후보는 unit, contract, integration, distributed, fault,
  determinism, security 및 performance gate 중 해당 범위를 통과해야 한다.

## 8. 외부 인터페이스와 데이터 제약

상세 signature와 wire contract는 IDD가 정의한다. 다음 규칙은 모든 인터페이스에
적용한다.

- 공개 메시지와 artifact는 schema name/version 및 unknown-field 정책을 가진다.
- 배열은 dtype, shape, byte order, compression, unit과 maximum size를 명시한다.
- 모든 분산 message는 run/federation/generation/correlation identity를 가진다.
- step/experience/policy message는 중복 판정을 위한 안정된 idempotency key를 가진다.
- gorti FOM handle은 참가 후 해석하며, process 간 숫자 상수를 암묵적으로 공유하지 않는다.
- SDK 오류는 invalid state, validation, timeout, transport, stale generation,
  incompatibility, resource exhaustion과 user-code failure를 구분한다.

## 9. 수용 및 추적성

`traceability.yaml`은 요구사항에서 milestone/task/test로, 시험에서 요구사항으로의
양방향 관계를 정의한다. `MILESTONES.md`는 수행 순서와 Plan-Do-Review-Reflect gate를
정의한다. SDD, IDD, STD는 각각 설계, 인터페이스, 시험 절차의 상세 기준선이다.

요구사항은 다음을 모두 만족해야 수용된다.

1. traceability record에서 orphan이 아니다.
2. 구현과 문서 변경이 peer review되었다.
3. STD의 대응 시험이 반복 가능한 환경에서 통과했다.
4. run manifest, log, report 또는 artifact digest로 증거를 재현할 수 있다.
5. 미해결 deviation은 승인자, 범위, 위험, 종료 조건과 만료일을 가진다.

## 10. 알려진 위험과 설계 입력

| 위험 ID | 위험 | 필수 통제 입력 |
|---|---|---|
| RISK-RL-001 | pyjevsim 버전별 executor 의미 차이 | version adapter와 contract/conformance 시험 |
| RISK-RL-002 | worker 완료 순서가 trajectory를 변경 | stable identity ordering과 semantic digest |
| RISK-RL-003 | gorti logical time과 simulator time 불일치 | 단위/정밀도/lookahead manifest 및 경계 시험 |
| RISK-RL-004 | at-least-once 전달의 중복 학습 | idempotency key와 learner deduplication |
| RISK-RL-005 | 대형 tensor로 RTI 제어면 고갈 | metadata/blob 분리, size limit, DDM, backpressure |
| RISK-RL-006 | stale federation generation 오염 | generation fencing과 fail-closed validation |
| RISK-RL-007 | 제3자 plugin의 임의 code 실행 | 신뢰 경계, 격리 옵션, allowlist와 최소 권한 |
| RISK-RL-008 | DEXSim 비교의 버전/조건 편향 | 출처 고정, 동등 workload, 미확인 사항 공개 |

## 11. 변경 관리

요구사항 변경은 change ID, 제안 사유, 영향 받는 요구사항·설계·interface·task·test,
위험 평가와 승인 결정을 기록해야 한다. ID는 재사용하지 않는다. 폐기 요구사항은
삭제하지 않고 상태와 대체 ID를 남긴다. Reflect에서 나온 개선은 이 절차에 따라 다음
milestone 또는 명시적 corrective-action task로 편입한다.
