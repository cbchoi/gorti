# gorti 성능 개선 계획

작성일: 2026-07-12
근거: `probe_result.md`
상태: Ralph Plan-Do-Review-Reflect 검토 대상

## 1. 목표 (Objective)

semantic parity와 deterministic replay를 유지하면서 production-shaped gorti 경로의 지연, 처리량, CPU, allocation을 개선한다. 목표는 benchmark-only 숫자가 아니라 실제 delivered events 기준으로 검증한다.

우선순위는 다음과 같다.

1. P0: 비교 가능한 측정 계약과 production benchmark를 만든다.
2. P1: callback/TAR 직렬성과 불필요한 timer 대기를 제거한다.
3. P1: event log와 outbox의 verified hot path를 줄인다.
4. P2: TM과 declaration/OM을 federate 수에 따라 확장되게 한다.
5. P3: profile이 필요성을 입증한 경우에만 큰 구조 변경을 한다.

## 2. 실행 원칙 (Plan)

각 변경은 하나의 측정 가능한 가설, production workload, semantic invariant, 수치 목표, 위험, 롤백 방법을 가져야 한다. single-run claim, sent-only throughput, 누락된 drop accounting, synthetic harness만의 개선은 자동 거부한다.

성능 측정 계약:

- 동일 FOM bytes, payload bytes, random seed, process topology, callback model, logging mode를 사용한다.
- 양쪽에서 payload 준비와 timer 시작/종료 경계를 동일하게 한다.
- 5회 warm-up 후 최소 20 measured runs를 AB/BA로 randomize한다.
- raw per-operation samples와 run metadata를 보존한다.
- commit, binary SHA-256, Go/Python/Java/RTI version, build flags, CPU/power state, `GOMAXPROCS`, logging mode를 기록한다.
- median, p95, p99, bootstrapped 95% 신뢰구간, run-order effect를 보고한다.
- `delivered + explicitly_rejected + dropped == expected_fanout`을 만족하고 no-drop workload의 dropped는 0이어야 한다.

## 3. P0 - 신뢰 가능한 baseline

### 가설

현재 20-80배 비율의 상당 부분은 서로 다른 timing boundary와 lockstep choreography에서 발생한다. production-shaped decomposition을 만들면 RTI core, gRPC, Python facade, TM tax를 분리할 수 있다.

### Do

다음 probe를 독립 실행기로 만든다.

| Probe | 측정 내용 | 필수 arms |
|---|---|---|
| API admission | caller-side update/send latency | no subscriber, 1 subscriber; pre-encoded payload |
| Transport decomposition | core와 client stack 비용 | direct Go registry, Go bufconn/TCP gRPC, Python TCP gRPC |
| RO delivery RTT | delivery end-to-end | 별도 producer/consumer process, immediate ACK |
| TAR-only | 순수 TM 비용 | 2/5/25/100 federates, OM traffic 없음 |
| TSO tax | time management 추가 비용 | 동일 workload의 RO와 TSO 차이 |
| Logging factorial | persistence 비용 | nil/test-only, discard, file, explicit durable Sync |
| Open-loop throughput | transport capacity | bounded outstanding, epoch당 TAR 1회 |

기존 `rti/internal/perf`는 accepted send만 세고 gRPC/event log/TM을 우회하므로 release baseline에서 제외하거나 `synthetic-core`로 명시한다.

### Review gate

- 20 measured runs, ratio CI width 10% 이하, run-order effect 5% 이하
- raw samples, delivery/drop accounting, CPU, allocations/op, RSS, goroutine count 포함
- no missing/duplicate/out-of-order semantic event
- workload 2/5/25/100 결과 포함

### 위험 및 롤백

benchmark가 제품 코드에 special path를 만들 수 있다. production public API만 호출하고 benchmark-only fast path를 금지한다. 기준 미충족 시 기존 snapshot을 삭제하지 않고 `non-comparable historical snapshot`으로 유지한다.

## 4. P1-A - callback 및 TAR critical path

### 가설

두 ambassador의 순차 evoke와 순차 TAR를 제거하면 현재 closed-loop batch mean/p95의 큰 부분이 줄고 10 ms quantization이 사라진다.

### Do

1. 검증기의 callback 완료를 `threading.Event` 또는 `Condition`으로 신호한다.
2. polling을 유지해야 하면 producer evoke 후 predicate를 즉시 재확인하고 consumer callback이 이미 완료된 경우 두 번째 evoke를 생략한다.
3. 두 federate를 별도 process로 실행하거나 Layer 1 async client에서 paired TAR를 동시에 제출한다.
4. latency mode에서 outbox batch size 1 또는 grant boundary explicit flush를 feature flag로 실험한다.
5. `sustained_throughput` 명칭을 현재 시나리오에서는 `closed_loop_delivery_rate`로 변경한다.

### Review gate

- callback wait p95 50% 이상 감소
- 10 ms 단위 latency peak 제거
- 2-federate closed-loop delivery rate 25% 이상 개선
- callback ordering, TAR grant order, TSO release-before-grant semantics byte-identical
- small-federation secondary workload regression 5% 이하

### 위험 및 롤백

immediate callback과 evoked compatibility가 섞여 re-entry 또는 lost wake-up이 생길 수 있다. callback mode별 contract test를 먼저 추가하고 outbox mode는 feature flag로 유지한다. semantic gate 실패 시 flag 기본값을 기존 timer mode로 되돌린다.

## 5. P1-B - event log hot path

### 가설

중복 marshal/unmarshal, lock 안의 serialization, prefix/body 분리 write를 줄이면 write-ahead semantics를 바꾸지 않고 OM call latency와 tail을 낮출 수 있다.

### Do

1. production event record를 typed direct-proto path로 전달한다.
2. sequence를 한 번 할당하고 protobuf를 한 번만 marshal한다.
3. length prefix와 body를 하나의 frame으로 조합해 한 번 write한다.
4. per-federation writer lookup을 immutable/cache된 fast path로 만든다.
5. group commit은 별도 prototype으로 두고 `Append return before mutation` 계약과 explicit `Sync` durability를 보존할 수 있을 때만 채택한다.

### Review gate

- event append ns/op 30% 이상 개선
- append allocations/op 50% 이상 감소
- file-backed production workload delivered throughput 10% 이상 개선 또는 OM p95 15% 이상 개선
- event log bytes, sequence, replay result byte-identical
- short write, disk-full, append failure에서 기존 atomicity/error contract 유지

### 위험 및 롤백

pooled buffer aliasing, partial write 처리, sequence drift가 핵심 위험이다. direct path를 feature flag로 두고 기존 writer를 fallback으로 유지한다. event bytes가 달라지면 즉시 reject한다.

## 6. P1-C - outbox batching

### 가설

recipient별 sparse traffic마다 생성되는 1 ms timer를 shared flush scheduler 또는 reusable worker로 바꾸면 timer allocation과 callback p95가 줄어든다.

### Do

- current `batch=32, flush=1ms`, `batch=1`, shared scheduler를 동일 workload로 비교한다.
- internal ordered `SendMany`를 prototype해 outbox table lookup과 enqueue setup을 한 번 수행한다.
- server channel batch가 실제 gRPC batch/frame으로 이어지는지 별도 측정한다.

### Review gate

- timer/heap allocations 80% 이상 감소
- size 100 delivered throughput 25% 이상 개선 또는 sparse p95 20% 이상 개선
- size 2/5 regression 5% 이하
- per-recipient ordering, overflow/drop policy, slow-consumer bound 유지

### 위험 및 롤백

flush fairness와 slow consumer head-of-line blocking이 위험이다. queue depth, flush age, drop counters를 관측하고 기존 scheduler를 runtime flag로 남긴다.

## 7. P2-A - TAR/LBTS 확장성

### 가설

global state scan, regulator sort, grant-one-and-rescan을 federation-local ordered state로 바꾸면 lockstep workload의 `O(F^2)` 성향을 줄일 수 있다.

### Do

1. state와 pending request를 federation별 partition한다.
2. regulator logical-time/lookahead의 ordered snapshot 또는 incremental LBTS를 유지한다.
3. 한 fixed-point pass에서 모든 eligible grants를 deterministic handle order로 산출한다.
4. 2/5/25/100 federate TAR-only CPU, mutex, block, allocation profile을 비교한다.

### Review gate

- size 25/100 TAR ticks/s 30% 이상 개선
- state scan 또는 sort가 server CPU의 10% 미만
- size 2 regression 5% 이하
- TAR/TARA/NER/NMRA/FQR, resign, halt, retraction, save/restore tests 통과
- grant 및 event-log sequence byte-identical

### 위험 및 롤백

