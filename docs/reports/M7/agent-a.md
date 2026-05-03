# Agent A — M7 W1 — Time-advance primitives (NMRA + TAR + TARA + FQR)

Cut-2 milestone M7. Adds the four IEEE 1516.1-2010 §8 time-advance
primitives that M3 (cut 1) deferred behind `ErrNotImplemented` stubs:

- `NextMessageRequestAvailable` (NMRA) — §8.12
- `TimeAdvanceRequest`          (TAR)  — §8.10
- `TimeAdvanceRequestAvailable` (TARA) — §8.11
- `FlushQueueRequest`           (FQR)  — §8.13

All four ride on the M3 W2 LBTS + grant-emission machinery; the per-mode
semantic divergence is captured in a single `decideGrant` pure function
that `tryGrantPending` consults.

## Outcome

- All 9 spec tests in `rti/spec/M7/` GREEN (4 sentinel-flipped, 4
  grant-condition, 1 determinism harness with 20 randomised scenarios).
- All M3 spec tests STILL GREEN — NER regression check passes.
- `go test ./...` GREEN across the repo.
- `go test -race ./rti/internal/time/... ./rti/spec/M3/... ./rti/spec/M7/...` clean.
- `golangci-lint run ./rti/internal/time/... ./rti/spec/M7/...` clean.

## Key design decisions

### Generalising `nerState` → mode-tagged request

Kept the `nerState` struct name (and the `pendingNER` field name) for
binary compatibility with M3 W3's stall detector and the existing
`regulatingSnapshot` consumer. Added:

```go
type nerState struct {
    currentTime   core.LogicalTime
    pendingNER    bool          // "any mode pending" — name retained
    requestedTime core.LogicalTime
    mode          AdvanceMode   // NEW: which §8.x primitive produced it
    pendingSince  stdtime.Time
}
```

Rationale: a wholesale rename to `pendingRequest` would have touched
~60 call sites across `ner.go`, `stall.go`, and the agent-owned tests
without semantic gain. The doc-comment on the struct explicitly notes
the historical naming and points readers to `mode`.

### Per-mode grant condition — `decideGrant`

The five primitives differ in two orthogonal axes:

|        | Full-grant predicate | Forced/incremental grant when LBTS < req |
| ------ | -------------------- | ---------------------------------------- |
| NER    | `LBTS > req`         | sole-pending → grant @LBTS, KEEP pending |
| NMRA   | `LBTS >= req`        | sole-pending → grant @LBTS, KEEP pending |
| TAR    | `LBTS > req`         | grant @LBTS unconditionally, CLEAR pending |
| TARA   | `LBTS >= req`        | grant @LBTS unconditionally, CLEAR pending |
| FQR    | `LBTS >= req` *      | grant @LBTS unconditionally, CLEAR pending * |

`*` Cut-2 simplification (see below).

Both axes are encoded as 1-line predicates (`mode.inclusiveLBTS()`,
`mode.allowsForcedGrant()`, `mode.allowsIncrementalGrant()`). The
`decideGrant` pure function consults them and returns `(fire, time,
clearPending)`. `tryGrantPending` no longer hard-codes any per-mode
semantics — it iterates handle-sorted candidates, calls `decideGrant`
per candidate, emits, and restarts on emission.

### Forced-grant escape hatch — divergence per mode

The M3 W2 NER "sole-pending → grant @LBTS, KEEP pending" path is
preserved verbatim for NER and EXTENDED to NMRA (the spec test
`TwoRegulators_GrantWaits` from M3 has direct analogues in M7 that
depend on this).

For TAR / TARA / FQR there is no forced-grant escape hatch — the TAR
family is "advance-as-far-as-LBTS-allows" by design, so the
`allowsIncrementalGrant()` path supplants the sole-pending gate
entirely. The grant fires whenever `LBTS > currentTime` (forward
progress), regardless of how many peers are pending. Pending always
clears on TAR/TARA/FQR grant — they're "one request → one grant"
primitives.

### FQR — cut-2 simplification

Cut-1/cut-2 has no real TSO message queue (TSO buffering is a cut-3
deliverable per `docs/srs.md` §10). FQR therefore behaves like TARA in
cut-2: inclusive-LBTS predicate, incremental grant at LBTS. The mode
is recorded distinctly (`ModeFQR`) so cut-3 can extend the dispatcher
without API churn — the public method signature is already correct.

