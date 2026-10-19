# LocalLRC callback completion and reporting

This increment reduces Go SDK callback bookkeeping and report transport costs.
It also reserves control-callback capacity within the server's existing pending
receipt limit after a sustained experiment exposed capacity exhaustion.
HLA service algorithms, callback authorization and saved state formats are
unchanged. The optional `callback_async` profile remains opt-in; Python and Java
SDK reporting implementations are not changed.

## Plan

Keep receive-order callback admission local where the server has negotiated it.
Retain ordered entry/outcome reporting, exact error propagation, recovery and
service barriers. Reduce completion scheduling first, then pipeline reports and
release completed application payloads. Evaluate the worker separately, then
compare the combined changes with the original SDK. Run the prepared IEEE
service regression inventory for the retained implementation.

## Implementation

The report transport now has one session-owned completion worker, rather than
one goroutine for each entry or outcome completion. It applies local bookkeeping
in report order, independently of wire ACK processing. A 256-slot budget covers
asynchronous work until local completion, including acknowledged work. Local
entry reserves its slot before taking the checkpoint lock. Confirmed control
reports do not consume completion slots, so checkpoint-held service barriers
can still make wire progress. Explicit drain continues to include local work
whose wire ACK has arrived.

A stream-owned writer sends a bounded window of up to four 64-report frames
while the transport reads their ACKs. Frame size, message schema and RPC names
are unchanged. A failure preserves the acknowledged prefix, cancels and joins
the old writer, and leaves unresolved outcomes retryable. Lost-ACK retries reuse
the original receipts and never rerun the application callback. Unsupported
servers retain the existing unary fallback.

After entry bookkeeping and outcome acknowledgement, the SDK drops its reference
to the application event. The small completed record retains identity, outcome
and the last receipt. A fresh recovery receipt reopens reporting, not application
execution. Unentered callbacks and unacknowledged results retain their payloads.
Snapshot construction remains a separate save-time operation. This is not a
removal of callback tracking or a new crash-recovery guarantee.

The production server reserves 64 of its 8,192 pending receipt slots for
non-data callbacks. Reflection/interaction fanouts are admitted atomically below
the remaining shared limit; LocalLRC waits using its existing uncommitted-operation
backpressure protocol. Controls still obey the total limit. Direct receipt
admission uses the same distinction. This is bounded headroom, not a guarantee
that arbitrarily many simultaneous controls can never exhaust capacity. Snapshot
restore retains the original total-capacity checks and does not discard pending
data to create headroom.

## Review

New tests first reproduced completion overtaking, admission beyond the unfinished
work budget, stop-and-wait report framing and completed payload retention. They
then passed with the implementation. Tests cover ordered completion independent
of ACKs, control progress under completion pressure, multiple frames before the
first ACK, precise partial rejection, retained failed results and fresh versus
duplicate recovery authorization. Existing callback, local-entry and SDK runtime
tests also passed in repeated sync and async runs.

The initial whole-service campaign passed the 978 required inventory tests in
both profiles, but the broader async campaign failed the actual-server control
outcome recovery test for both time representations. A pre-send interceptor
failure left the new ACK reader waiting on a stream with no request in flight.
The writer now cancels that wait and preserves the send error; gRPC send EOF is
left to Recv so the server's terminal status is not lost. The fault injector also
uses synchronized, per-batch ACK errors instead of one shared mutable error.
The same unsent-result and accepted-result/lost-ACK scenarios remain active.
The repaired live tests passed three repetitions and the focused callback suite
passed fifty repetitions. The initial failure remains in
`callback-pipeline-ieee-qc-20261006`; the final fresh campaign is separate.

Passing the prepared inventory is internal regression evidence, not independent
full-conformance certification. Complete Python and Java SDK regression campaigns
were not rerun for this Go SDK and server transport increment.

The final fresh campaign, including server control headroom, passed without
supplemental reruns:

| Profile | Required inventory | Top-level passes | Failures | Skips |
| --- | ---: | ---: | ---: | ---: |
| `callback_sync` | 978/978 | 2,915 | 0 | 15 async-only cases |
| `callback_async` | 978/978 | 2,930 | 0 | 0 |

Both campaigns include the actual Float64/Integer64 server restart and control
outcome recovery tests, historical wire projections and retained-FDD schema
checks. No required inventory test was omitted or relaxed.