stale LBTS, federation cross-talk, grant order 변경이 위험하다. 기존 algorithm과 differential test를 수행하고 새 algorithm을 federation-level feature flag로 배포한다.

## 8. P2-B - declaration, OM, DDM allocation

### 가설

immutable sorted publication/subscription snapshot과 direct membership check를 cache하면 매 send의 lock, map iteration, sort, allocation이 줄어든다.

### Do

- declaration mutation 시 copy-on-write snapshot을 생성한다.
- read path는 allocation 없이 snapshot을 조회한다.
- interaction MOM class/name 판정을 handle 기반 cache로 바꾼다.
- DDM committed bounds를 immutable snapshot으로 유지해 query-time region map copy를 제거한다.

### Review gate

- declaration lookup allocations 50% 이상 감소
- core fanout CPU/op 20% 이상 감소
- size 25/100 production delivered throughput 10% 이상 개선
- DDM 25 federate/100 region query 2x 이상 개선
- subscription churn, wildcard/boundary, ownership, MOM semantics 유지

### 위험 및 롤백

stale snapshot과 memory retention이 위험이다. mutation/read differential test와 RSS gate를 추가한다. RSS 또는 goroutine이 10% 넘게 증가하면 rework한다.

## 9. P3 - 조건부 federation reactor

앞 단계 후에도 lock contention과 repeated manager crossing이 server CPU의 20% 이상이면 federation별 ordered ingress reactor를 prototype한다. logging, mutation, sequencing, fanout을 한 owner에서 처리하되 public call의 synchronous acknowledgement와 callback re-entry 안전성을 유지한다.

채택 조건은 size 25/100에서 30% 이상 개선, size 2/5 regression 5% 이하, event/callback sequence byte-identical이다. 조건을 충족하지 못하면 prototype을 폐기하고 작은 최적화로 돌아간다.

## 10. 공통 semantic gate (Review)

모든 변경은 다음을 통과해야 한다.

- Go race 및 전체 Go tests
- Python SDK tests와 verification tests
- C++ runtime tests와 cross-language encoding tests
- FM/OM/DM/TM service-usage gate
- TAR/TSO/retraction, ownership, DDM, save/restore, replay tests
- canonical semantic log byte equality
- deterministic event log bytes와 replay result equality
- zero missing, duplicate, unexpected ordering
- expected fanout 완전 accounting과 no-drop workload의 dropped=0

성능 변경은 semantic gate 하나라도 실패하면 성능 수치와 관계없이 reject한다.

## 11. 성능 합격 기준 (Review)

변경을 accept하려면 primary workload에서 다음 중 하나를 만족해야 한다.

- throughput 10% 이상 개선
- median 또는 p95 latency 10% 이상 개선
- CPU, allocations, RSS 중 하나 20% 이상 감소

동시에 bootstrap 95% 신뢰구간이 개선 방향을 지지하고, ratio CI width가 10% 이하이며, secondary workload regression은 5% 이하여야 한다. p99를 악화시키면서 mean만 개선한 변경은 reject한다.

Pitch parity provisional gate는 call median upper CI `<= 1.25x`, p95 upper CI `<= 1.50x`, throughput lower CI `>= 0.90x`다. 제품 책임자 승인 전에는 release gate가 아닌 목표값으로만 사용한다.

## 12. 실행 순서와 산출물 (Do)

| 단계 | 산출물 | 종료 조건 |
|---|---|---|
| 0 | production benchmark contract, raw schema, metadata | P0 review gate 통과 |
| 1 | callback/TAR probe와 최소 수정 | semantic gate + P1-A target |
| 2 | event-log direct frame prototype | log/replay equality + P1-B target |
| 3 | outbox scheduler/SendMany prototype | ordering/drop + P1-C target |
| 4 | TM federation-local prototype | TM differential + P2-A target |
| 5 | declaration/DDM snapshot | churn/DDM + P2-B target |
| 6 | 필요 시 reactor experiment | P3 conditional target |

각 단계는 before/after raw data, CPU/mutex/block profile, decision record, rollback flag를 남긴다. 다음 단계는 이전 단계의 profile이 우선순위를 재확인한 뒤 시작한다.

## 13. Reflect 판정 규칙

각 실험은 다음 중 하나로 종료한다.

- **Accept**: semantic gate와 수치 목표를 모두 통과한다. baseline과 backlog를 갱신한다.
- **Rework**: 가설 방향은 맞지만 CI, tail, secondary workload, resource gate가 부족하다. 원인과 다음 한 번의 실험을 명시한다.
- **Reject**: semantic 차이, hidden drop, sent-only 개선, synthetic-only 개선, 재현 불가, p99 악화가 있다. feature flag를 끄고 변경을 제거한다.

Reflect 기록에는 예상과 달랐던 결과, 남은 위험, 제거한 실험 코드, deferred work 재검토 trigger를 포함한다. 결론은 단순 측정 반복이 아니라 backlog 또는 운영 지침을 실제로 변경해야 한다.

## 14. 자동 거부 조건

- stale baseline 또는 binary provenance 없음
- single-run 성능 주장
- delivered events 대신 accepted/sent만 보고
- drop/reject/missing accounting 없음
- raw samples 또는 p95/p99 없음
- production 경로에서 재현되지 않는 benchmark-only 개선
- semantic/event-log/callback sequence 변경
- secondary workload 5% 초과 regression
- RSS/goroutine 10% 초과 증가

## 15. Ralph AI Loop 기록

이 문서는 `verification/ralph/improvement_loop.py`로 Plan-Do-Review-Reflect 검토한다. 점수는 단계별 0-2점, 총 8점이며 Plan과 Review가 모두 2점이고 총점 7점 이상이어야 approve한다. 실행 결과는 `verification/improvement_ralph_result.md`와 `verification/out/improvement-ralph/`에 기록한다.

## 16. 실행 진행 기록

### 2026-07-12 - `perf/improvement-p0`

**Plan**

- P0 production contract를 먼저 구현하고 그 측정 경로로 P1-A callback wait 변경을 평가한다.
- 기존 aggregate metric과 semantic transcript 형식은 유지한다.
- outbox, event log, TM 변경은 이번 변경에 섞지 않는다.

**Do**

- `verification/common/perf_contract.py`: raw nanosecond sample, provenance metadata, median/p95/p99, delivery accounting contract 추가
- `verification/common/provenance.py`: commit/branch/dirty, RTID/Python SHA-256와 version, host, exact argv, 시작/종료 시각과 outcome 기록
- `verification/gorti/run.ps1`: run마다 `run-metadata.json`과 `benchmark.json` 생성
- `verification/gorti/verifier.py`: aggregate와 함께 raw samples 기록, `expected_fanout == delivered + explicitly_rejected + dropped` 검증
- callback polling을 shared `threading.Event` wakeup으로 교체해 immediate callback을 기다리면서 ambassador event loop를 불필요하게 점유하지 않도록 변경

**Review**

count 3 smoke run에서 benchmark contract는 raw samples 52개, summaries 24개, expected 6, delivered 6, dropped 0으로 통과했다. count 100, file-backed event log 3회 before/after 방향성 probe 결과는 다음과 같다.

| 지표 | Before 중앙값 | After 중앙값 | 변화 |
|---|---:|---:|---:|
| Completed batch mean | 15.791 ms | 3.413 ms | -78.39% |
| Completed batch p95 | 32.918 ms | 4.059 ms | -87.67% |
| Update call mean | 4.375 ms | 0.631 ms | -85.57% |
| Interaction call mean | 2.778 ms | 0.531 ms | -80.90% |
| Closed-loop delivery rate | 31.337/s | 555.645/s | +1673.13% |

세 after run의 semantic transcript SHA-256은 모두 `c39b3a17bd970b9317d2d01e0c10fcebdbf82f016fc4a193b3891a3c5316bc6a`이며 before transcript와 byte-identical이다. 이 결과는 기존 evoke polling이 callback task와 같은 ambassador loop를 반복 점유해 API call latency까지 키웠음을 보여준다.

**Reflect**

- P1-A callback 변경은 목표치를 크게 넘었고 semantic transcript도 동일하므로 **Provisional Accept**다.
- gorti baseline은 5 warm-up 뒤 40 measured runs를 완료했다. expected/delivered는 `8000/8000`, rejected 0, dropped 0이다.
- run-level median의 bootstrap 95% CI 폭은 completed batch 4.57%, interaction delivery 4.31%, reflection delivery 4.78%, interaction call 7.05%, update call 7.70%로 모두 10% gate 안에 들었다.
- 40-run median은 completed batch 3.871 ms, interaction delivery 3.143 ms, reflection delivery 3.643 ms, interaction call 0.616 ms, update call 0.706 ms다.
- P0 gorti baseline과 multi-run analyzer는 완료됐다. Pitch provenance/raw integration과 balanced AB/BA order-effect 검증은 남아 있다.
- P1-A의 before arm은 3회 historical aggregate만 있어 20-run ratio CI를 계산할 수 없으므로 release-level Accept 대신 **Provisional Accept**를 유지한다.
- 다음 작업은 Pitch P0 integration이며, 그 뒤 outbox batch-size 1 실험을 별도 factor로 진행한다.

