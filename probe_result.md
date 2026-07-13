# gorti 성능 분석 리포트

작성일: 2026-07-12
대상: gorti Python IEEE 1516e facade + loopback gRPC + Go `rtid`
비교 대상: Pitch pRTI Java 예제

## 1. 결론

현재 관측된 큰 성능 차이는 하나의 느린 함수 때문이 아니다. 가장 큰 원인은 다음 세 층이 겹친 것이다.

1. 현재 검증 시나리오는 매 logical time마다 `update -> interaction -> producer TAR -> consumer TAR -> callbacks`를 직렬 수행한다. 따라서 보고된 throughput은 순수 OM 처리량이 아니라 TM 장벽과 callback 대기를 포함한 **closed-loop scenario rate**다.
2. gorti Python 경로는 각 HLA 호출마다 동기 facade에서 별도 asyncio thread로 넘어가 unary gRPC를 수행한다. 100회 snapshot에서 네 직렬 RPC 평균의 합은 `20.03 ms`이고, completed batch 평균 `23.04 ms`의 대부분을 설명한다.
3. callback 대기와 서버 outbox가 짧은 트래픽에 고정 지연을 더한다. 검증기는 두 ambassador에 최대 10 ms의 evoke를 연속 호출하고, 서버 outbox는 부분 batch를 1 ms timer로 flush한다.

event log 파일 쓰기도 영향을 주지만 단독 주원인은 아니다. 3회 A/B 실험에서 log를 끈 경우 중앙값 throughput이 `31.34 -> 33.65 deliveries/s`로 7.4% 개선됐다. 반면 실행 간 변동이 커서 이 수치는 정밀한 release claim으로 사용할 수 없다.

또한 기존 Pitch/gorti 수치 중 completed delivery latency와 sustained throughput은 시작 시점이 서로 달라 직접 비율 비교가 무효다. 현재 결과는 RTI core 순위가 아니라 서로 다른 client stack과 시나리오의 local snapshot으로만 해석해야 한다.

## 2. 관측 결과

### 2.1 기존 100-iteration snapshot

동일 seed `1516`, semantic log는 byte-identical이었다.

| 지표 | Pitch | gorti | 관측 비율 |
|---|---:|---:|---:|
| Attribute update call mean | 0.247 ms | 6.634 ms | 26.82x |
| Attribute update call p95 | 0.358 ms | 16.632 ms | 46.51x |
| Interaction send call mean | 0.138 ms | 4.768 ms | 34.66x |
| Interaction send call p95 | 0.224 ms | 17.858 ms | 79.69x |
| Completed batch mean | 1.007 ms | 23.043 ms | 22.88x |
| Completed batch p95 | 1.094 ms | 71.365 ms | 65.21x |
| Reported sustained throughput | 70.39/s | 26.42/s | 0.375x |

주의: 마지막 두 종류의 지표는 양쪽 timing boundary가 다르므로 성능 비율로 사용할 수 없다.

### 2.2 critical-path 산술

gorti snapshot의 평균 호출 시간은 다음과 같다.

```text
updateAttributeValues       6.633987 ms
sendInteraction            4.767820 ms
2 x timeAdvanceRequest      8.632026 ms
--------------------------------------
serialized call sum        20.033833 ms
completed batch mean       23.043308 ms
remaining delivery path     3.009475 ms
```

즉, closed-loop batch 평균의 약 87%가 네 개의 직렬 API 호출만으로 설명된다. 이 결과는 protobuf 한 함수보다 시나리오의 round-trip 수와 직렬성이 우선 병목임을 보여준다.

### 2.3 event log A/B probe

동일 bundled `rtid.exe`, seed 1516, count 100으로 file-backed log와 discard sink를 각각 3회 실행했다.

| 지표 | Log on 중앙값 | Log off 중앙값 | 변화 |
|---|---:|---:|---:|
| Update mean | 4.375 ms | 3.710 ms | -15.2% |
| Interaction mean | 2.778 ms | 2.612 ms | -6.0% |
| Completed batch mean | 15.791 ms | 13.033 ms | -17.5% |
| Closed-loop delivery rate | 31.337/s | 33.649/s | +7.4% |

범위는 log-on throughput `27.34..35.29/s`, log-off `33.60..35.13/s`였다. warm-up과 실행 순서 randomization이 없고 표본도 3회이므로 방향성 증거로만 사용한다.

event log는 매 append마다 `fsync`하지 않는다. 그러나 federation mutex를 잡은 채 protobuf 직렬화와 length/body 두 번의 `os.File.Write`를 수행한 뒤 fan-out한다. 100 iteration의 steady state에는 update, interaction, 두 grant를 합쳐 약 400 append가 있다.

