# Coding Conventions (Strict)

These conventions are **non-negotiable**. CI enforces what it can; the orchestrator enforces the rest at code review. PRs that violate these rules are rejected without inline comments — fix and resubmit.

This document is the **single source of truth** for code style. `AGENTS.md` and the per-agent briefs reference it; they do not duplicate it.

---

## 0. Universal Rules

These apply to every language used in the project.

| Rule | Enforcement |
|---|---|
| **U-1** No emojis in code, comments, commit messages, or PR descriptions. | grep in CI |
| **U-2** No commented-out code. Delete it; git history is the archive. | review |
| **U-3** No `TODO`, `FIXME`, `XXX` without a tracked issue reference: `// TODO(#123): ...` | grep in CI |
| **U-4** No `print` / `println` / `fmt.Println` debug statements in committed code. | grep in CI |
| **U-5** Trailing whitespace forbidden; files end with exactly one newline. | pre-commit hook |
| **U-6** UTF-8, LF line endings (no CRLF). | gitattributes |
| **U-7** No binary files in source tree except deliberate fixtures under `tests/` or `examples/`. | review |
| **U-8** Default to writing **no comments**. Explain WHY only when non-obvious. Never explain WHAT. | review |
| **U-9** Single logical change per commit. No "and also fixed Y" commits. | review |
| **U-10** Function cyclomatic complexity ≤ 15. Refactor or extract before exceeding. | linter |
| **U-11** File length: Go ≤ 500 LOC, Python ≤ 400 LOC. Larger = split the file. | linter / review |
| **U-12** Imports grouped: stdlib / third-party / local, separated by blank lines. | linter |
| **U-13** No `panic` / `raise Exception(...)` for control flow. Errors are values / typed exceptions. | review |

---

## 1. Determinism Discipline (Cross-cutting)

The reproducibility guarantee (SRS NFR-DET-1, NFR-DET-2) is load-bearing. Every line of code in the **core path** (anything that touches federation state, the event log, or wire output) must obey:

| Rule | Why |
|---|---|
| **D-1** No wall-clock reads. Use `core.Clock` (Go) / `Clock` protocol (Python). `RealClock` in production, `FakeClock` in tests. | Reproducibility |
| **D-2** No iteration over Go maps without sorting keys first when iteration order affects output. Use `slices.Sorted(maps.Keys(m))` or equivalent. | Reproducibility |
| **D-3** No reliance on goroutine scheduling order. If order matters, serialize through a channel/queue with explicit ordering. | Reproducibility |
| **D-4** Stable tie-break on equal HLA timestamps: federate handle → object handle → attribute handle. Never (handle, pointer address) or anything per-process. | Reproducibility |
| **D-5** RNG seeded deterministically (federation name + federate handle, or configured seed). Never `math/rand` global, never `random.random()` without seeding. | Reproducibility |
| **D-6** No `os.Getenv` / `os.environ` reads in core path. Read at startup; pass values down. | Testability |
| **D-7** No global mutable state in core packages. Inject dependencies through constructors. | Testability + reproducibility |
| **D-8** Determinism tests run **at least 10 iterations** asserting byte-identical output. Single-iteration determinism tests will be rejected. | Catches latent flake |

CI runs a determinism harness across PRs touching core packages; it will catch most violations of D-1, D-2, D-5.

---

## 1.5 Test-Driven Development (Methodology)

All production code in `rti/` (Go) and `pysdk/` (Python) is developed **test-first**. Tests precede the implementation that satisfies them in commit history. The full playbook — Red-Green-Refactor cycle, commit-order patterns, test classification, domain examples, mutation sanity, anti-patterns, PR self-check — is in **`docs/TDD.md`**. Read it before opening any feature PR.

The coverage targets in §2.5 / §3.7 are necessary but not sufficient. The harder bar:

> **Mutation sanity**: removing or altering any single new line of implementation causes at least one new test to fail with a meaningful message.