## 17. 2026-07-12 Async and grant-boundary iteration

### Plan

- Preserve the synchronous IEEE surface and add opt-in asynchronous OM/TAR
  admission with explicit ordering barriers.
- Align `completed_delivery_batch_latency` with Pitch by starting immediately
  before paired TAR, while retaining `send_to_delivery_batch_latency` as the
  producer-send-to-callback metric.
- Separate event-log, outbox batching, and TAR transport factors in provenance.

### Do

- Added bounded async OM submission, cancellation-safe observer Futures,
  flush-before-time-advance barriers, and an opt-in `timeAdvanceRequestAsync`.
- Added `threaded|async` TAR transport arms to the verifier and provenance.
- Added outbox batch/flush runtime flags and immediate recipient-batch flush on
  `TimeAdvanceGrant`, preserving all preceding TSO callbacks in order.
- Reduced event-log framing to one marshal-append and one sink write.

### Review

- Async TAR, 10 balanced pairs: TAR admission median `0.9693 -> 0.8561 ms`
  (-11.7%), completed delivery `1.3024 -> 1.2979 ms` (-0.35%), and
  send-to-delivery `2.2254 -> 2.2358 ms` (+0.47%). This is not a material
  end-to-end win; keep it opt-in.
- Grant-boundary flush, 10 balanced pairs at batch size 32: completed delivery
  median `2.6418 -> 1.4435 ms` (-45.4%) and send-to-delivery
  `3.6372 -> 2.4522 ms` (-32.6%). TAR admission rose `0.7778 -> 1.0792 ms`
  because delivery work is no longer deferred behind the timer.
- All 20 grant-flush runs produced the same semantic SHA-256
  `c39b3a17bd970b9317d2d01e0c10fcebdbf82f016fc4a193b3891a3c5316bc6a`,
  delivered 4,000/4,000 callbacks, and dropped zero.
- Discarding event-log files improved aligned completed delivery by about 8%
  and TAR by about 12%; persistence is not the dominant remaining gap.

### Reflect

- **Accept** grant-boundary flush: it exceeds the latency gate with identical
  semantics and no delivery loss.
- **Accept as opt-in** async TAR: useful for admission overlap, insufficient as
  the default performance fix.
- **Test then reject** the explicit ordered OM batch RPC when balanced local
  measurements show that serialized server execution loses to concurrent
  unary HTTP/2 calls.
- Add a per-federation grant evaluator lock before increasing TAR concurrency;
  concurrent evaluators can otherwise snapshot and emit the same pending grant.

### Follow-up experiments

- Direct callback delivery, 10 balanced pairs: completed delivery
  `1.3245 -> 1.2637 ms` (-4.6%) and send-to-delivery
  `2.2666 -> 2.1411 ms` (-5.5%), with 4,000/4,000 delivery and one semantic
  hash. **Keep opt-in** because the gain is below the 10% acceptance gate and
  slow callbacks directly backpressure the stream.
- Ordered update+interaction batch RPC, 10 balanced pairs: completed delivery
  `1.2973 -> 1.4160 ms` (+9.1%) and send-to-delivery
  `2.2299 -> 2.2569 ms` (+1.2%). Concurrent unary calls already overlap on
  HTTP/2, while the batch serialized the two server operations. **Reject and
  remove** the experiment despite semantic equality.
- Continue with a native Go SDK production benchmark to isolate RTID/server
capability from Python facade and callback scheduling cost.

## 18. Native Go closure iteration

> Superseded by section 19. The retained single-Pitch-run comparison below is
> historical evidence only and is not a fair performance claim.

### Plan

- Remove Python scheduling and synchronous-facade overhead from the RTI-core
  comparison by exercising the public Go federate SDK over real TCP gRPC.
- Preserve the same FOM, seed, HLA encodings, two-federate TSO choreography,
  delivery accounting, and payload/timestamp validation.
- Require one server binary, stable workload fingerprints, 20 measured runs,
  raw samples, run-level confidence intervals, and exact runtime provenance.

### Do

- Added the missing public Go SDK object declaration, registration, update,
  discovery, and reflection surface.
- Added `verification/gorti-go`, with producer and consumer event pumps,
  concurrent update/send and TAR calls, QPC-based Windows timing, complete
  delivery accounting, and the shared production benchmark schema.
- Kept endpoint, absolute paths, and exact RTID argv in provenance while
  excluding run-location fields from the workload fingerprint.
- Evaluated and removed an ordered OM batch RPC after it regressed completed
  delivery by 9.1% against concurrent HTTP/2 unary calls.

### Review

- 20/20 runs passed against RTID SHA-256
  `95f5f357642f8d120511a7d18feb39bfedd1661a26041fd61f39000c2fd5c955`.
- Accounting is expected/delivered `4000/4000`, rejected 0, dropped 0.
- Run-median results are completed delivery 0.1946 ms, update 0.1759 ms,
  send 0.1826 ms, and TAR 0.1736 ms.
- Against the retained Pitch Java 100-cycle medians, Go improves completed
  delivery by 73.8%, update by 36.5%, and TAR by 27.4%. Send admission is
  19.0% slower, but remains within the provisional `<= 1.25x` parity gate and
  has a lower observed p95.
- Focused Go tests, benchmark-contract analysis, delivery semantics, and
  provenance stability checks pass.

### Reflect

**Accept.** The user objective of Pitch-like or better performance is met for
the verified two-federate production path. The result supports the diagnosis
that the old gap was dominated by Python facade and serialized choreography,
not by Go as an implementation language. Keep direct callbacks and async TAR
opt-in, retain grant-boundary flushing, reject the OM batch RPC, and track the
remaining send-admission difference plus 5/25/100-federate scaling as follow-up
work rather than blocking this iteration.

## 19. Claim-grade paired comparison correction

### Plan

- Replace the unmatched retained-Pitch comparison with the same FOM bytes,
  seed, two independent federate processes, sequential update-send-TAR
  choreography, subscriber-pre-TAR boundary, immediate callbacks, and file
  server logging in both arms.
- Run five warm-up pairs and twenty measured pairs with exactly ten AB and ten
  BA orders, then calculate paired bootstrap confidence intervals.
- Fail closed on semantic projection differences, incomplete delivery,
  zero-duration samples, or callback/grant ordering violations.

### Do

- Added the Java Pitch and public-Go adapters, shared workload/result schemas,
  deterministic analyzer, and exact FM/DM/OM/TM semantic projection.
- Added missing public Go synchronization and object-name reservation surfaces,
  plus strict parsing of the exact Pitch FOM bytes.
- Replaced Windows wall-clock timing with QueryPerformanceCounter after the
  first full session exposed zero-duration Go samples.
- Fixed a startup race where a synchronization announcement could precede the
  participant snapshot.
- Restored strict ordinary-TAR behavior at `LBTS == requestedTime` and made the
  verifier reject a grant that overtakes either timestamp-equal callback.

### Review

- Final session: 5/5 warm-up pairs and 20/20 measured pairs passed; measured
  order was 10 AB and 10 BA.
- Pitch and Go each delivered 4,000/4,000 measured callbacks with zero reject,
  drop, duplicate, invalid callback, or zero timing sample.
- Both arms produced semantic projection SHA-256
  `0f148007c8c8e394c398ca05636afda672938a80f7c15b2ed9f25ac4a7da6c42`.
- Paired median Go/Pitch ratios (95% CI): completed delivery
  `0.973x (0.957..0.997)`, update `0.834x (0.818..0.867)`, TAR
  `0.958x (0.940..0.984)`, interaction send `1.612x (1.548..1.657)`.
- Median order-effect intervals include 1.0 for all four metrics. Completed
  delivery p99 is inconclusive at `0.944x (0.747..3.677)` and several call p99
  metrics remain worse for Go.

### Reflect

**Partial Accept / Rework.** Accept the semantic fixes, fair-comparison
contract, and Pitch-class completed-delivery median. Withdraw the earlier 73.8%
overall-win claim because it used unmatched evidence. The provisional per-call
parity gate is not met: interaction-send median is 1.612x Pitch, above the
1.25x target, and tail latency is not consistently better. The next optimization
must profile and reduce interaction-send admission without changing the paired
choreography; repeat this exact 5+20 AB/BA gate before claiming improvement.