### 2.4 Python facade thread-hop probe

`Rti1516eAmbassador._run(asyncio.sleep(0))` 2,000회를 측정했다.

| mean | p50 | p95 | max |
|---:|---:|---:|---:|
| 0.069 ms | 0.065 ms | 0.100 ms | 0.211 ms |

thread hop 자체는 평균 수 ms를 설명하지 못한다. 다만 실제 RPC, event translation, callback과 경쟁할 때 tail variance를 키울 수 있으므로 stage timing이 필요하다.

### 2.5 기존 Go perf harness

`TestThroughput_Size25`를 1회 실행한 결과는 다음과 같았다.

```text
size=25 duration=6.05s sent=1704319 throughput=281578/s p50=0.00ms p99=22.07ms
```

이 수치는 gRPC, event log, TM을 우회하고 accepted sends를 세며 delivery/drop 완전성을 보장하지 않는다. 따라서 production 성능 수치가 아니다. 다만 Go object/declaration in-process 경로가 본질적으로 26 operations/s로 제한된다는 해석은 반박한다.

## 3. 원인 분석

### A. 비교 및 workload 정의 문제 - 확신도 매우 높음

- Pitch batch latency는 subscriber TAR 직전에 시작하지만 gorti는 producer update 직전에 시작한다.
- Pitch는 두 JVM process, gorti는 한 Python process 안의 두 ambassador다.
- Pitch call timer 밖에서 payload map을 준비하지만 gorti는 일부 dict 생성과 encode를 timed lambda 안에서 수행한다.
- 매 메시지마다 paired TAR를 수행하므로 throughput이 OM capacity보다 TM/callback pacing을 측정한다.
- Pitch를 항상 먼저 실행하고 warm-up, AB/BA randomization, raw samples, confidence interval이 없다.

따라서 기존 `22.88x`, `65.21x`, `0.375x`를 RTI core 비교값으로 사용하면 안 된다.

### B. 네 unary RPC의 직렬화 - 확신도 높음

`verification/gorti/verifier.py`는 update, interaction, producer TAR, consumer TAR를 한 thread에서 순서대로 기다린다. Python facade의 `_run`은 coroutine을 background loop에 제출하고 `Future.result()`로 완료까지 block한다. 각 transport call은 별도의 protobuf unary RPC다.

실제 federate가 별도 process에서 병렬로 TAR를 제출하는 동작보다 불리하며, 네 round trip 평균 합이 batch 평균의 대부분을 차지한다.

### C. callback evoke polling - 확신도 높음

gorti callback은 이미 background task에서 immediate 방식으로 전달된다. 그런데 `_wait_until`은 predicate를 확인한 후 producer와 consumer에 `evokeMultipleCallbacks(0.0, 0.01)`를 연속 호출한다.

첫 evoke 중 consumer callback이 완료되어도 두 호출 사이 predicate를 다시 확인하지 않는다. 두 번째 evoke는 이미 도착한 callback을 놓치고 새 callback을 기다리며 최대 10 ms를 소비할 수 있다. 이 구조는 10 ms 단위 tail을 만들 수 있다.

### D. outbox 1 ms timer - 확신도 높음

production outbox는 최대 32 events를 batch하고 작은 batch는 1 ms 후 flush한다. 이 시나리오는 매 tick에서 recipient별 event 수가 작아 timer path를 자주 탄다. 이후 stream service는 batch를 다시 event별 `stream.Send`로 전송하므로 wire batching 이득도 제한적이다.

### E. synchronous event-log append - 확신도 중간 이상

update, interaction, grant는 write-ahead append가 끝난 뒤 state/fan-out을 진행한다. 현재 경로에는 다음 비용이 있다.

- global multiplex lookup lock
- event adapter의 marshal/unmarshal 후 writer에서 재-marshal
- federation writer mutex 안에서 serialization
- length prefix와 body의 두 write

A/B probe는 median 기준 6-18% 개선 방향을 보였지만 분산이 커서 더 많은 randomized run과 server profile이 필요하다.

### F. TAR/LBTS algorithm의 확장성 - 2 federate 영향 중간, 대규모 위험 높음

각 TAR는 global state와 pending request를 scan하고 regulator를 sort한다. grant 하나를 내보낸 뒤 fixed-point scan을 다시 시작하므로 federate 수 `F`에서 lockstep workload가 `O(F^2)`에 가까워질 수 있다. store key가 federation별 partition이 아니어서 다른 federation도 scan 대상이 된다.

