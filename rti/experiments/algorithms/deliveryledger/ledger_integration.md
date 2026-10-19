# R3 Experimental Delivery Accounting Boundary

Status: tested experimental foundation, not a completed R3 feature or production
RO/TSO total accounting. This prototype lives in
`rti/experiments/algorithms/deliveryledger` as package `deliveryledger`.
The second-batch prototype changed no production hooks, server metric sources,
core contracts, SDKs or wire schema. The third-batch adapter integration below
is a separate custody diagnostic, not promotion of this experimental ledger.
This is an engineering contract, not a new IEEE conformance disposition.

The prototype has no production caller. It remains outside runtime pending the
required shared-envelope and callback-reporting protocol. Do not import it into
production merely to expose server-only counts. An outbox-only diagnostic would
need a separate custody boundary and a handoff terminal outcome: dequeue is
neither callback invocation nor cancellation in this ledger.

## Third-Batch Production Outbox Slice

`rti/internal/adapters/delivery` now contains an opt-in outbox-custody observation
port. `NewObservedWithBatch(capacity, batchSize, flushInterval)` enables it;
`ObserveQueue(federation, handle)` returns an epoch-bound observation with
`Inspect() OutboxQueueCounts`. Existing constructors keep it disabled. The
parent-owned server composition selects the observed constructor when DEBUG
logging is enabled at startup; otherwise accounting remains disabled. Changing
the log level later does not change that constructor choice.

The real `MultiOutbox.Send`, single/multi-recipient reservation commit, deferred
flush, close/unbind and restore commit paths call these hooks. No object import,
event wrapper, producer-sequence identity, forwarding channel or new goroutine
is introduced. Tests additionally drive a real time manager into this outbox:
failed drains retain time custody, retry admits once, and pre-handoff retraction
never becomes an outbox admission. The experimental ledger still has no runtime
caller and is not imported by the adapter.

The separate engineering identity, counting **all outbound envelopes**, is:

    admitted = drained + invalidated + scratch + queued

Drained means a batch was removed from the outbox channel, not transport-send
success, SDK enqueue, invocation or ACK. A ring of `channel capacity + 1` batch
lengths reconciles FIFO receives under an observation lock, preserving exact
event counts for short grant/timer batches without retaining payload identities.
Inspect samples channel length while all publishers are excluded. Snapshots
are per-recipient, not an atomic federation-wide or cross-stage view.

Reservations/rejections do not admit anything until custody is acquired.
Observation does not change admission capacity, fanout reservation locking,
event type/order, timer retry, or close's existing final-flush behavior. Restore
counts lengths actually removed by the purge, so concurrent reader drains are
not confused with invalidation. Commit closes the old observation and creates
a fresh one for the restored handle/epoch; Release leaves it unchanged.

Close invalidation ends observation at a channel-length sample. It does **not**
claim cancellation: existing channel batches may still be read after cancel or
unbind, exactly as before. Closed observations detach their channel and batch
metadata and retain only scalar counts. Rebind creates a distinct observation.

Disabled hooks perform nil checks but allocate no accounting state, take no
accounting lock, update no accounting counters and start no worker. The hot
Send/commit/flush paths do not add deferred-unlock bookkeeping. Focused allocation
tests and before/after reservation benchmarks retain the original allocation
counts. Literal zero CPU overhead is not established, and must not be claimed.

The parent-owned resign hook captures the current binding's observation under
exclusive lifecycle admission and unbinds the outbox. It emits a DEBUG log with
`scope=outbox-only` only when the captured snapshot is already closed. Concurrent
stream cancellation can unmap a binding before finishing closure; an open
snapshot is suppressed without waiting, retrying, or starting a goroutine.
This best-effort log is not a complete history of retired epochs, a membership
metric, or a MOM total source.

The current read handle scopes a local binding/epoch; it does not carry
authenticated `core.MemberIdentity.Generation/Incarnation`. Any future external
membership-scoped diagnostic collector must acquire the existing membership lease,
pair the observation with the current MemberIdentity, revalidate that identity,
and retire that association on resign/destroy or committed restore. Do not look
up old observations by a reused numeric handle alone. An explicit identity-bound
bind/read extension remains subject to parent interface approval.