Evidence:
`verification/out/fair-comparison/claim-file-1516-v5-strict-tar/analysis.json`.

## 20. Persistent interaction transport iteration

### Plan

- Align Go and Pitch RTI server lifecycles, strictly alternate AB/BA order, and
  retain the same synchronous caller-API timing boundary.
- Isolate SDK preparation, RTI business logic, file logging, and unary gRPC
  transport before selecting an optimization.
- Accept only with identical semantics, complete accounting, stable binary
  provenance, and a repeated five-warmup/twenty-measured-pair result.

### Do

- Added pre-resolved object/interaction handle APIs and prepared handle-keyed
  maps outside timed samples for both OM calls.
- Cached normal-versus-MOM interaction classification and added direct
  publication membership lookup.
- Added an internal persistent interaction stream with a pre-send capability
  handshake, synchronous server acknowledgement, cancellation/resign cleanup,
  conservative unary fallbacks, and RTID-only opt-in when OIDC is absent.
- Removed the receive goroutine/channel scheduler hop and shared one defensive
  parameter map between event-log serialization and callback fanout.
- Added the persistent comparison wrapper, strict alternating order, accurate
  dirty state, exact RTID/client hashes, and exact per-operation sample counts.

### Review

- Retained diagnostic probe: direct handler 0.18-0.84 us; unary TCP 182-258 us,
  10.9 KB/212 allocations; persistent stream 101-134 us, 2.0 KB/70 allocations.
  Evidence: `verification/out/fair-comparison/interaction-transport-probe.txt`.
- Final v12: all 50 arms valid, measured order 10 AB/10 BA, identical semantic
  SHA `0f148007c8c8e394c398ca05636afda672938a80f7c15b2ed9f25ac4a7da6c42`,
  and 4,000/4,000 deliveries per implementation with all error counters zero.
- Paired median ratios (95% CI): interaction `1.290x (1.209..1.312)`, update
  `1.105x (1.018..1.165)`, TAR `1.216x (1.142..1.299)`, delivery
  `0.954x (0.782..1.016)`.
- Interaction improved 14.9% in absolute Go latency and 19.9% in paired ratio
  versus v5. Every median order-effect CI includes 1.0.
- Interaction median fails the provisional upper-CI gate (`1.312 > 1.25`).
  TAR p95 also fails at `3.352x (2.486..4.514)`, above the 1.50 upper-CI gate.
- The Go server lifecycle is attested as persistent. Pitch uses one configured
  external endpoint, but its PID/start time/command line are not recorded.

### Reflect

**Partial Accept / Rework.** Accept the transport implementation, safety
fallbacks, persistent Go lifecycle alignment, provenance fixes, and the
completed-delivery result.
The user-visible goal is not fully closed under the documented provisional
gate: interaction is 1.290x Pitch with upper CI 1.312, and TAR p95 is 3.352x
with upper CI 4.514. Keep the implementation because it is a material,
semantics-preserving interaction improvement; retain interaction admission,
TAR tail latency, and persistent federation cleanup/scaling as explicit
rework. Do not claim a uniform Pitch win, statistical equivalence, or tail
superiority.

Evidence:
`verification/out/fair-comparison/claim-file-1516-v12-final/analysis.json`.

## 21. 2026-07-13 Current-state analysis and next performance plan

### Plan

The v12 result is a useful paired baseline, but it is not a completed Pitch
non-inferiority claim. The exact current signal is:

| Metric | Pitch run median | Go run median | Paired Go/Pitch (95% CI) | Gate |
|---|---:|---:|---:|---|
| interaction call median | 129.150 us | 162.575 us | 1.290x (1.209..1.312) | fail: upper CI > 1.25 |
| update call median | 224.850 us | 248.425 us | 1.105x (1.018..1.165) | pass |
| TAR call median | 198.900 us | 241.775 us | 1.216x (1.142..1.299) | fail: upper CI > 1.25 |
| TAR p95 | 354.950 us | 1,116.800 us | 3.352x (2.486..4.514) | fail: upper CI > 1.50 |
| completed delivery median | 757.775 us | 734.150 us | 0.954x (0.782..1.016) | no formal equivalence conclusion |

The semantic projection is identical, each implementation delivered
4,000/4,000 measured callbacks, and all reject/drop/duplicate/invalid counters
are zero. These facts validate this workload, but they do not prove full HLA
equivalence or statistical equivalence between products.

Six independent read-only reviews covered interaction transport, TAR, event
logging/outbox allocation, persistent lifecycle, fair-comparison statistics,
and safety. They changed the execution order. Correctness, lifecycle symmetry,
and evidence provenance are P0 work; only then should admission latency be
tuned. The primary performance endpoints are interaction median and TAR p95.

### Do

#### Phase 0 - close correctness and evidence blockers

1. Fix acknowledged reliable-delivery accounting. `fanoutReceive` must not
   ignore `Outbox.Send` failure or count a failed enqueue as delivered. For a
   reliable fanout, atomically reserve bounded capacity for every recipient
   before append; after append, enqueue to every reservation and acknowledge
   only after all recipients own the event. Release reservations if append
   fails. Timer flush may backpressure but must never discard a reliable batch.
   A post-append failure is indeterminate to the caller and must not invite an
   automatic retry. Inject failures at every reservation, recipient enqueue,
   timer flush, and stream-send point with batch sizes 1, 32, and 1024.
2. Preserve one receive-order sequence per federate. Assign a monotonic SDK
   sequence at the call linearization point and route all calls through one
   per-federate sequencer across stream use, fallback transitions,
   cancellation, and resign. Fallback is allowed only before request bytes are
   transmitted; a post-send failure returns an indeterminate result and is
   never retried automatically. Test event-log, RO callback, and equal-time TSO
   order against the assigned sequence.
3. Make handle validation authoritative on the server. Validate federation
   generation, class existence, parameter membership/inheritance,
   publication, and exact HLA exception precedence. Join attests the canonical
   FOM digest and generation before a fast path is usable. Cover invalid,
   stale, zero, cross-class, recreated-federation, and mismatched-FOM handles,
   with identical unary/stream errors.
4. Implement serialized federation teardown before exposing it to the fair
   verifier. First block new joins, then perform action-aware resign cleanup,
   clean FOM-dependent declaration, object, ownership, synchronization, DDM,
   time, save/restore, MOM, cluster, stream/outbox, timer, and reservation
   state, close the per-generation event-log writer last, and finally remove
   the empty roster. Internal cleanup is idempotent, while a second public HLA
   destroy retains its specified error. Fix join/destroy serialization,
   replace handle-range probing with registry enumeration, and use federation
   generation IDs so stale callbacks or streams cannot affect a recreated
   name. Distinguish all resign actions from destroy semantics.
5. After Phase 0 teardown tests pass, expose the existing federation destroy
   RPC through the public Go SDK. Pitch destroys each federation while the Go
   verifier currently only resigns; invoke destroy after both Go federates
   resign and record teardown attestation. Version the canonical projection
   before adding a cross-product semantic event.
6. Bind streams to an authenticated federate identity and bound their resource
   use. Add token-expiry/max-lifetime handling, per-connection and per-federate
   stream quotas, idle timeout, request byte/map-entry limits, bounded backlog
   and rate, forged-handle tests, and idle-stream exhaustion tests.
7. Own or attest the Pitch RTIexec. Record PID, start time, command line, JVM
   path/hash, loaded RTI JAR path/hash/version, settings, endpoint, and stop
   time. Require the same PID across the session and copy newly written,
   nonempty Pitch event logs into the result bundle. Otherwise label the arm
   `external endpoint, identity/logging unverified`; that endpoint-scoped grade
   may guide engineering but cannot support a product-level Pitch claim.
8. Prebuild and hash all binaries before AB/BA execution. Seal a SHA-256/size
   inventory, dirty patch or clean commit, CPU model, power plan, Go/JVM
   versions, `GOMAXPROCS`, flags, FOM, workload, and analyzer. No compilation
   is allowed inside measured scheduling. Require exactly one newly created,
   nonempty, hashed server log per federation in both implementations.

Phase 0 exit gates:

- zero acknowledged reliable loss, partial-fanout ambiguity, duplicate, timer
  flush drop, or order violation under stress;
- exact unary/stream negative-case error parity and a clean Go race run;
- zero live manager entries, streams, timers, outboxes, or open federation log
  writers after destroy;
- cover 1,000 create/join/resign/destroy cycles and 1,000 join/resign cycles in
  one live federation, every resign action, high handle watermarks with holes,
  disconnect without resign, force-resign, joined-destroy rejection, failed
  append/cleanup, admin destroy, concurrent join/send/resign/destroy, duplicate
  destroy, and same-name recreation;