현재 두-federate 결과의 첫 원인은 아니지만 25/100 federate에서는 우선 확인해야 할 서버 병목이다.

### G. OM allocation과 lookup - 확신도 중간

gRPC handler와 registry가 요청마다 map을 재구성하고, declaration query가 lock/iteration/sort를 반복한다. interaction은 MOM 여부 확인을 위해 FOM name resolution도 반복한다. 작은 payload에서는 milliseconds의 단독 원인으로 보기 어렵지만 높은 fanout에서 CPU와 allocation을 늘린다.

## 4. 측정 한계

- bundled `bin/rtid.exe`의 source commit SHA를 기록하지 않아 검사한 Go source와 binary 일치 여부가 보장되지 않는다.
- A/B probe는 3회이며 warm-up, randomization, CPU affinity, power-state 고정이 없다.
- gorti metric은 raw sample을 보존하지 않아 bootstrap CI를 계산할 수 없다.
- event-log off는 nil log가 아니라 serialization을 유지하는 discard writer다. 따라서 file I/O만 주로 제거한다.
- 기존 Pitch/gorti delivery timing boundary와 topology가 다르다.
- 기존 Go perf harness는 production 경로와 delivery accounting을 우회한다.

## 5. 판정

현재 증거로는 “Go RTI core가 Pitch보다 20-80배 느리다”라고 결론낼 수 없다. 정확한 판정은 다음과 같다.

> 현재 gorti Python end-to-end lockstep 시나리오는 네 개의 직렬 unary RPC, per-message TAR 장벽, callback polling, 1 ms outbox timer, write-ahead logging을 모두 critical path에 포함한다. 이 때문에 사용자 관점의 scenario latency는 실제로 높다. 동시에 Pitch와 측정 경계가 달라 RTI core 간 비율은 아직 검증되지 않았다.

개선은 측정 계약을 먼저 바로잡고, callback/TAR 직렬성을 제거한 뒤, event log와 TM server profile에 따라 진행해야 한다. 구체적 실행 계획은 `improvement.md`에 정의한다.

## 6. 근거 코드 및 산출물

- Python sync bridge: `pysdk/rti1516e/standard.py:1268`, `:1303`
- OM/TAR choreography: `verification/gorti/verifier.py:619`, `:764`
- callback polling: `verification/gorti/verifier.py:828`
- callback facade semantics: `pysdk/rti1516e/standard.py:994`
- event log writer: `rti/internal/eventlog/writer.go:127`
- OM write-ahead paths: `rti/internal/object/update.go:147`, `interaction.go:63`
- outbox timer: `rti/cmd/rtid/outbox.go:14`, `:132`
- stream expansion: `rti/internal/transport/grpc/stream.go:62`
- TAR scans: `rti/internal/time/ner.go:157`, `:268`, `:317`
- controlled probe artifacts: `verification/out/perf-probe/`
- original parity snapshot: `verification/out/ralph-live-100/`

## 7. Follow-up findings: async transport and grant delivery

The Python SDK already uses `grpc.aio`; the synchronous facade adds a
cross-thread wait but does not create a blocking gRPC channel. Opt-in async TAR
reduced admission latency by 11.7% across 10 balanced pairs, yet changed the
grant-observed completed latency by only 0.35%. It is therefore not the main
remaining bottleneck.

The production outbox timer was on the grant-observed critical path. Making a
`TimeAdvanceGrant` flush its recipient's partial batch reduced completed
delivery by 45.4% and send-to-delivery by 32.6% at the default batch size 32.
The flush includes preceding buffered TSO callbacks, so the required
TSO-before-grant sequence is unchanged.

The time manager also has a concurrency risk: concurrent `tryGrantPending`
evaluators can snapshot the same eligible request before either clears it and
emit duplicate grants. A per-federation evaluator lock is required before more
aggressive concurrent TAR processing.

Direct stream-to-callback delivery removed one asyncio queue hop and improved
completed delivery by 4.6% across 10 balanced pairs. It remains opt-in because
the gain is modest and callback execution directly backpressures stream reads.

An ordered update+interaction batch RPC was implemented and measured, then
removed. It regressed completed delivery by 9.1% because concurrent unary
requests already overlap on HTTP/2 while the batch executes the server
operations serially. Network-call-count reduction is therefore not the next
local-loopback optimization.

## 8. Superseded native-Go comparison

The earlier native-Go run was useful for proving that the Python facade was a
major part of the original gap, but its comparison against one retained Pitch
run was not claim-grade. In particular, the two arms did not share paired
AB/BA execution, equal warm-up and measured counts, or paired confidence
intervals. The previous statement that Go completed delivery 73.8% faster is
therefore withdrawn and superseded by section 9.