The orchestrator may manually mutate code at review (flip `<` to `<=`, swap `min`/`max`, drop a `slices.Sort`) to confirm. PRs that survive mutation are rejected.

Exemptions (no test-first required): generated code, vendored data, CI scripts, trivial constructors, logging emission, `main` packages. Full list in `docs/TDD.md` §6.

---

## 2. Go Conventions

### 2.1 Formatting & Linting

- `gofmt` + `goimports` mandatory; CI fails on unformatted code.
- `golangci-lint run --config .golangci.yml` must be clean.
- Enabled linters (project config): `errcheck`, `govet`, `staticcheck`, `revive`, `gocyclo` (max 15), `gosec`, `misspell`, `goconst`, `unused`, `gocritic`, `forbidigo` (forbids `time.Now`, `fmt.Println`, `panic` outside whitelist).
- Tab indentation (Go default).

### 2.2 Naming

- Packages: lowercase, single-word, no underscores: `federation`, `eventlog`, `encoding`. Avoid `util`, `common`, `helpers` — name by responsibility.
- Exported identifiers: `CamelCase`. Acronyms uppercase: `FOM`, `RTI`, `LBTS`, `TSO` — but only at word boundaries (`FOMParser`, not `FomParser`; `parseFOM`, not `parseFom`).
- Unexported identifiers: `camelCase`.
- Receiver names: 1–3 chars, consistent across the type's methods (`(f *Federation)` always, never sometimes `(fed ...)`).
- Interfaces named for behavior, not "I"-prefix: `Codec`, not `ICodec`. Single-method interfaces end in `-er` when natural (`Encoder`).
- Test functions: `TestXxx_Condition_ExpectedBehavior` — e.g. `TestParser_MissingDataType_ReturnsFOM001`.

### 2.3 Error Handling

- Errors are values. No panics in core code paths. Panic only on invariant violations the program can never recover from, with the invariant in the message: `panic("invariant: federate registry is nil after init")`.
- Wrap with context using `%w`: `fmt.Errorf("federation %q join: %w", name, err)`.
- Sentinel errors named `ErrXxx`, exported only when callers must match on them: `var ErrFederationExists = errors.New("federation exists")`.
- Typed errors when they carry data: `type ValidationError struct { Code string; Field string; Msg string }`. Implement `Error() string`.
- Wire errors from gRPC handlers map to codes in `proto/rti/v1/errors.proto`. Never invent a code locally.
- Do not log AND return an error — the caller logs at the boundary. Pick one.

### 2.4 Concurrency

- Every public function that performs IO or blocks takes `ctx context.Context` as the first parameter.
- Prefer channels for handoff between goroutines, mutexes for protecting state.
- Document the synchronization strategy in a one-line package doc comment: `// Package federation: state mutated only by Manager goroutine; all queries via channel.`
- No goroutine pools or worker patterns "for performance" without a benchmark proving need.
- No `sync.Once` for lazy initialization in core packages — initialize explicitly in constructors.
- Never start a goroutine without a mechanism to stop it (context, done channel, or `errgroup`).

### 2.5 Testing

- Tests live next to code: `foo.go` + `foo_test.go`.
- **Table-driven tests** for any function with >2 distinct cases: `tests := []struct{...}{...}` then `for _, tc := range tests { t.Run(tc.name, ...) }`.
- Use `t.Parallel()` where safe (no shared mutable state).
- Use `testdata/` directory for fixture files. Reference via `testdata/foo.xml`.
- **No mocking frameworks** — write small fakes inline or as test helpers. Mocks rot.
- Coverage targets per package:
  - `rti/pkg/encoding`: ≥ 85%
  - `rti/pkg/fom`: ≥ 75%
  - `rti/internal/time`: ≥ 80%
  - `rti/internal/federation`, `declaration`, `object`, `eventlog`: ≥ 80%
- Integration tests live in `*_integration_test.go` with `//go:build integration` tag.
- Determinism tests live in `*_determinism_test.go` and run via `go test -tags=determinism -run=Determinism -count=10`.

### 2.6 Comments