- after a fixed warmup, sample post-GC heap, goroutines, and platform OS handles
  at preregistered intervals and fit a regression slope with a 95% CI. Require
  target-federation manager cardinalities and open handles to be exactly zero,
  no positive goroutine/handle slope, a bounded heap/RSS plateau, a declared
  bytes-per-cycle evidence budget, median last/first quarter ratio <= 1.05, and
  p95 ratio <= 1.10;
- full product claims require actual Pitch process and per-federation log
  evidence; endpoint-scoped runs remain explicitly qualified.

#### Phase 1 - add sampled stage attribution without changing behavior

Add an opt-in diagnostic stage trace, disabled by default and excluded from
claim timing. Use one monotonic clock per process and correlation IDs. Define
non-overlapping spans and residuals inside each process; never subtract raw
timestamps from different process clocks. Limit trace overhead to 2% in a
paired trace-on/off control.

Interaction spans:

- client stream lock wait, cancellation registration, `SendMsg`, and `RecvMsg`;
- server `RecvMsg`, handle/FOM validation, publication lookup, event-log writer
  lookup/lock/marshal/write, subscriber snapshot, TSO buffer/outbox enqueue,
  handler completion, and ACK `SendMsg`;
- stream attempts, successes, unary fallbacks, resets, active streams, queue
  depth, and allocations.

TAR spans:

- unary transport, request-state lock, evaluator mutex wait/hold, regulator and
  candidate snapshot construction, each fixed-point pass, grant count;
- event-log append, TSO release/partition, timer arm/stop/fire, outbox lock,
  grant-boundary flush, response serialization, GC and scheduler pauses;
- role, logical time, candidate count, released-event count, and queue depth.

Run the unchanged two-process choreography with diagnostic-only factorials:
file/discard logging, outbox batch 1/32, default/raised `GOGC`, unary/persistent
interaction, named/pre-resolved handles, and early/late persistent-session
blocks. File logging remains mandatory for the primary claim.

Phase 1 exit gate: the sum of non-overlapping spans plus an explicit residual
accounts for each process-local interval, and measured causes classify at least
70% of TAR samples above 1 ms. Preregister a screening or fractional-factorial
design rather than running every factor combination. Do not choose a production
optimization from CPU intuition alone.

#### Phase 2 - remove interaction admission cost

Apply candidates one at a time in measured contribution order:

1. Remove avoidable persistent-stream per-message objects and scheduler work:
   share an error-only server helper, benchmark safe request/ACK reuse, and
   replace per-call cancellation machinery only when cancellation semantics are
   unchanged. Preserve synchronous acknowledgement after handler completion.
2. Retain exactly one deep defensive parameter copy, including each payload
   byte slice, then share the immutable owned wire map between event-log
   serialization and fanout. Add mutation and race tests. Cache immutable sorted
   subscriber snapshots on declaration mutation when profiling shows the
   current allocation/sort above 5% of the call.
3. If event logging is material, use an atomic read-mostly writer lookup and a
   writer-owned reusable frame buffer under the existing ordering lock. Keep
   one synchronous file write and the rule that append failure prevents
   mutation, fanout, and successful acknowledgement.

Candidate microgates compare the candidate with the frozen v12 Go baseline by
paired run-level ratios: interaction median improves at least 5% or allocations
fall at least 25%; no p95 regression above 10%; update and completed delivery
median upper-CI regression is no more than 5%. Treat p99 as a safety diagnostic
until each run has enough iterations for a preregistered tail estimator. Target
a Go interaction median <= 152 us so the non-inferiority upper CI has headroom
below 1.25x rather than merely touching it.

#### Phase 3 - remove TAR tail latency

Proceed only from Phase 1 evidence:

1. First test an ordered `SendMany`/grant-boundary operation that appends the
   eligible TSO callbacks and grant under one recipient lock and flushes once,
   avoiding a timer that is immediately armed and stopped.
2. Partition time state by federation, retain deterministic handle order, and
   reuse evaluator/candidate scratch storage. Initially preserve the existing
   fixed-point decisions and synchronous errors exactly.
3. Reduce global rescans by evaluating all currently eligible grants in one
   deterministic pass only after differential TAR/TARA/NER/NMRA/FQR tests
   prove identical decisions and event order.
4. Consider a grant worker/reactor or persistent TAR stream only if stage
   traces show that the safer changes cannot meet the gate. These are separate,
   feature-flagged experiments because acknowledgement, cancellation, and
   synchronous-error semantics are higher risk.

Candidate microgates compare with the frozen v12 Go baseline: TAR p95 improves
at least 25%, median and completed-delivery median upper-CI regression is no
more than 5%, and p99 shows no severe safety regression. The claim target is
TAR p95 <= about 532 us at the current Pitch median p95, with paired
non-inferiority upper CI <= 1.50x.

#### Phase 4 - scale and throughput after correctness

Run gorti lifecycle/scale cells for persistent cycles 1/25/100/1,000; active
federations 1/10/100;
federates per federation 2/8/32/128; subscribers 1/7/31/127; and objects
1/100/10,000. Record CPU, allocations/op, RSS, heap, goroutines, OS handles,
manager cardinalities, queue depth, drops/backpressure, median, p95, and p99.

Run identical Pitch cells only where the license and supported capacity permit;
otherwise label the matrix gorti-only and do not make a cross-product claim.

Async or pipelined calls use a separate paired delivered-throughput protocol:
same outstanding window, payloads, delivery acknowledgement, error policy,
logging, process topology, independent warmup, fixed duration/count, ABBA/BAAB
blocks, and delivered-events-per-second estimator with a block-bootstrap CI.
Do not use this result to claim improvement in the synchronous API metric.

#### Phase 5 - final paired claim run

Use an independent, discarded pilot to freeze an even pair count, per-arm
iteration count, and the two co-primary endpoints before the final run. Keep
the same FOM bytes, seed 1516, two independent federate processes,
sequential update-send-TAR choreography, subscriber-pre-TAR delivery boundary,
immediate callbacks, file server logging, five server warm-up pairs, and an
unmeasured steady-state prelude inside every fresh client arm. Normalize the
publisher/subscriber launch delay. Use two-pair/four-arm blocks randomized from
a recorded seed as ABBA or BAAB and bootstrap complete blocks; add
implementation, slot/order, block, and elapsed-pair trend analysis.
Require the entire practical order-effect CI inside 0.95..1.05 rather than only
requiring it to contain 1.0. Define the order estimator on the log-ratio scale
and power the fixed run for <= 10% relative primary-endpoint CI width and the
order margin. Do not reuse pilot data or stop early. Increase per-arm iterations
substantially for tail inference; otherwise p99 remains diagnostic. Apply an
intersection-union decision: both co-primary gates must pass.

### Review

A change can advance only when all relevant gates pass:

- semantic: identical cross-product canonical projection and grant sequence,
  within-implementation pre/post event-log bytes and replay state where
  supported, callback-before-grant invariants, and zero reject/drop/duplicate/
  invalid events;
- correctness: reliable backpressure, exact FOM/handle errors, race,
  cancellation, restart/GOAWAY, old/new client-server, mTLS/OIDC, and lifecycle
  stress tests pass, with zero unauthorized/unbounded stream cases and exact
  actual-path/fallback attestation;
- non-inferiority versus the same-session Pitch arm: interaction and TAR median
  paired upper CI <= 1.25x and interaction/TAR p95 upper CI <= 1.50x;
- regression versus frozen v12 Go: update and completed-delivery median upper
  CI <= 1.05x; p99 is diagnostic unless the powered tail protocol says
  otherwise;
- separate delivered-throughput protocol: lower CI >= 0.90x Pitch;
- resources versus frozen v12 Go after the same warmup: upper confidence bound
  on steady-state RSS/heap/goroutine/handle change <= 10%, no positive leak
  slope, and exact zero target-federation manager cardinality after destroy;
- provenance: exact process identities, binaries, settings, logs, workload,
  schedule, raw samples, and analysis hashes are sealed and reproducible.

Passing a point estimate is insufficient. Completed delivery has a point ratio
below 1.0, but its CI crosses 1.0 and does not prove equivalence. A clean
four-record projection proves only the declared observation, not all HLA
semantics.

### Reflect

**Rework.** Keep the v12 persistent transport as evidence of a material
interaction improvement, but do not declare it default-on or claim uniform
Pitch non-inferiority yet. Three P0 findings now precede further tuning: reliable
outbox/backpressure and stream-order correctness, complete federation cleanup
with symmetric destroy, and Pitch process/log attestation.