Native race compilation was attempted with `CGO_ENABLED=1` but could not start
because `gcc` is not installed on PATH. Docker's Linux engine pipe is unavailable.
Neither repeated tests nor an older race result substitutes for this run.

## Sustained-load correction

The initial combined SDK experiment (`callback-pipeline-final-paired-20261006`)
stopped at candidate pair 15 with `MOM: callback receipt capacity exceeded`.
Its fifteen completed pairs and the failed trial remain recorded; they are not
presented as a successful twenty-pair experiment. A separate unit test reproduced
data admission consuming the last slots needed by a control fanout. Control
headroom, its hard upper bound, wake-up on released capacity, impossible fanout
rejection and direct receipt admission now have regression coverage.

The original experimental error did not identify the service call, so its exact
call site cannot be recovered from that log. Participant errors now retain the
role/arguments and timed service name. The capacity defect is demonstrated by
the deterministic test, not inferred as a proven trace of the missing call site.
Final SDK comparisons use the headroom server on both sides, and keep the failed
original campaign separate.

## Worker comparison

The worker-only candidate and its preceding retention baseline used identical
server bytes and the same DEVStone-HLA HI 10 by 10 workload: 81,200 measured
callbacks, two independent Go federates, LocalLRC batch size 32, handle callbacks,
compact summaries and `GOMAXPROCS=2`. Five warm-up pairs precede twenty measured
AB/BA pairs with identical FOM bytes and per-pair seeds. All 50 trials completed
and all 25 pairs matched semantic evidence.

| Metric | Retention baseline | Bounded completion worker |
| --- | ---: | ---: |
| Mean completion, seconds | 3.458150 | 3.331604 |
| Median completion, seconds | 3.424018 | 3.284268 |
| Mean per-trial callbacks/second | 23,566.9 | 24,535.4 |

Mean completion fell 3.66%; mean-time speedup is 1.03798 with a paired,
order-stratified bootstrap 95% interval of [0.98807, 1.07929]. Seventeen of twenty
pairs favored the worker. The interval includes one, so this does not establish
an end-to-end speedup. The worker is retained for bounded completion resource use
and ordered bookkeeping. The 4.471-second candidate sample remains included.

## Combined SDK comparison

With the control-headroom server held identical on both sides, the original SDK
and the final worker/pipeline/payload SDK completed all 50 trials. All 25 pairs
matched callback counts, payload/order hashes and lifecycle evidence. Conditions
match the worker comparison: 81,200 measured callbacks, five warm-up and twenty
measured AB/BA pairs, LocalLRC batch size 32, and `GOMAXPROCS=2`. The participant
also adds error-only service context; successful workload choreography is unchanged.

| Metric | Original SDK | Combined SDK changes |
| --- | ---: | ---: |
| Mean completion, seconds | 3.206967 | 2.704372 |
| Median completion, seconds | 3.181414 | 2.691366 |
| Mean per-trial callbacks/second | 25,412.8 | 30,036.4 |
| Completion standard deviation, seconds | 0.215148 | 0.053370 |

Mean completion fell 15.67%. The mean-time speedup is 1.18585, with a paired,
order-stratified bootstrap 95% interval of [1.15549, 1.22663]. All twenty measured
pairs favored the candidate. Fixed-work aggregate throughput rises 18.58%; this
is the reciprocal mean-completion comparison, not the ratio of mean per-trial
throughputs. The 4.026-second baseline observation remains included. This run
measures the combined SDK changes, not an isolated estimate for each component.
The headroom correction is common to both conditions, so its performance effect
is not estimated by this comparison.

## Portico comparison

The final build was compared directly with Portico 2.1.4 on Java 11, using the
same 81,200-callback workload, FOM bytes, per-pair plan/seed and compact validation
summaries. Both use two independent federate processes; gorti also has its server
process. Each trial starts fresh processes. Portico uses JGroups TCP/TCPPING over
IPv4 loopback; gorti uses its native gRPC transport, `callback_async`, LocalLRC
batch size 32 and handle callbacks. Five warm-up pairs precede twenty measured
AB/BA pairs, with 128 warm-up operations per channel inside each trial.