Neither this port nor the experimental ledger populates HLAROlength/HLATSOlength.
The required shared identity, time-queue accounting, transport correlation,
authenticated SDK enqueue and terminal reporting, recovery and normative metric
boundary review below remain open. No SDK/protobuf/core changes are introduced
by this diagnostic; the server integration is limited to constructor selection
and retired-local-epoch logging described above.

Third-batch evidence is outside the repository:
`D:/workspace/gorti/ieee2010-batch3-r3-handoff.md` records exact changed files,
commands, behavioral RED, final Windows/race/vet results, diagnostic benchmarks,
review/reflection and the parent-owned membership/protocol proposal.

## Experimental Contract

`NewDeliveryLedger(DeliveryScope, capacity)` creates one bounded ledger for a
federation generation, federate incarnation and callback epoch. Capacity must
be 1..MaxOutstanding (1,048,576). This experiment owns that bound and does not
import the production delivery adapter. The initial callback epoch may be zero.

- `Admit(event, stage)` records one committed per-recipient callback and returns
  an opaque process-local ticket. Released reservations and failed admission
  must never call it. Reflect, receive and remove callbacks are the currently
  selected data-event class. Discovery, grants, retraction notifications,
  internal deletion markers and other control callbacks are excluded.
- Each callback gets its own ticket even when producer sequences are equal or
  one update was split. Tickets are not derived from `OutboundEvent.Seq()`.
- `Transfer(ticket, from, to, receivedOrder)` changes only location/order, not
  admission count. Scratch/channel batching and frame splitting retain tickets.
  Only time-buffer release may reclassify TSO to actual RO reception. Failed
  drain or reservation attempts leave the ticket at its original location.
  For an owned, retired ticket in an open epoch, late transfers are no-ops even
  with unspecified order or invalid stages. Closed epochs and foreign/unissued
  tickets are still rejected. Active tickets still validate all transfer data.
- `Settle(tickets, outcome)` validates an explicit batch atomically, then retires
  each ticket once. Invocation includes callbacks returning an exception.
  Cancellation means confirmed removal, not a sent requestRetraction callback.
  There is intentionally no producer-ACK or high-watermark-as-invocation API.
  Failed terminal observations for the active epoch must remain pending for
  retry or cause `LoseCoverage()`; `Settle` errors do not do that automatically.
  Retry the observation, never the already-invoked callback or canceled event.
- `Close()` invalidates outstanding observations once and fences late reports.
  It does not claim cancellation of callbacks already running remotely. Keep
  the same ledger/tickets for a genuinely resumable reconnect; close only when
  its observed epoch is abandoned. Restore and rejoin create new ledgers.
- Storage retains bounded outstanding metadata only, with no event payloads or
  terminal-ticket history. The first terminal observation wins; duplicate and
  conflicting late outcomes are no-ops, not an outcome audit trail.
- `LoseCoverage()` and missed admission/transfer observations make coverage loss
  sticky. Such errors must not break accepted delivery or replay callbacks.
  A rejected foreign/stale ticket does not invalidate the current ledger.

`Inspect()` is diagnostic for the supplied observations only. **Never expose its
RO/TSO fields as HLAROlength/HLATSOlength**, even with `CoverageLost == false`.
There is no coverage-completion certificate or MOM total-source implementation.
The production RO provider must remain unset and TSO totals unavailable.

The independently recomputed engineering identity is:

    admitted = invoked + canceled + invalidated + outstanding RO + outstanding TSO

Stage counts partition outstanding entries. No queue snapshots are added.

## Read Interface Decision

Keep `DeliveryAccounting` write-only. The experimental tests use the concrete
`Inspect()` method for detached diagnostic snapshots; there is no runtime read
consumer, so this change adds no speculative read interface or production export.
When a real diagnostic consumer exists, give it a separate consumer-owned read
interface with only `Inspect() ObservedDeliveryCounts`, not the write port or the
concrete ledger. That interface must preserve coverage/closed flags and must not
be treated as the MOM total-source contract. Eligibility for an end-to-end total
requires the independent coverage gate below, not just a readable snapshot.

## Required Parent-Owned Integration

1. **Identity-bearing internal transfer contract.** `core.OutboxDelivery`,
   `core.TSOBufferedDelivery`, the time buffer and the subscription's
   `[]core.OutboundEvent` currently do not carry a common accounting ticket.
   Adding a field to the reservation descriptors alone is insufficient because
   the subscription would lose it. A core-owned envelope/identity contract
   needs parent approval, preservation through save cloning, and encoder
   handling; a blind event wrapper would break current concrete type switches.
   The experimental package must not be imported into core/time/object to solve it.
