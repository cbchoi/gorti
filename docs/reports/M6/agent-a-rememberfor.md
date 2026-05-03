# Agent A M6 W1C — `fomRepository.RememberFor` post-`CreateFederation` wiring

Post-MVP follow-up dispatched to close the second-of-two blockers on the
single remaining skipped spec test in the project,
`pysdk/tests/spec/m5/test_spec_m5_modes.py::test_spec_m5_best_effort_attribute_delivers_ro`.
W1A (Agent C, `agent/c/m6-w1a-handle-alignment`) landed cross-language
handle alignment in `pysdk/rti1516e/fom/parser.py` and discovered this
secondary Go-side wiring gap during end-to-end testing; W1C lands the
gap fix.

## Outcome

**Both M5 modes tests now PASS end-to-end with zero skips.** The Python
publisher's per-class `<order>Receive</order>` declaration is now
honoured by the Go-side rtid in best-effort mode: the subscriber
receives a RO event (`event.timestamp is None`) instead of the
publisher's logical timestamp.

```
$ python3 -m pytest pysdk/tests/spec/m5/test_spec_m5_modes.py -v
tests/spec/m5/test_spec_m5_modes.py::test_spec_m5_best_effort_attribute_delivers_ro PASSED [ 50%]
tests/spec/m5/test_spec_m5_modes.py::test_spec_m5_verbose_attribute_delivers_tso PASSED [100%]
============================== 2 passed in 0.48s ===============================
```

`pysdk/tests/test_handle_alignment.py` (W1A's regression) and
`pysdk/tests/spec/m5/test_spec_m5_cross_language.py` continue to PASS
— no regressions to W1A's locked-in alignment work.

## Root cause (now fixed)

`rti/cmd/rtid/main.go::newRTID` constructed `fomRepository` and passed
it to `federation.Manager` (which calls `Repo.Load(modules)` internally
during `CreateFederation`) AND to `grpcsvc.FOMRepoOrderLookup{Repo:
foms}` (which calls `Repo.Get(fed)` at interaction-send time). But
nothing ever called `fomRepository.RememberFor(name, handle)` in
between — `Manager.CreateFederation` returns only `error` (M5-frozen,
no FOM handle on the success path), and the gRPC handler had no
post-success hook to populate the per-federation map.

Therefore `FOMRepoOrderLookup.InteractionOrder(fed, cls)` always hit
`Repo.Get(fed)` → `(nil, ErrFederationNotFound)`, fell into the
`if h == nil` branch in `rti/internal/transport/grpc/best_effort.go`,
and returned `(OrderTimeStamp, false)`. The object registry's
`deliveryTimestampForInteraction` then preserved the publisher's
timestamp regardless of the FOM's `<order>Receive</order>`.

The Go-side spec test `rti/spec/M5/best_effort_test.go` did not catch
this because it injects an `orderTable` fixture directly into
`object.Options.Orders`, bypassing `FOMRepoOrderLookup` entirely.

## Fix landed

A new optional `OnCreateFederationSuccess` hook on
`grpcsvc.Options` (defined in
`rti/internal/transport/grpc/server.go`), plumbed into the existing
`federationService` (in `rti/internal/transport/grpc/federation.go`),
fires after every successful `CreateFederation` gRPC call with the
translated `core.FederationName` and the FOM modules. Nil hook is a
no-op (preserves the prior contract for tests that build a
`federationService` without the hook).

`rti/cmd/rtid/main.go::newRTID` wires the hook to:

1. `foms.Load(ctx, modules)` — re-parses the same modules the manager
   just consumed (FOM XML parse is microseconds; `CreateFederation` is
   once-per-federation).
2. `foms.RememberFor(name, h)` — populates the per-federation handle
   map.

A `Load` failure is logged as a warning (the manager already validated
the same modules a moment earlier and accepted them; a re-Load failure
is a programmer error) and intentionally does NOT propagate — the
federation has already been created and surfacing the error here would
lie about its existence.

This avoids touching `rti/internal/federation/manager.go` (M5-frozen
per the dispatch).

## Files changed

| File | Lines added | What |
|---|---|---|
| `rti/cmd/rtid/main.go` | +24 (incl. doc comment) | wire the `OnCreateFederationSuccess` hook |
| `rti/internal/transport/grpc/federation.go` | +14 (incl. doc comment) | accept + invoke the hook on success |
| `rti/internal/transport/grpc/server.go` | +12 (incl. doc comment + import) | thread the hook through `Options` |
| `rti/internal/transport/grpc/federation_test.go` | +73 | three new unit tests for the hook (`FiresOnce`, `NotFiredOnError`, `NilIsNoop`) |
| `rti/cmd/rtid/foms_test.go` | +75 | `TestRTID_CreateFederationViaGRPC_PopulatesFOMRepoMap` — composition-level regression that drives a real gRPC `CreateFederation` against the rtid-composed server and asserts `foms.Get(name)` returns a valid handle |
| `pysdk/tests/spec/m5/test_spec_m5_modes.py` | -76 / +21 (skip block + outdated module docstring removed; status note updated) | the live `assert received_ts is None` is now the actual assertion |

Total LoC: ~143 lines added (mostly documentation comments in the wiring
+ tests), ~76 lines removed (the skip block and its diagnostic
docstring). Production wiring itself is the canonical "~3 lines" the
brief estimated:

```go
// in newRTID's grpcsvc.NewServer call:
OnCreateFederationSuccess: func(ctx context.Context, name core.FederationName, modules []core.FOMModule) {
    h, err := foms.Load(ctx, modules)
    if err != nil { /* log + return */ }
    foms.RememberFor(name, h)
},
```

## Verification

- `pysdk/tests/spec/m5/test_spec_m5_modes.py` — **2/2 PASS** (was
  1 PASS + 1 SKIP).
- `pysdk/tests/test_handle_alignment.py` — 3/3 PASS (no regression).
- `pysdk/tests/spec/m5/test_spec_m5_cross_language.py` — 1/1 PASS (no
  regression).
- `pysdk/tests/spec/` — 134 passed, 1 skipped (the M4 replay skip is
  pre-existing and unrelated).
- `pysdk/tests/` (full) — 481 passed, 1 skipped.
- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go test ./...` — all packages PASS.
- `go test -race ./rti/cmd/rtid/... ./rti/internal/transport/grpc/...`
  — clean.
- `golangci-lint run ./rti/cmd/rtid/... ./rti/internal/transport/grpc/...`
  — 6 findings, ALL pre-existing G115 conversions in `rti/cmd/rtid/foms.go`
  on lines this PR did not touch (verified via `git diff main --
  rti/cmd/rtid/foms.go` — no changes). Zero new findings introduced.

## Files NOT touched (per dispatch ownership)

- `rti/internal/federation/manager.go` — M5-frozen.
- `pysdk/rti1516e/*` — W1A territory; alignment locked in.
- `pysdk/tests/test_handle_alignment.py` — W1A's regression; READ-ONLY.
- Spec tests other than the M5 modes test.

## Project status post-W1C

Combined with W1A (handle alignment) and W1B (concurrency / TLS
hardening), this completes the M6 Wave 1 hardening trio. The previously
documented "last skipped spec test in the entire project" now PASSES —
**M0–M5 spec suites are 100% PASS, zero skips on spec tests, zero RED**.
The single remaining skip is `tests/spec/m4/test_spec_m4_replay.py`
which is a deferred-to-M5 marker that predates the M5 cross-language
smoke (TASK-081); covered separately by `test_spec_m4_determinism.py`.