## 9. Paired Pitch/Go fair comparison

The replacement session used the exact same FOM bytes (SHA-256
`28c473b45137c4becaa0f93259e7c8d1dffd6785353b971424402669430c2618`),
seed 1516, two independent federate processes per arm, sequential
update-send-TAR choreography, subscriber-pre-TAR timing boundary, immediate
callbacks, and file-backed server event logging. It ran five warm-up pairs and
twenty measured pairs with ten AB and ten BA orders. Every run used 100
timestamped update/interaction cycles.

The measured RTI binary hashes were Go RTID
`34019082c24528d37f8f6214ee0256b4ccd1ca22ece2982637101ec729bcbfac`
and Pitch RTI
`f20b23513bbe51b64012244fe22d7357ec10cab6d61fa6de68f0e0f1b931032a`;
each remained constant across all measured arms.

All 50 arms passed. The measured runs delivered 4,000/4,000 callbacks per
implementation with zero rejection, drop, duplicate, or invalid callback. The
FM/DM/OM/TM semantic projection was identical with SHA-256
`0f148007c8c8e394c398ca05636afda672938a80f7c15b2ed9f25ac4a7da6c42`.
No raw timing sample was zero.

The ratios below are paired medians of Go/Pitch run statistics; lower is
better. Confidence intervals are deterministic paired-bootstrap 95% intervals.

| Metric | Pitch run median | Go run median | paired Go/Pitch | 95% CI |
|---|---:|---:|---:|---:|
| Completed delivery batch median | 0.6186 ms | 0.6014 ms | 0.973x | 0.957..0.997x |
| Update call median | 0.2198 ms | 0.1879 ms | 0.834x | 0.818..0.867x |
| Interaction send median | 0.1165 ms | 0.1911 ms | 1.612x | 1.548..1.657x |
| TAR call median | 0.1784 ms | 0.1717 ms | 0.958x | 0.940..0.984x |
| Completed delivery p95 | 15.8090 ms | 1.1945 ms | 0.190x | 0.076..1.108x |
| Completed delivery p99 | 16.7387 ms | 11.7206 ms | 0.944x | 0.747..3.677x |

The accurate conclusion is mixed. gorti matches Pitch on the measured
two-federate end-to-end median and is faster on update and TAR admission, but
it is materially slower on interaction-send admission and has unstable p99
tails in several call metrics. AB/BA order-effect intervals include 1.0 for
all median metrics, so no material median order bias was detected. This is a
Pitch-class overall path, not evidence that gorti uniformly beats Pitch.

The retry also exposed and fixed two semantic races: a synchronization
announcement could arrive before the local participant snapshot, and ordinary
TAR was incorrectly inclusive at `LBTS == requestedTime`, allowing a grant to
overtake a timestamp-equal message. The verifier now rejects any subscriber
grant that arrives before both timestamped callbacks.

Evidence: `verification/out/fair-comparison/claim-file-1516-v5-strict-tar/analysis.json`
and `manifest.json` in the same directory.

## 10. Interaction admission root cause and final rerun

The interaction gap was not caused primarily by Go language execution or the
RTI interaction registry. The retained final probe measured a no-op direct
handler at 0.18-0.84 microseconds, localhost unary gRPC at 182-258
microseconds and about 10.9 KB/212 allocations, and the persistent stream at
101-134 microseconds and about 2.0 KB/70 allocations. These diagnostic numbers
are retained separately in
`verification/out/fair-comparison/interaction-transport-probe.txt`.

The production changes therefore preserve synchronous acknowledgement but
reuse one interaction stream per federate. The SDK waits for the server reply;
it does not return after enqueue. It performs a capability handshake before
the first request, falls back to unary for old servers, concurrent calls,
outgoing per-call metadata, bearer credentials, and externally wrapped gRPC
connections, and cancels the stream on call cancellation or resignation. The
server stream is opt-in only for the RTID composition without OIDC interceptors.

Additional hot-path changes resolve handles and construct value maps before
timing, cache MOM/FOM classification, use O(1) interaction-publication
membership checks, and share one defensive parameter map between event logging
and fanout. The fair harness now uses a persistent Go RTID to mirror the
persistent Pitch endpoint shape, strictly alternates AB/BA order, records the
dirty worktree accurately, and records both Go RTID and client binary hashes.
Because Pitch RTIexec is externally managed, its PID/start time are not
attested.