After those close, optimize interaction transport/event logging and TAR
evaluator/outbox tails as separate workstreams. Async admission, group commit,
callback wire batching, and a federation reactor remain conditional experiments
behind feature flags; reject them if they change synchronous acknowledgement,
write-ahead durability, FIFO/TSO-before-grant order, error reporting, or replay.

The persistent interaction path has independent client and server modes
`off | observe | on`, defaulting to `off` until release gates pass. `observe`
may attest capability and stages but must not process the interaction twice.
Forced disable stops new streams, drains existing streams at acknowledged
boundaries, and uses a circuit breaker so only future pre-send calls fall back
to unary. Always-on low-cost counters record active streams, resets, fallback
reasons, indeterminate sends, queue age, and reliable reservation backlog. A canary
soak must demonstrate both normal operation and flag rollback before default-on.

Evidence:
`verification/out/fair-comparison/claim-file-1516-v12-final/analysis.json`,
`verification/out/fair-comparison/claim-file-1516-v12-final/manifest.json`, and
`verification/out/fair-comparison/interaction-transport-probe.txt`.

## 22. 2026-07-13 Phase 0 correctness implementation loop

### Plan

This bounded Ralph iteration implemented the first Phase 0 slice from section
21. The slice covered ordered interaction transport, server-side interaction
handle validation, explicit fanout/outbox failure propagation, federation
lifecycle cleanup, and symmetric teardown in the fair Go verifier. It did not
claim to complete the whole Phase 0 exit gate.

The acceptance boundary for this iteration was:

- no unary overtaking while a persistent interaction send owns the per-federate
  ordering point;
- successful stream ACK is the commit result even if caller cancellation races
  immediately afterward;
- resign interrupts both stream and unary fallback calls;
- invalid interaction class and parameter handles fail before publication,
  event logging, or ordinary fanout;
- synchronous outbox failures reach the caller and timer retry does not discard
  an accepted batch;
- send, timer flush, cancel, and federation unbind cannot reorder restored
  scratch data or send to a closed channel;
- the fair Go verifier resigns both independent processes, destroys the empty
  federation, and records the actual interaction transport counters.

### Do

Implemented work:

1. Serialized persistent-stream selection and unary fallback under the same
   federate lock. The fallback RPC now inherits the federate lifecycle context
   and disables transparent gRPC replay buffering. Transport counters record
   stream/unary ACKs, opens, resets, fallback reasons, and indeterminate sends.
2. Added server validation and HLA/gRPC exception mappings for unknown
   interaction classes and parameters, plus deep payload ownership before the
   immutable event is shared by logging and fanout.
3. Propagated fanout and outbox errors instead of acknowledging a failed
   enqueue. A missing recipient binding now has an explicit unavailable error.
4. Kept accepted outbox scratch data attached to its recipient while retrying.
   Enqueue and close are serialized by the recipient lock, stale timers are
   identified by timer instance, and unbind closes active recipient channels.
5. Added federation cleanup hooks for declaration, object, time, FOM, MOM,
   cluster, outbox, and event-log state, and exposed federation destroy through
   the public Go SDK.
6. Updated `verification/gorti-go-fair` to perform orderly subscriber resign,
   publisher resign, federation destroy, disconnect, and exact persistent-path
   attestation.

### Review

Focused Go verification passed for the federate SDK, object registry, gRPC
transport, federation, declaration, time, event log, `rtid`, and the fair Go
verifier. Python verification previously passed 58 common/fair tests and 7
transport tests in this same iteration. `git diff --check` passed. A real
two-process smoke run with seed 1516 and five interactions produced one
nonempty federation event log and reported exactly 5 stream sends, 5 stream
ACKs, one successful stream open, and zero unary sends, fallbacks, resets, or
indeterminate results. Both processes resigned and the publisher destroyed the
federation.

The broad Go run had environment-qualified gaps: the race build could not run
because this Windows Go installation has no C compiler, and one dependency
cache path was unavailable under the sandbox. An isolated admin status test
passed after a parallel-suite lazy-dial timing failure.

Adversarial review found remaining Phase 0 release blockers:

- reliable fanout still lacks atomic all-recipient capacity reservation, so a
  middle-recipient failure can leave a delivered prefix and an indeterminate
  write-ahead record;
- buffered TSO release still needs failed events requeued and the grant withheld
  until every eligible TSO callback is owned by the recipient;
- membership and federation generation are not yet authoritatively validated,
  the legacy missing-FOM compatibility path is fail-open, parameter inheritance
  is incomplete, and MOM dispatch must use the same validation precedence;
- destroy cleanup is not yet transactional, generation-scoped, or complete for
  ownership, synchronization, DDM, save/restore, active streams, and high sparse
  handles; join/resign/destroy lock ordering needs a dedicated deadlock proof;
- stream shutdown cancels an active request rather than draining at an ACK
  boundary, and indeterminate classification still needs exact pre-send versus
  post-send evidence and mixed concurrency tests;
- Pitch process identity and per-federation server-log provenance remain
  unverified, and no new 5-warm-up plus 20-measured AB/BA performance claim was
  made in this correctness iteration.

### Reflect

**Rework.** Retain this implementation as a tested Phase 0 foundation, but do
not enable the persistent interaction path as a release default and do not make
a new Pitch parity claim. The next Ralph iteration starts with atomic reliable
fanout reservation and TSO-before-grant recovery, then federation generation
and membership validation, complete generation-scoped teardown, stream drain
semantics, and the clean race/lifecycle stress gates. Performance tuning resumes
only after these correctness gates pass.

## 23. 2026-07-13 Phase 0 atomicity and lifecycle closure loop

### Plan

This Ralph iteration closes the six runtime correctness gates requested after
section 22: exact all-recipient fanout admission, TSO failure-before-grant,
federation generation validation, composed teardown, ACK-boundary interaction
drain, and clean race evidence. The acceptance boundary is observable rather
than implementation-shaped:

- a failed mixed immediate/TSO fanout owns no recipient event and creates no
  WAL record;
- every event that forces a short outbox batch, including a grant, is included
  in capacity admission before WAL;
- a failed TSO or grant-sequence reservation leaves logical time and pending
  state unchanged and restores eligible TSO FIFO state;
- a validated mutation holds a membership lease until its handler or stream
  ACK boundary completes, while resign and destroy drain those leases before
  cleanup;
- same-name recreation and RTI restart cannot reuse the preceding generation's
  federate handles;
- destroy empties every composed runtime manager and event stream, and cleanup
  failure never republishes a partially cleaned generation or reuses its writer;
- resign rejects new sends, waits for the real wire ACK, then issues the
  federation resign RPC;
- the deterministic gates pass under the Go race detector repeatedly.

### Do

1. Replaced recipient-count reservation with exact `(recipient, event)`
   reservation. The production outbox locks all recipients in handle order,
   simulates real batch boundaries, and commits the captured deliveries without
   a post-WAL capacity decision. Timer identity now uses an initialized token,
   removing the callback-construction race.
2. Added a TSO buffer reservation that owns the federation evaluator boundary
   from final classification through WAL and outbox admission. Logical-time
   advancement can promote a formerly buffered delivery into the same atomic
   immediate fanout. This removes both the grant/extraction TOCTOU and the
   outbox/evaluator ABBA lock order.
3. Grant emission reserves the complete FIFO `eligible TSO callbacks + TAG`
   sequence before WAL. Admission or WAL failure restores TSO state and
   withholds the grant and logical-time mutation.
4. Added federation operation leases. Unary handlers hold a lease for their
   full mutation; the persistent interaction stream acquires one for every
   non-handshake message and releases it only after the ACK send. Resign and
   destroy take the exclusive side before membership mutation or cleanup.
5. Encoded generation in federate handles, retained monotonically increasing
   same-name generations, and added a production restart epoch. With a log
   directory the epoch is atomically persisted and incremented; non-persistent
   server mode uses a random nonzero process epoch.
6. Completed destroy hooks for declaration, object, time, synchronization,
   ownership, DDM, save/restore protocol state, FOM, MOM, cluster assignment,
   outbox channels/timers, and the per-federation event-log writer. Cleanup
   failure keeps the partially cleaned execution absent and prevents its writer
   from being reused. Writer close is marked complete only after the sink
   actually closes.
7. Interaction resign now closes admission first, drains an active stream or
   lifecycle-linked unary call to its ACK boundary, always attempts the resign
   RPC even after a forced drain timeout, and lets event-pump cancellation
   interrupt a full SDK callback channel.

### Review

Six parallel adversarial reviews examined fanout/outbox, time/grants,
membership/generation, teardown, SDK ACK drain, and verification design. Their
P0 findings led to exact-event reservation, TSO evaluator reservation,
membership operation leases, retryable writer close, real wire-ACK testing,
and a workspace-local Windows race toolchain.