This is documented in (a) the `FlushQueueRequest` method docstring
in `manager.go`, (b) the `AdvanceMode` enum doc-comment in `advance.go`,
and (c) the `TestDecideGrant_FQR_BehavesLikeTAR` unit test.

### Pre-dispatch sentinel tests — flipped

The four `Test*_NotImplementedYet` tests in `rti/spec/M7/` were
pre-dispatch sentinels that asserted `errors.Is(err, ErrNotImplemented)`.
The orchestrator's intent comment on the TAR sentinel reads "Agent A's
M7 work flips this to a real test." All four were flipped to assert
that the dispatcher's eligibility check fires
(`core.ErrTimeNotRegulating`) instead, which is the correct
post-implementation contract.

The functional spec tests (`*_GrantsImmediately`, `*_GrantBoundedByLBTS`,
`*_GrantAtLBTSEqualsT`, `*_DrainsQueueAndGrants`) were left unchanged;
their `t.Skip(... M7 RED state)` blocks now never fire because the
implementation succeeds.

### Determinism harness — `TestSpec_M7_Determinism_20RandomizedScenarios`

20 scenarios, each with a distinct seed drawn from a master seed. Per
scenario:

- 2-5 regulating federates (random handles 1..N).
- Lookaheads in [0, 3]; ~25% draw lookahead 0 to exercise the
  inclusive-LBTS path that distinguishes NMRA / TARA from NER / TAR.
- 10-30 advance operations, each picking a random federate + random
  primitive from {NER, NMRA, TAR, TARA, FQR}.
- Per-federate "monotonic target" tracker keeps requested times above
  the lookahead floor (avoids `ErrTimeRequestInPast` as scenario noise).
- Each scenario runs twice; the SHA-256 of a canonical
  outbox-then-event-log byte trace must match.

Every scenario is marked `t.Parallel()` — concurrent execution is part
of the test, not just a speedup.

## Files touched

Modified:
- `rti/internal/time/manager.go` — replaced four `ErrNotImplemented`
  stub bodies with one-line delegations to `dispatchAdvance`.
- `rti/internal/time/ner.go` — added `mode` field to `nerState`;
  refactored `tryGrantPending` to delegate per-mode semantics to
  `decideGrant`; turned `nextMessageRequest` into a one-liner that
  delegates to `dispatchAdvance(ctx, fed, h, t, ModeNER)`. Updated
  `emitGrant` to clear `mode` alongside `pendingNER` on full grant.
- `rti/spec/M7/{tar,tara,nmra,fqr}_test.go` — flipped the four
  `*_NotImplementedYet` sentinel assertions per the orchestrator's
  intent comment ("Agent A's M7 work flips this to a real test").
- `rti/spec/M7/determinism_test.go` — replaced the `t.Skip` scaffold
  with the 20-scenario randomised harness.

Created:
- `rti/internal/time/advance.go` — the `AdvanceMode` enum, the
  per-mode predicate helpers, the pure `decideGrant` function, and
  the unified `dispatchAdvance` entry point.
- `rti/internal/time/advance_test.go` — 17 agent-owned unit tests
  covering `decideGrant` per-mode semantics, dispatcher pre-flight,
  duplicate-pending cross-mode rejection, and the "no stale mode after
  TAR" regression check.
- `docs/reports/M7/agent-a.md` — this report.

NOT touched (per file-ownership rule):
- `rti/internal/core/timemgr.go` (orchestrator-frozen interface).
- `rti/internal/time/{lbts,regulation,stall,halt,lookahead,grant}.go`
  — the M3 W1A/W1B/W2/W3 code is reused as-is.
- `rti/spec/M3/*` and any non-`rti/` package.

## Test counts

```
rti/spec/M7  — 9 tests, 9 pass (4 sentinel-flipped, 4 functional, 1 determinism × 20 sub-tests = 29 sub-tests total)
rti/spec/M3  — 11 tests, 11 pass (NER regression — most important gate)
rti/internal/time — 39 tests, 39 pass (17 new in advance_test, 22 pre-existing)
go test ./... — every package GREEN
go test -race ./rti/internal/time/... ./rti/spec/M3/... ./rti/spec/M7/... — clean
golangci-lint — clean
```

## Branch + commit

- Branch: `agent/a/m7-w1-time-primitives`
- Commit SHAs: see `git log` on the branch.

DO NOT MERGE — pending orchestrator review per the cut-2 dispatch
model.