Final v12 used five warmup pairs and twenty measured pairs, identical FOM SHA
`28c473b45137c4becaa0f93259e7c8d1dffd6785353b971424402669430c2618`,
seed 1516, two processes, sequential update-send-TAR choreography, immediate
callbacks, and file-backed server logs. All 50 arms passed. Each implementation
delivered 4,000/4,000 measured callbacks with zero rejection, drop, duplicate,
or invalid callback. Semantic projection SHA was
`0f148007c8c8e394c398ca05636afda672938a80f7c15b2ed9f25ac4a7da6c42`.

| Metric | Pitch median | Go median | paired Go/Pitch | 95% CI |
|---|---:|---:|---:|---:|
| Interaction send | 0.1292 ms | 0.1626 ms | 1.290x | 1.209..1.312x |
| Attribute update | 0.2249 ms | 0.2484 ms | 1.105x | 1.018..1.165x |
| Time advance request | 0.1989 ms | 0.2418 ms | 1.216x | 1.142..1.299x |
| Completed delivery batch | 0.7578 ms | 0.7342 ms | 0.954x | 0.782..1.016x |

Go interaction latency improved from 0.1911 ms in v5 to 0.1626 ms in v12
(-14.9%), and the paired ratio improved from 1.612x to 1.290x. Completed
delivery observed ratio is 0.954x with a CI that includes 1.0; this passes the
provisional practical margin but does not prove statistical equivalence. The
interaction point estimate exceeds 1.25x by 3.2%, while its upper CI exceeds
the gate by 5.0%. TAR p95 also fails at 3.352x (2.486..4.514). The accurate
decision is partial accept with interaction median and TAR tail latency both
remaining rework, not a claim that gorti beats Pitch.

Evidence: `verification/out/fair-comparison/claim-file-1516-v12-final/`.

## 11. Synchronous interaction attribution update

The attested persistent-session v14 comparison supersedes v12 for the current
decision. It completed five warm-up and twenty measured AB/BA pairs with the
same FOM, seed, two-process choreography, callback mode, logging condition, and
measurement boundaries. All semantic and accounting gates passed.

The key medians were:

| Operation | Pitch | gorti | Paired Go/Pitch |
|---|---:|---:|---:|
| sendInteraction | 121.275 us | 165.600 us | 1.218 |
| updateAttributeValues | 215.400 us | 212.700 us | 0.902 |
| timeAdvanceRequest | 184.375 us | 219.475 us | 1.089 |
| completed delivery | 726.525 us | 650.700 us | 0.871 |

This changes the interpretation of the interaction result. gorti interaction
admission is already faster in absolute time than gorti attribute update.
Pitch interaction admission is unusually fast relative to Pitch's own update
path. The relative deficit is therefore specific to Pitch's optimized
interaction transport/return path, not evidence that Go executes interaction
business logic slowly.

New retained microbenchmarks isolate the gorti path:

- registry interaction, RO: 0.63--0.67 us;
- registry interaction, reserved TSO: 0.74--0.83 us;
- real file event-log append: 2.13--2.19 us;
- raw persistent gRPC request/ACK: 65--79 us;
- SDK persistent gRPC request/ACK: 62--81 us.

The registry plus WAL contributes roughly 3 us. The dominant cost is the
synchronous gRPC request/ACK scheduling boundary, which expands under the real
workload's concurrent callback, unary update, and TAR traffic.

Three controls were rejected. `GOMAXPROCS=1` made the paired interaction median
2.125x Pitch and also regressed update and TAR. gRPC shared write buffers added
allocations without reducing latency. Prepared empty ACK frames saved three
allocations and about 40 bytes but produced no consistent latency gain and use
an experimental API.

The semantics-preserving local changes retain one reusable client ACK object,
one immutable server ACK, and one authoritative multi-recipient TSO
classification reservation. They remove avoidable allocation and lock work but
do not justify a new Pitch parity claim.

The remaining performance work is an API-level choice. The strict synchronous
API must continue waiting for its server ACK. A separate bounded pipelined
asynchronous API can improve throughput and amortize transport scheduling, but
must be compared only with an equivalent Pitch asynchronous choreography. An
earlier ACK would make the existing synchronous comparison semantically unfair
and is not recommended.

Evidence:

- `verification/out/fair-comparison/claim-persistent-1516-v14-20260713/`
- `verification/out/fair-comparison/gomax1-v15-screen6-20260713/`
- `rti/internal/object/interaction_benchmark_test.go`
- `rti/internal/eventlog/multiplex_test.go`
- `rti/internal/transport/grpc/interaction_benchmark_test.go`
- `rti/pkg/federate/interaction_benchmark_test.go`