Verification evidence:

- focused normal tests passed for `rtid`, object, time, federation, gRPC
  transport, event log, and the Go SDK;
- all tests in those seven packages passed once under `go test -race`;
- the selected atomicity/generation/teardown/ACK gates passed 20 repeated race
  runs, and the persisted restart epoch/lifecycle gates passed 10 more;
- the repository-wide Go run reported every discovered Go package passing;
  the root glob alone reported an existing Windows ACL denial while scanning
  `verification/out/pytest-analyzer`, after package execution had completed;
- Python SDK transport passed 7 tests, verification common passed 36, and the
  fair-comparison contracts passed 22;
- the race compiler is the official MSYS2 2026-06-11 UCRT64 GCC 16.1.0 in the
  workspace. Its SFX SHA-256 matched the official release checksum and
  `libsynchronization.a` was present.

No performance result or Pitch parity claim is inferred from these correctness
tests.

### Reflect

**Accept for the requested runtime correctness slice.** Atomic production
fanout admission, TSO-before-grant failure handling, live and restart generation
fencing, composed teardown, wire ACK drain, and clean race execution now have
deterministic tests and repeated race evidence.

**Rework remains for the wider Phase 0 product release.** Legacy mutating RPCs
that carry a federation name but no federate handle cannot be identity- or
generation-bound without a wire-contract change. Persisted save bundles and
cross-generation event-log provenance need an explicit compatibility policy,
and Pitch process/log attestation plus the preregistered AB/BA performance run
remain outside this iteration. Persistent interaction transport therefore does
not receive a new default-on or Pitch-performance decision from this loop.

## 24. 2026-07-13 Phase 0 generation and provenance closure loop

### Plan

Close the remaining generation and provenance gaps without weakening the six
runtime correctness gates accepted in section 23:

- carry an expected federation generation on every federation-name mutation
  that cannot derive it from a generation-bearing federate handle;
- bind event logs and save bundles to an immutable federation generation, FOM
  digest, logical-time mode, and seed where applicable;
- keep create, join, resign, destroy, and cleanup hooks inside explicit
  lifecycle gates, with no lock inversion or partially active generation;
- reserve restart epochs durably and atomically across concurrent `rtid`
  processes, including abrupt process exit;
- require Pitch process, command line, JAR, and server-log evidence before a
  fair-comparison result can be claim grade;
- rerun broad normal, race, SDK, contract, and Ralph verification before making
  a release decision.

### Do

1. Extended the append-only protobuf contract with expected-generation fields
   for federation destroy/join, admin destroy/promote, and cluster notification.
   Go and Python clients cache and send the authoritative generation; server
   interceptors validate the token before mutation. MOM target queries remain
   read-only lookups and do not impersonate the queried federate.
2. Added generation-qualified event-log v2 paths and headers. Same-name
   recreation opens a distinct file with the actual generation, mode, and seed;
   replay preserves that provenance and never overwrites an existing log.
3. Added generation-qualified savepoint v2 storage, exact-generation restore,
   ordered FOM-byte digests, atomic temporary-file publication, and invisible
   aborted writes. Legacy storage remains available only through the explicit
   resolver-free compatibility path.
4. Reworked lifecycle gates so create setup completes before join publication,
   join setup completes before mutation leases, pre-resign hooks see the live
   member, and destroy owns the exclusive operation gate through cleanup and
   removal. Failed cleanup leaves no partially cleaned execution reachable.
5. Replaced process-local restart epoch allocation with a locked, durable
   high-water reservation. Windows uses `LockFileEx` and write-through replace;
   Unix uses `flock`, atomic rename, and parent-directory sync.
6. Hardened fair-comparison provenance. Go and Pitch adapters seal executable,
   JAR, command-line, FOM, event-log, and server-process evidence. Claim-grade
   scheduling requires exactly five warm-up pairs, twenty measured pairs,
   strict AB/BA alternation, and ten measurements in each order.
7. Restricted Go discovery in Make and CI to actual Go roots so generated
   verification output and Windows ACLs cannot change the tested package set.

### Review

Final verification evidence:

- the explicit repository-wide Go run passed all 57 discovered packages;
- federation, event log, savepoint, object, time, gRPC transport, Go federate
  SDK, and `rtid` all passed together under the race detector;
- fair-comparison contracts passed 30 tests, Pitch evidence generation passed
  5, and the Ralph engine passed 8;
- the Python SDK suite reported 856 passed and 16 skipped. Its six remaining
  failures are the pre-existing optional `pyjevsim` symbol/delivery cases (four),
  the M5 `b'None'` payload case (one), and strict mypy without the optional
  `cryptography` dependency (one). Generation fencing and the changed transport
  tests pass;
- cross-process epoch tests reserve unique blocks in 12 concurrent processes,
  recover after abrupt exit, and ignore orphan temporary files;
- protobuf generation succeeds with the pinned local Buf tool. Repository-wide
  Buf lint still reports longstanding naming/style violations outside this
  wire-compatible append-only change.

The Pitch evidence path was exercised against an isolated pRTI CRC. That CRC
printed its startup banner but did not open the configured port within 20
seconds, so no attested Pitch result or claim-grade comparison was emitted. The
verifier failed closed as intended. No performance ratio is inferred from this
run.

### Reflect

**Accept for the requested correctness and provenance closure.** Atomic fanout,
TSO-before-grant recovery, generation fencing, complete non-reactivating
teardown, ACK-boundary drain, generation-scoped persistence, durable restart
epochs, and clean race execution now have deterministic evidence.

**Rework for the performance objective.** A new claim-grade Pitch comparison is
still blocked by isolated CRC readiness. Beating or matching Pitch remains
unproven until the preregistered five-warm-up plus twenty-measured AB/BA run
finishes with process/log attestation and all semantic/accounting gates passing.

## 25. 2026-07-13 persistent Pitch claim-grade comparison loop

### Plan

Resolve the Pitch Free CRC startup constraint without weakening the comparison
contract, then execute the preregistered performance session:

- keep Pitch and Go servers session-persistent;
- use the exact same FOM bytes, seed 1516, count 100, two independent federate
  processes, sequential update/send/TAR choreography, immediate callbacks, and
  file server logging;
- exclude five warm-up pairs and measure twenty strictly alternating pairs with
  exactly ten AB and ten BA orders;
- seal the Pitch RTIexec PID, executable, start time, hash, and per-arm appended
  CRC log bytes, plus the existing Go process and generation-log evidence;
- reject before ratio analysis on any semantic, delivery, provenance, or
  schedule mismatch.

### Do

1. Added a persistent Pitch mode to `verification/pitch/Run.ps1` and the fair
   adapter. It binds the online RTIexec by PID and endpoint, records a
   `persistent_session` process descriptor, and copies the CRC bytes appended
   during each federation into an immutable arm-local artifact.
2. Kept the existing isolated per-arm mode as the fallback. The Python evidence
   builder now accepts either lifecycle while still requiring per-arm Java
   verifier processes and exact JAR/classpath evidence.
3. Fixed live CRC log observation to read file length through a shared file
   handle. Pitch can leave stale `FileInfo.Length` metadata while the log is
   open, even though the bytes have already been written.
4. Rebuilt the current gorti RTID binary and ran a two-pair smoke before the
   claim session. The smoke passed in both AB and BA order.
5. Ran count 100 with five warm-up and twenty measured pairs, 10,000 paired
   bootstrap resamples, and claim-grade evidence. All 50 arms completed.

### Review

The session at
`verification/out/fair-comparison/claim-persistent-1516-v14-20260713`
passed the semantic, accounting, provenance, and schedule gates:

- semantic projection: pass, SHA-256
  `0f148007c8c8e394c398ca05636afda672938a80f7c15b2ed9f25ac4a7da6c42`;
- delivery accounting: Pitch 4,000/4,000 and Go 4,000/4,000, with zero
  rejected, dropped, duplicate, or invalid deliveries;
- schedule: five warm-up, twenty measured, strict alternation, AB=10 and BA=10;
- all measured Pitch arms used PID 31260 with `persistent_session` lifecycle
  and nonempty sealed CRC log bytes;
- all 40 measured result contracts and the session manifest passed validation;
- the modified fair/Pitch test suites passed 35 tests.

Paired median Go/Pitch ratios are below. Ratios below 1 favor gorti. Pitch and
Go columns are medians across run summaries; the ratio is the median of paired
per-run ratios, so it is the decision statistic rather than a quotient of those
two displayed columns.

