# How to add a new alternative strategy implementation

Status: Phase 4 reference guide — companion to `docs/research-platform.md`.

This guide walks a researcher through adding a new alternative
algorithm-level strategy to gorti without forking the codebase.
The pattern is intentionally short: one `alt_*.go` file in the owning
package, one line in `research.Default()`, optionally a TOML stanza,
done. The Phase-4 in-tree alts (`max-projected`, `eager`,
`random-acquirer`) are the canonical templates — copy any of them.

Audience: a researcher who has read `docs/research-platform.md` and
wants to swap in their own LBTS, grant, or ownership-negotiation
algorithm. No knowledge of the registry internals required.

---

## 1. Pick the strategy slot

Phase-2 froze three algorithm-level strategy interfaces. Pick whichever
one corresponds to the algorithm you want to swap:

| Slot | Interface | File | What it controls |
|---|---|---|---|
| `time.lbts` | `time.LBTSStrategy` | `rti/internal/time/strategy.go` | LBTS computation over the regulating set |
| `time.grant` | `time.GrantStrategy` | `rti/internal/time/strategy.go` | per-request grant decision (per-mode predicate, forced-grant escape hatch) |
| `ownership.negotiation` | `ownership.NegotiationStrategy` | `rti/internal/ownership/strategy.go` | who-wins decision at the three Manager swap-points |

Each interface declares three methods:

- The algorithm hook (`LBTS`, `DecideGrant`, `SelectAcquirer`).
- `Name() string` — the registry key used in TOML config.
- `DeterminismPreserving() bool` — see §4 below.

The interfaces are FROZEN: don't change them. Phase 2 cemented the
shapes so registry + TOML wiring would stay stable across alts.

---

## 2. Create the file

Drop a new file in the owning package, named `alt_<name>.go`. The
`alt_` prefix is reserved for alternative implementations and gives a
grep-friendly handle.

For example, an LBTS alt named "lookahead-pinned" goes in
`rti/internal/time/alt_lookahead_pinned.go`:

```go
package time

import "github.com/cbchoi/gorti/rti/internal/core"

// lookaheadPinnedLBTS is an LBTSStrategy that ... (one paragraph
// describing what the algorithm does, when a researcher would want it,
// and whether it preserves determinism).
type lookaheadPinnedLBTS struct{}

func (lookaheadPinnedLBTS) LBTS(set []RegulatingFederate) core.LogicalTime {
    // ... your algorithm here.
}

func (lookaheadPinnedLBTS) Name() string                  { return "lookahead-pinned" }
func (lookaheadPinnedLBTS) DeterminismPreserving() bool   { return true /* or false */ }

var _ LBTSStrategy = (*lookaheadPinnedLBTS)(nil)

// LookaheadPinnedLBTSStrategy is the constructor the research registry
// calls in Default().
func LookaheadPinnedLBTSStrategy() LBTSStrategy { return lookaheadPinnedLBTS{} }
```

Keep the type unexported and expose a constructor — it lets you change
the underlying type (e.g. add private state) without breaking callers.

---

## 3. Register it in `research.Default()`

Open `rti/internal/research/registry.go` and add ONE line in `Default()`:

```go
_ = r.RegisterLBTS("lookahead-pinned", timepkg.LookaheadPinnedLBTSStrategy())
```

Update the doc comment block listing pre-registered alts so the next
researcher can find yours by reading the constructor.

The codebase uses explicit `Register*` calls in `Default()` rather than
package `init()` because:

- `Default()` is the single grep-able list of every alt the rtid
  knows about.
- Registration order is deterministic — important when two alts share
  internal state (none today, but future ones might).
- Tests that build a `NewRegistry()` (empty) instead of `Default()`
  get a known-clean starting point with zero magic.

---

## 4. Mark `DeterminismPreserving()` honestly

`DeterminismPreserving() bool` must return:

- `true`  — when same inputs always produce same outputs across runs,
            machines, and goroutine schedules.
- `false` — when the impl uses ANY of the following:
            `math/rand`, `crypto/rand`, `time.Now()`, unsorted `map`
            iteration, `sync.Map.Range`, goroutine race conditions,
            external network/disk state, OS scheduling.

The flag is consumed by:

- `research.Apply()` in strict mode — rejects boot if any wired alt
  reports `false`. See §3.2 + §8 of `docs/research-platform.md`.
- M3/M4 replay test fixtures under `per-impl-opt-in` mode — skip with
  reason if any wired alt reports `false`.

Lying here is a bug: a non-preserving impl claiming `true` will pass
strict mode and then fail replay determinism in production conformance.
A preserving impl claiming `false` is a wasted strategy slot
(strict mode rejects it for no reason).

Edge case: a non-conservative-but-deterministic alt (the `max-projected`
LBTS, the `eager` grant) returns `true` because the algorithm itself is
a pure function of its inputs. The fact that it violates HLA causality
is a CORRECTNESS issue, not a determinism one — strict mode is about
replay determinism, not about spec compliance. If you want correctness
gating, write a regression suite under `rti/research/<name>/` per
design doc §8.5.

---

## 5. Opt in via TOML research-config

A `--research-config <file.toml>` flag on `rtid` reads a TOML document
that selects which alt is wired. The flag is already live (Phase 3,
`rti/cmd/rtid/main.go`).

Minimal stanza for the lookahead-pinned alt:

```toml
# Top-level determinism mode: "strict" | "per-impl-opt-in" | "off".
# Default when absent: "per-impl-opt-in".
determinism = "per-impl-opt-in"

[time]
lbts = "lookahead-pinned"   # your alt
grant = "default"           # leaves the default wired

[ownership]
negotiation = "default"
```

Run:

```sh
rtid --research-config myconfig.toml
```

Omitting any field falls back to the package default; an empty TOML
file resolves to behavior identical to running `rtid` with no flag at
all. This is the hard guarantee: default-config behavior is invariant
across phases.

A typo in the alt name (`lbts = "lookahed-pinned"`) yields a clear
boot-time error naming the missing key. A typo in the TOML key
(`lbtss = "..."`) yields a clear unknown-field error. Silent
misconfiguration is impossible.

---

## 6. Determinism gate behavior

The gate is enforced in `rti/internal/research/apply.go` `Apply(cfg, reg)`.
Behavior summary (full table in `docs/research-platform.md` §3.2):

| `determinism` | Non-preserving alt selected | Behavior |
|---|---|---|
| `strict` | yes | `Apply` returns `*NonPreservingError`; `cmd/rtid` exits non-zero |
| `strict` | no | `Apply` succeeds; replay tests run normally |
| `per-impl-opt-in` (default) | yes | `Apply` succeeds; replay tests skip with reason |
| `per-impl-opt-in` | no | `Apply` succeeds; replay tests run normally |
| `off` | yes or no | `Apply` succeeds; replay tests skip unconditionally |

The `Resolved.AllPreserving()` helper is what replay-test fixtures
consult — see `docs/research-platform.md` §8.

---

## 7. Add a unit test

Drop `alt_<name>_test.go` next to `alt_<name>.go`. Pin three things at
minimum:

- `Name()` matches the string you registered with.
- `DeterminismPreserving()` matches what you claim in the doc comment.
- The alt produces a DIFFERENT output than the default on at least one
  non-trivial input. (If it never differs, why is it an alt?)

For non-preserving alts add a fourth test in
`rti/internal/research/apply_test.go` proving `Apply(strictCfg, reg)`
returns `*NonPreservingError` naming your alt — this guards against a
future regression silently flipping the determinism flag and disabling
the strict-mode gate.

---

## 8. Worked example, end-to-end

The Phase-4 `eager` GrantStrategy is the cleanest concrete walkthrough.
All three artifacts ship in this repo.

### 8.1 The TOML stanza

`docs/examples/eager-grant.toml` (illustrative; not committed):

```toml
determinism = "per-impl-opt-in"

[time]
lbts = "default"
grant = "eager"

[ownership]
negotiation = "default"
```

Run:

```sh
rtid --research-config docs/examples/eager-grant.toml
```

### 8.2 The alt file

`rti/internal/time/alt_eagergrant.go` (excerpt):

```go
type eagerGrant struct{}

func (eagerGrant) DecideGrant(c GrantContext) GrantDecision {
    return GrantDecision{
        Fire:         true,
        Time:         c.Requested,
        ClearPending: true,
    }
}

func (eagerGrant) Name() string                { return "eager" }
func (eagerGrant) DeterminismPreserving() bool { return true }

var _ GrantStrategy = (*eagerGrant)(nil)

func EagerGrantStrategy() GrantStrategy { return eagerGrant{} }
```

### 8.3 The registration

`rti/internal/research/registry.go`, in `Default()`:

```go
_ = r.RegisterGrant("eager", timepkg.EagerGrantStrategy())
```

### 8.4 The test

`rti/internal/time/alt_eagergrant_test.go` (excerpt):

```go
func TestEagerGrant_DiffersFromDefault_OnNoProgressInput(t *testing.T) {
    ctx := GrantContext{Mode: ModeNER, CurrentTime: 5, Requested: 7, LBTS: 5, SolePending: true}

    def := DefaultGrantStrategy().DecideGrant(ctx)
    alt := EagerGrantStrategy().DecideGrant(ctx)

    if def.Fire {
        t.Errorf("default: fired %+v, want hold (no progress)", def)
    }
    if !alt.Fire || alt.Time != core.LogicalTime(7) {
        t.Errorf("eager: %+v, want fire@7", alt)
    }
}
```

### 8.5 What the researcher sees

- `go test ./rti/internal/time/... -run EagerGrant` passes locally
  before the registry change is even made (the strategy is
  self-contained).
- `go test ./rti/internal/research/...` passes because the new
  registration is purely additive — every existing test that uses the
  `default` strategy keeps using the default.
- `rtid --research-config eager-grant.toml` boots and runs with eager
  grants for time-advance.
- `rtid` (no flag) boots with the conservative default, behavior
  bit-identical to the previous release.

---

## 9. Cross-references

- `docs/research-platform.md` §3.2 — determinism contract definition.
- `docs/research-platform.md` §4.1 — naming conventions (`alt_*.go`).
- `docs/research-platform.md` §6.1 / §6.3 — the time + ownership
  algorithm-level extension points.
- `docs/research-platform.md` §7.2 — registry + TOML config design.
- `docs/research-platform.md` §8 — full determinism gate semantics.
- `docs/research-platform.md` §9 — phase table; Phase 4 deliverables.

In-tree reference impls (copy-and-edit starting points):

- `rti/internal/time/alt_maxprojected.go` (LBTS, preserves determinism)
- `rti/internal/time/alt_eagergrant.go` (Grant, preserves determinism)
- `rti/internal/ownership/alt_randomacquirer.go` (Negotiation,
  DOES NOT preserve determinism)