2. **Admission and pre-fanout rollback.** Wire exactly one committed observation
   per recipient/split callback at the existing transaction boundaries:
   `object/update.go` (`preparedAttributeFanout.commit/abort`),
   `object/interaction.go` (`preparedInteractionFanout.commit/abort`), removal
   paths, `time/asyncdelivery.go` (`tsoBufferReservation.Commit`), and
   `delivery/outbox.go` (`Send`, `commitOutboxDeliveryLocked`). The ticket must
   exist before another consumer can observe the committed event. Time-to-outbox
   handoff must reuse it, never create a second admission. Ledger overflow is
   lost observation coverage, not a new delivery rejection.
3. **Time and actual received order.** Carry tickets through
   `takeBufferedTSO`, `restoreBufferedTSO`, `releaseBufferedTSO`,
   `drainAllBufferedTSO`, and `deletion_lifecycle.go:deliverBufferedEvent`.
   Observe successful handoff and its final ReceivedOrder. RetractMessage and
   RetractMessageNotify must settle the tickets actually removed, including
   all affected recipients. Internal deletion markers remain excluded.
4. **Transport and receipt correlation.** Preserve tickets through
   `grpc/stream.go` encoding, coalescing and `sendEventBatchChunks` splits.
   Correlate the ticket with `grpc/stream_receipts.go` / `CallbackReceipts.Prepare`
   before delivery becomes observable. Record the committed Outbox -> Transport
   handoff before the SDK can observe that envelope. Only actual receipt outcome
   application may settle invocation. Stream `Send` success or
   `commit(sent=true)` proves neither SDK enqueue nor callback invocation;
   failed/ambiguous sends or lost receipt capacity require coverage invalidation,
   not invented completion. Authenticate reports using existing membership and
   lifecycle admission. Stale reports must never match new membership state.
5. **SDK enqueue, outcome coverage and reporting protocol.** Keeping Transport
   and CallbackQueue as separate stages requires an explicit authenticated SDK
   enqueue observation, emitted only after the callback queue accepts the
   identity-bearing envelope. Apply that observation as Transport -> CallbackQueue
   using its correlated ticket and received order. Server stream-send completion
   cannot synthesize it. A profile with no observable queue must define its actual
   ownership boundary or merge these stages in a future reviewed design; it cannot
   claim the current four-stage diagnostic is covered. Go's
   `pkg/federate/callback_receipt.go` supports explicit invocation reports but
   simply receiving on Events does not invoke a callback. Python's
   `rti1516e/standard.py` and C++'s `src/RtiAmbassador.cpp` report selected actual
   invocations. All advertised callback models need reviewed handling for
   disabled queues, omitted handlers, exceptions, explicit discard, retraction
   and replay. `proto/rti/v1/support.proto` currently has a unary single-receipt
   invocation report, not a negotiated enqueue/invocation/cancellation progress
   contract. Existing confirmed-object/LocalLRC ACKs are producer-operation
   admission, not this contract. Parent-owned SDK/protocol work must define
   batching, gap/cancellation semantics and legacy capability coverage. Enqueue
   and terminal observations need epoch-bound identities, ordered application,
   acknowledgement only after ledger application, and idempotent retry after a
   lost acknowledgement. The reporting adapter must preserve per-ticket handoff
   order and deduplicate retries already superseded by a later stage, with bounded
   pending/reordering storage. The ledger alone is not a reorder buffer. A terminal
   observation may retire a ticket before a delayed enqueue report arrives; that
   later transfer is a no-op and must not resurrect it or lose coverage.
   Lost correlation, unrecoverable sequence gaps, retry exhaustion, pending-buffer overflow or
   abandoned active-epoch terminal reports require `LoseCoverage()`. Failed
   terminal application must stay pending and retry with the same identity/outcome,
   or invalidate coverage; silently dropping a `Settle` error is not acceptable.
   Foreign/stale reports rejected at the authentication/lifecycle boundary must
   not poison an unrelated current epoch. Do not replay user callbacks to repair
   telemetry, add synchronous per-event calls, or alter the shared wire independently.