This cross-RTI campaign uses runtime-default CPU settings for both Go and Java
on the same Windows host with 20 logical CPUs. It does not reuse the
`GOMAXPROCS=2` measurements above. No other tests or builds ran during timing.
Host background activity was not isolated, and fresh JVMs do not represent a
long-lived JVM steady-state experiment.

All 50 trials completed, all 25 pairs matched normalized workload evidence and
all twenty measured pairs were retained.

| Metric | Portico | gorti |
| --- | ---: | ---: |
| Mean completion, seconds | 1.865878 | 2.600594 |
| Median completion, seconds | 2.212848 | 2.593030 |
| Completion standard deviation, seconds | 0.746162 | 0.043250 |
| Minimum completion, seconds | 0.581440 | 2.542769 |
| Maximum completion, seconds | 2.690842 | 2.679529 |
| Aggregate callbacks/second | 43,518.4 | 31,223.6 |

The gorti/Portico mean completion ratio is 1.39376, with paired, order-stratified
bootstrap 95% interval [1.20262, 1.69996]. Portico was faster in nineteen of twenty
pairs and had 1.39376 times the aggregate delivery rate in this campaign.
Aggregate rate is total callbacks divided by total measured time, not the mean
of each trial's reciprocal duration. SDK call-return medians are recorded in the
raw summaries but are not used to infer equivalent remote-completion latency.

Portico's measured pairs 14 through 18 form a fast cluster of 0.581-0.673 seconds.
Their callback counts, payloads and ordering still match; the cause of this time
variation is unresolved. No observations were excluded. As a post-hoc sensitivity
check, a circular moving-block bootstrap of four consecutive pairs, with even
start indices to preserve AB/BA balance, gave [1.14289, 1.97995] using 10,000
resamples and seed 1516. This wider interval highlights temporal variation; it
does not turn this campaign into a steady-state or universal RTI ranking.

The conclusion is that the combined SDK changes improve gorti, but gorti does
not overtake Portico on this measured workload. Earlier short-workload,
confirmed-producer results are not pooled with this LocalLRC experiment.

## Measurement boundary

Timing starts when the subscriber arms before VERIFY_START and stops at the
last subsequently validated data callback's receive timestamp. Go records pump
dequeue before DispatchEvent; Java records callback entry before validation.
Successful payload/order validation, report drain and teardown are required,
but final outcome settlement is outside the timer. API return times are not an
equivalent remote-completion boundary across RTIs.

## Reflect

The server still handles reports individually and the SDK retains conservative
service-call barriers. Batch domain processing and service-specific dependency
barriers remain separate changes; they are not implied by pipelined transport.
The current change does not generalize local entry to TSO, region-bearing or
control callbacks, or make Integer64 producer LocalLRC available.

## Artifacts

Results, logs and binaries are outside Git under `../benchmark-results/`:

- `callback-worker-builds-20261006` and `callback-worker-paired-20261006`.
- `callback-pipeline-builds-20261006` and `callback-pipeline-ieee-qc-20261006`
  retain the initial candidate and its failed broad regression.
- `callback-pipeline-final-builds-20261006` and
  `callback-pipeline-final-ieee-qc-20261006` contain the SDK-repaired candidate.
- `callback-pipeline-final-paired-20261006` retains the interrupted load experiment.
- `callback-pipeline-headroom-builds-20261006` and
  `callback-pipeline-headroom-ieee-qc-20261006` contain the final server/SDK build
  and complete regression evidence.
- `callback-pipeline-headroom-paired-20261006` contains the successful final
  twenty-pair SDK comparison.
- `portico-callback-pipeline-20261006` contains the fresh cross-RTI comparison,
  including every pair, the runtime manifest and normalized semantic evidence.

The server used before the headroom correction has SHA-256
`a48aea2cd6896e5f82d8c1450ceea8b70041ded6c158883e3afb9d8f3f704d77`.
The final async server has SHA-256
`90893f4492b12e105359c83581d7fff9c15e481fd62301bd0cdb4e85c3670dad`.
The final Go participant has SHA-256
`a13ece3f3e6b987a99efa7aa40b90d6006171b1ef63fb7c2fe8d91199da876cd`.
The `source-complete.tgz` archive in the headroom build directory has SHA-256
`e3581b55c70dda1c8815b4586ccf52170111a16a35902d3c342090f160ae0fa1`.