| Metric | Statistic | Pitch us | Go us | Go/Pitch | Paired 95% CI |
|---|---:|---:|---:|---:|---:|
| sendInteraction | median | 121.275 | 165.600 | 1.218 | 1.120..1.327 |
| sendInteraction | p95 | 492.350 | 428.100 | 1.067 | 0.840..1.335 |
| sendInteraction | p99 | 1,004.150 | 2,385.650 | 1.531 | 1.045..2.979 |
| updateAttributeValues | median | 215.400 | 212.700 | 0.902 | 0.811..0.972 |
| updateAttributeValues | p95 | 597.100 | 504.150 | 0.765 | 0.692..1.086 |
| updateAttributeValues | p99 | 1,128.050 | 862.100 | 0.933 | 0.557..1.444 |
| timeAdvanceRequest | median | 184.375 | 219.475 | 1.089 | 0.826..1.169 |
| timeAdvanceRequest | p95 | 311.800 | 489.650 | 1.409 | 0.894..2.855 |
| timeAdvanceRequest | p99 | 544.150 | 868.500 | 1.519 | 0.936..3.100 |
| completed delivery | median | 726.525 | 650.700 | 0.871 | 0.715..0.941 |
| completed delivery | p95 | 16,143.750 | 1,866.050 | 0.112 | 0.049..0.286 |
| completed delivery | p99 | 17,087.000 | 7,532.050 | 0.435 | 0.228..0.772 |

No order-effect confidence interval excluded 1, so this session does not show a
statistically clear second-run penalty favoring either implementation.

### Reflect

**Accept the comparison and completed-delivery result.** The claim-grade
contract is satisfied. gorti improves completed-delivery median by about 12.9%,
p95 by about 88.8%, and p99 by about 56.5% on the paired decision ratios. Its
attribute-update median is about 9.8% better.

**Rework the uniform Pitch-performance objective.** gorti does not beat or
match Pitch across every operation. `sendInteraction` median is about 21.8%
slower with a confidence interval entirely above parity, and p99 is about 53.1%
slower. TAR results are inconclusive because all intervals cross parity. The
next optimization loop should therefore target synchronous interaction
admission and its p99 path without trading away the accepted delivery tail.

## 26. 2026-07-13 synchronous interaction attribution loop

### Plan

Explain and reduce the remaining synchronous `sendInteraction` gap while
preserving the accepted correctness boundary:

- keep one ACK per call after WAL and atomic fanout ownership transfer;
- separate SDK, gRPC, registry, TSO reservation, and file-WAL costs;
- test runtime and gRPC tuning only behind paired controls;
- reject any optimization that weakens TSO-before-grant ordering, generation
  fencing, teardown drain, delivery accounting, or the caller-visible ACK;
- promote a candidate to a new 5+20 claim run only after a smaller AB/BA screen
  shows at least a 5% paired median improvement without tail regression.

### Do

1. Reused the client-side empty ACK object and the server stream's immutable
   empty ACK. The client change removes one per-call allocation without moving
   the ACK boundary.
2. Classified all timestamped recipients under one authoritative TSO evaluator
   reservation. Non-time-engaged recipients remain immediate and time-engaged
   recipients remain buffered; low-level `BufferTSO` retains its prior
   always-buffer contract.
3. Added permanent microbenchmarks for the SDK persistent stream, raw gRPC
   stream, complete registry interaction path, and real file event-log append.
4. Fixed persistent comparison process attestation so the session launcher
   passes the known RTID PID directly. This removes an unnecessary privileged
   Windows port-owner query while retaining executable-path and SHA-256 checks.
5. Screened `GOMAXPROCS=1`, gRPC shared write buffers, and prepared ACK frames.
   None changed semantics; none met the latency gate.

### Review

The accepted claim-grade v14 result remains the performance decision baseline:

- gorti `sendInteraction` median: 165.600 us;
- Pitch `sendInteraction` median: 121.275 us;
- paired Go/Pitch ratio: 1.218, 95% CI 1.120..1.327;
- gorti `updateAttributeValues` median: 212.700 us, already better than Pitch's
  215.400 us on the paired decision ratio;
- gorti completed-delivery median and tails remain better than Pitch.

The attribution measurements show:

| Layer | Representative cost | Allocation evidence |
|---|---:|---:|
| Registry RO path | 0.63..0.67 us | 904 B, 16 allocs |
| Registry reserved TSO path | 0.74..0.83 us | 944 B, 17 allocs |
| Real file WAL append | 2.13..2.19 us | 112 B, 3 allocs |
| Raw persistent gRPC RTT | 65..79 us | 1,943 B, 69 allocs |
| SDK persistent gRPC RTT | 62..81 us | 2,135 B, 71 allocs |

The registry plus file WAL is therefore only about 3 us. The dominant floor is
the synchronous HTTP/2/gRPC request-ACK round trip and its Windows scheduler
interaction with the concurrent callback and TAR traffic.

`GOMAXPROCS=1` was decisively rejected by a two-warm-up, six-measured-pair
screen: `sendInteraction` paired median Go/Pitch was 2.125 (CI
1.054..3.900), and update/TAR also regressed. All 1,200 deliveries per
implementation were nevertheless valid. Shared write buffers added about four
allocations and 200--250 B per call without a stable latency gain. Prepared ACK
frames removed three allocations and about 40 B, but did not improve latency
consistently and rely on an experimental gRPC API.

A follow-up default-runtime screen completed four measured pairs before a
Pitch Java process-path attestation transiently reported `ntdll.dll`; the
incomplete session was correctly rejected and is not used as a performance
claim.

Verification closure:

- normal tests passed for object, time, Go federate SDK, gRPC transport, and
  event log;
- those five packages passed together under the Windows race detector using
  the workspace UCRT64 GCC toolchain;
- fair-comparison contract tests passed 30/30 with a workspace-local temporary
  directory;
- atomic mixed-recipient fanout, delivery-failure grant withholding,
  concurrent exactly-once grant evaluation, wire-ACK resign/drain, forced
  drain cancellation, and composed federation teardown each passed 20 repeated
  runs;
- PowerShell parsing passed for both changed persistent-comparison scripts.

### Reflect

**Accept the low-risk allocation and TSO batching changes.** They preserve the
strict ACK and time-order semantics, pass focused tests, and reduce avoidable
work. Also accept the persistent PID attestation fix and the new diagnostic
benchmarks.

**Reject runtime tuning, shared buffers, and prepared ACKs.** They do not close
the claim-grade median gap and either worsen resource use or rely on an
experimental API.

**Reframe the root cause.** gorti interaction is not slow relative to its own
object-update path; it is already about 22% faster in absolute median. Pitch's
interaction path is unusually fast relative to Pitch's own update path. The
remaining gap is transport and return-boundary specialization, not Go language
execution or registry business logic.

**Next loop requires an API-level transport decision.** Keep the strict
synchronous API for exact error and ACK semantics. Add a separate bounded,
pipelined asynchronous interaction API with ordered completion futures for
throughput workloads, then compare it only against an explicitly equivalent
Pitch asynchronous choreography. Beating Pitch's single-call synchronous
latency requires either a lighter dedicated transport or an earlier success
boundary; the latter is rejected because it would make the existing comparison
semantically unfair.

## 27. 2026-07-13 release closure and deferred performance track

### Plan

- freeze the accepted synchronous interaction semantics for the v0.9 release;
- move further transport work to a clearly labeled future roadmap;
- preserve the claim-grade v14 run as the decision baseline; and
- make publication and documentation cleanup the active release work.

### Do

- documented the bounded asynchronous interaction API as future work in
  `docs/roadmap.md` and `docs/performance.md`;
- retained exact ACK, atomic recipient reservation, TSO-before-grant,
  generation fencing, delivery accounting, and complete teardown as mandatory
  acceptance gates;
- excluded machine-local launcher paths and generated benchmark/runtime output
  from the public source tree; and
- added the SoftwareX manuscript, reproducibility documentation, and citation
  metadata without inventing a DOI, certification, or acceptance status.

### Review

The current implementation has passed the focused lifecycle, race, semantic,
and fair-comparison checks recorded above. The remaining synchronous caller
latency is dominated by the transport/ACK boundary; screened runtime tuning and
allocation reductions did not produce a stable claim-grade improvement.
Changing that boundary during release cleanup would mix an API experiment with
publication evidence and risk making the Pitch comparison semantically unfair.

### Reflect

**Close the current optimization loop.** The v0.9 publication branch keeps the
strict synchronous API and its verified semantics. No further performance gain
is claimed for this release.

**Open a separate future track only when an equivalent comparator exists.** A
bounded asynchronous API or dedicated transport may improve throughput, but it
must have explicit backpressure, ordered completion, failure reporting, and an
equivalent Pitch choreography. A candidate reaches a new 5+20 AB/BA claim run
only after a smaller paired screen shows a stable gain without semantic or tail
regression.