6. **Lifecycle and persistence.** Compose ledgers with committed generation and
   MemberIdentity, and coordinate `delivery/restore_transition.go`,
   `time/persistent_state.go`, receipt clearing and SDK epoch invalidation.
   Close only on committed invalidation, not Prepare/Release. Saved time events
   require new-epoch accounting reconstruction; process-local tickets cannot be
   serialized/reused. Reconnect is not implicit drain: preserve tickets if
   continuity is proved, otherwise keep totals unavailable. Resign/destroy must
   remove ledger ownership without allowing reused handles to accept old reports.
7. **MOM total gate.** Parent owns `server/mom_metrics_sources.go` and composition.
   Supply neither total until independent production-path conservation tests
   prove every selected stage and negotiated client profile. A healthy diagnostic
   snapshot, time-buffer length, outbox batch depth, or receipt-pending count is
   insufficient. Metric-boundary normative review remains separate.

## Verification And Limits

- Initial RED: six new ledger contract tests failed behaviorally against an
  inert interface scaffold, not against a claimed pre-existing outbox defect.
- Review RED: a regression demonstrated that an invalid/missed transfer returned
  an error while falsely retaining healthy observation coverage. The fix makes
  this loss sticky without changing event counts.
- Pre-relocation GREEN: 49 delivery package tests, including 15 new ledger
  tests, passed on Windows and Linux/WSL with the race detector; zero skips or
  race warnings.
  Go vet passed. These are historical results at the original package path.
  The deterministic independent history model checks 4,000
  operations; concurrency tests include duplicate outcomes racing epoch close.
- Relocation GREEN: all 15 tests passed under
  `./rti/experiments/algorithms/deliveryledger` on Windows and Linux/WSL with the
  race detector, with zero skips or race warnings. The experimental package's
  Go vet check passed. The unchanged production delivery package separately
  passed its remaining 34 tests. The move adds no production export or consumer.
- The full-suite build exposed an introduced test import cycle:
  `hla/object` tests imported `adapters/delivery`, while the original
  `ledger_events.go` imported `hla/object`. Relocation removes that production
  import edge. The complete `./rti/internal/hla/object` package now builds and
  passes without moving or splitting any existing test.
- Independent review found that late transfers of retired tickets validated
  order/stages first and could incorrectly lose coverage. Regression RED captured
  eight failures across invoked/canceled tickets and four malformed transfer
  cases, while active-ticket validation and foreign/closed-ticket controls passed.
  Moving the retired-ticket check after ownership validation and before transfer
  metadata validation fixes the defect without weakening those assertions.
- Retired-transfer GREEN: all 17 experimental tests pass on Windows and under
  Linux/WSL's race detector, with zero skips or race warnings. Go vet passes.
  The new cases verify whole snapshots remain unchanged after late transfers,
  including while an unrelated callback occupies the released capacity.
- No live cross-SDK total-source test, persistence reconstruction, complete
  callback-boundary review or performance campaign is claimed. Those integration
  checks remain open; this correction changes no production file or metric source.

Raw logs are outside the repository under `D:/workspace/gorti/`:
`ieee2010-batch2-r3-red.jsonl`, `ieee2010-batch2-r3-review-red.jsonl`,
`ieee2010-batch2-r3-green.jsonl`, `ieee2010-batch2-r3-race.jsonl`, and
`ieee2010-batch2-r3-vet.log`. `ieee2010-batch2-r3-green-initial.jsonl`
contains the first focused GREEN. A sandbox cache-access failure was rerun with
approved access; it is not behavioral RED evidence.

Post-relocation evidence: `ieee2010-batch2-r3-experimental-green.jsonl`,
`ieee2010-batch2-r3-experimental-race.jsonl`, and
`ieee2010-batch2-r3-experimental-vet.log`. The relocation is architectural
separation of a tested experimental foundation, not completion of R3. Required
shared-envelope, callback-reporting, recovery and normative-boundary work remains.
The import-cycle failure is recorded in `ieee2010-batch2-go-all.jsonl`; its
post-relocation object-package check is `ieee2010-batch2-r3-object-cycle-green.jsonl`.

Retired-transfer review evidence: `ieee2010-batch2-r3-retired-transfer-red.jsonl`,
`ieee2010-batch2-r3-retired-transfer-green.jsonl`,
`ieee2010-batch2-r3-retired-transfer-race.jsonl`, and
`ieee2010-batch2-r3-retired-transfer-vet.log`.