- Package doc: 1–3 sentences at the top of one file per package, what + why-it-exists.
- Public exported godoc: full sentence starting with the identifier name, what + when-to-use. No fluff.
- Internal: write none unless WHY is non-obvious. Never explain WHAT.

Examples:

```go
// Package eventlog persists every federation-crossing message in TSO.
// It is the determinism guarantee — replay reads from this log and
// re-feeds the RTI to reproduce a run byte-identically.
package eventlog

// LBTS computes the Lower Bound on Time Stamp across regulating federates.
// Returns +Inf if no federate is regulating.
func LBTS(...) Time { ... }

// Iteration must be deterministic — federate handles are sorted to make
// the LBTS calculation reproducible across processes with different map
// hash seeds.
sortedHandles := slices.Sorted(maps.Keys(regulating))
```

### 2.7 Logging

- `log/slog` with JSON handler. Never `log.Println`, never `fmt.Println`.
- Always include structured fields where applicable: `federation`, `federate_handle`, `seq`, `phase`.
- Log levels:
  - `Debug` — verbose mode only; per-message detail.
  - `Info` — lifecycle events (federation create/destroy, federate join/resign).
  - `Warn` — recoverable anomalies (slow federate, malformed message ignored).
  - `Error` — failures that affect correctness (federation halted).

```go
slog.InfoContext(ctx, "federate joined",
    "federation", fedName,
    "federate_handle", h,
    "federate_name", name,
)
```

### 2.8 Forbidden Patterns

- `init()` functions that do anything other than register tiny things; never IO, never mutate global state heavyweight.
- `interface{}` / `any` in public APIs except where genuinely needed (codec returning decoded value).
- `panic` / `recover` for control flow.
- `time.Now()`, `time.Since()` in core path. Use `core.Clock`.
- `os.Exit` outside `cmd/`.
- `os.Getenv` in core path. Read in `cmd/`, pass down.
- `math/rand` package without seeding.
- `fmt.Sprintf` for log messages — use slog structured fields.

---

## 3. Python Conventions

### 3.1 Formatting & Linting

- `black` formatting with line length **100**.
- `ruff check` with project config (`ruff.toml`); rule set includes E, F, W, B, C4, I, N, UP, S (security), RET, SIM, PIE, ASYNC.
- `mypy --strict` clean. No `Any` in public APIs; use `object` or generics.
- 4-space indentation.

### 3.2 Naming

- PEP 8: `snake_case` for functions/variables/modules, `CamelCase` for classes, `UPPER_SNAKE` for constants.
- Acronyms uppercase: `FOMParser`, `RTIConnection`.
- Module names lowercase, no underscores when single-word: `encoding`, `fom`. Multi-word allowed: `pyjevsim_bridge`.
- Test files: `test_<module>.py`. Test functions: `test_<condition>_<expected>`.

### 3.3 Type Hints

- All public APIs fully typed.
- Use `from __future__ import annotations` for forward refs without quotes.
- Prefer `list[X]`, `dict[K, V]`, `X | None` (Python 3.10+) over `List[X]`, `Optional[X]`.
- `Final` for module-level constants: `MAX_FEDERATES: Final[int] = 1024`.
- `TYPE_CHECKING` guard for import-only-for-types to avoid circular imports.
- Generic protocols where structural typing fits.

### 3.4 Error Handling

- All custom exceptions inherit from `RtiError(Exception)` (defined in `pysdk/rti1516e/errors.py`).
- Typed exceptions per category:

```python
class RtiError(Exception): ...
class FederationNotFound(RtiError): ...
class FederateAlreadyJoined(RtiError): ...
class FOMValidationError(RtiError):
    def __init__(self, code: str, msg: str, *, line: int | None = None) -> None: ...
```

- Never `raise Exception(...)`. Never `except Exception` without re-raising or narrow handling.
- Error messages identify the federation, federate, and operation: `f"federation {fed!r} join: {reason}"`.

### 3.5 Async

- All IO uses `async`/`await`. No blocking calls in async code paths.
- pyjevsim is sync; isolate it with `asyncio.to_thread(...)` at the bridge boundary.
- Cancellation: every long-lived task respects `CancelledError` and cleans up.
- No `asyncio.run()` inside library code; only in entrypoints / tests.

### 3.6 Data Structures

- `dataclass` with `frozen=True` for value types crossing module boundaries.
- `slots=True` on hot dataclasses.
- No `dict[str, Any]` as a structure across modules — define a dataclass.
- `enum.Enum` / `enum.IntEnum` for closed sets, not string constants.
- `tuple` for fixed-shape returns; `NamedTuple` if 3+ fields.

### 3.7 Testing

- `pytest` + `pytest-asyncio`. Tests under `pysdk/tests/`.
- Use `@pytest.mark.parametrize` for table-driven cases.
- Use `pytest.fixture` for setup; never module-level test state.
- Mark async tests `@pytest.mark.asyncio`.
- Coverage target on owned packages: ≥ 80%.
- Determinism tests: `pysdk/tests/determinism/` run 10× with byte-equality assertion.

### 3.8 Forbidden Patterns

- Mutable default arguments (`def f(x=[]):`).
- Module-level mutable state (no module-level `dict`, `list` that gets mutated).
- `from x import *`.
- Bare `except:`.
- `print()` for logging; use `logging` module.
- `time.time()` / `datetime.now()` in core path; use `Clock` protocol.
- `random.random()` without seeding from federation context.
- `sys.exit` outside CLI entrypoints.
- Monkey-patching pyjevsim or any third-party module.

### 3.9 Logging

- `logging` module with JSON formatter (configured at SDK init).
- Logger per module: `logger = logging.getLogger(__name__)`.
- Structured fields via `extra={...}`: `logger.info("federate joined", extra={"federation": name, "federate_handle": h})`.

---

## 4. Project-Specific Naming

These names are fixed across Go and Python; do not invent variants.

| Concept | Identifier |
|---|---|
| Federation | `Federation` (class), `federation` (var) |
| Federate | `Federate`, `federate` |
| Federate handle | `FederateHandle` (typed), not raw int |
| Object handle | `ObjectHandle` (typed) |
| Attribute handle | `AttributeHandle` (typed) |
| Interaction class handle | `InteractionClassHandle` (typed) |
| Parameter handle | `ParameterHandle` (typed) |
| HLA logical time | `LogicalTime`, sometimes `HLATime` |
| Lookahead | `Lookahead` |
| LBTS | `LBTS` (acronym uppercase) |
| FOM | `FOM` |
| MIM | `MIM` |
| TSO / RO | `TSO`, `RO` |
| Event log entry | `Event` |
| Event sequence number | `Seq` (Go) / `seq` (Python) |

Error code prefixes:

| Domain | Prefix |
|---|---|
| FOM parsing/validation | `ERR_FOM_*` |
| Federation lifecycle | `ERR_FED_*` |
| Object management | `ERR_OBJ_*` |
| Time management | `ERR_TIME_*` |
| Encoding | `ERR_ENC_*` |
| Transport / wire | `ERR_WIRE_*` |

Numbered diagnostics (e.g. `FOM-001`, `FOM-002`) are documented in `docs/idd.md` §1.2.

---

## 5. Versioning

- Pre-1.0: breaking changes allowed. No backwards-compat shims (per AGENTS.md anti-goals).
- After 1.0: SemVer. `proto` changes follow Buf breaking-change rules.
- Wire protocol version field in every gRPC envelope; version mismatch = explicit error code `ERR_WIRE_VERSION_MISMATCH`.

---

## 6. Self-Check Before Opening a PR

Run these locally (Makefile targets exist):

```bash
make fmt          # gofmt + goimports + black
make lint         # golangci-lint + ruff
make typecheck    # mypy --strict
make test         # go test + pytest
make determinism  # 10x determinism harness on touched core paths
```

If any fail, fix before opening the PR. CI runs the same checks; do not waste an orchestrator review on a red PR.
