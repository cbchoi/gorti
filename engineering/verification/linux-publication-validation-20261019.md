# Linux Publication Validation, 19 October 2026

## Publication scope

The candidate branch is `codex/go-python-java-validation`, based on remote
`main` at `5cf718a`. It contains the HLA service refactoring, service tests,
Go/Python/Java SDKs, supported-language workflows, and related documentation.
The SDK campaign started from source tree
`2353c684b2668e2e210abdacb1606e7f47297e14`. Final Go and lint validation used
`61c7f5fa0b1e1012ae11b2df498a22fb8655d748`; the only intervening source changes
were three line-specific analyzer comments, with no executable statement
changes. This report is updated after the test campaigns.

The original development branch contains 167 additional unpublished commits
from separate RL work. Those commits were not used as the publication base.
The original branch, working files, and pre-existing staged renames were
preserved. New RL/PPO implementations, examples, tests, dependencies, local
execution artifacts, and untracked `docs/assets` files were excluded. RL
files already present in remote `main` were not deleted.

Forty-nine absent, unselected RL entries were removed from the publication
candidate's Python test inventory. No selected HLA test was removed. The
supported Python packages pass whole-package static checks on this candidate;
the broader RL working-tree failures in the 18 October report do not describe
this candidate. Those earlier failures involved both unpublished commits and
uncommitted work, not only uncommitted files.

## Environment and results

Validation used Ubuntu 24.04 under WSL 2 with source on the native Linux
filesystem, Go 1.22.2, Python 3.12.3, Temurin JDK 11.0.32.1, Maven 3.9.16,
Buf 1.71.0, and golangci-lint 1.61.0. Each live test campaign built its RTI
server from the candidate source and used loopback listeners. No C++ build
or C++ interoperability test was included.

| Check | Result |
|---|---|
| Go default race suite | 80 test packages passed; 3,533 top-level tests and 7,020 subtests passed |
| Go `callback_async` race suite | 3 packages passed; 972 top-level tests and 2,357 subtests passed |
| Python HLA inventory and live services | 2,127 passed; no skips |
| Java clean build, SDK and live services | 588 total: 581 passed, 7 skipped; no failures or errors |
| Internal IVCT-derived subset | 35 passed |
| Validation runner and workflow configuration | 18 passed |
| Ruff across `pysdk/` | Passed |
| Strict mypy for `rti1516e` and `pyjevsim_bridge` | Passed; 59 source files |
| Go vet, formatting, Buf lint, generated Go bindings | Passed |
| Go coverage profile and function report | Generated; 64.4% aggregate statement coverage |
| Determinism target, ten repetitions with the race detector | Passed |
| Example entrypoints, release metadata, shell syntax | Passed |
| Benchmark contract checks | Passed; no performance experiment was run |
| MkDocs strict build | Passed |
| `make go-lint`, compared with `origin/main` | Passed; zero findings, also checked with reporting limits removed |

Go's default suite skips 16 optional asynchronous-callback cases and three
external FDD-schema checks. The asynchronous cases ran in the tagged suite.
Eleven Go packages contain no tests and are not counted as passing test
packages. With coverage enabled, Go reports nine of these as package passes
and two as package skips. The external-server DDM tests used a freshly built
local server. Coverage includes generated protocol code and is not a measure
of IEEE requirement coverage.

Java's seven skips are one external IEEE API archive comparison, three
external FDD-schema checks, and three external cross-binding fixtures.
Ordinary Java/Python interoperability tests ran against a real server.
These internal results are not an external IEEE certification.

## Corrections found during publication validation

1. The Java example's `run.sh` lacked Git mode `100755`. Its executable bit
   is now recorded, and the Linux entrypoint check passes.
2. Release validation installed the Python SDK after running Go tests even
   though Go interoperability tests launch Python peers. Installation now
   precedes Go tests; a workflow regression assertion checks that ordering.
3. The initial complete Go run failed in `TestGoTimedEndToEnd`. A federate
   started advancing before another process completed time-management
   initialization, making the late participant's fixed first TAR target
   invalid. The example now accepts `--participants`; the three-process
   launchers use an HLA synchronization point before the first advance.
   Callback entry and outcome are dispatched through the SDK. The integration
   test deliberately delays one join by one second. The revised example
   passed five consecutive race runs, its targeted lint check, and the
   subsequent full Go suite. The final small handler extraction was followed
   by another five race runs and targeted lint. Server time semantics and
   acceptance assertions were not relaxed.

## Go lint cleanup

The first lint run displayed 301 findings because reporting limits were
enabled. With those limits removed, the same branch comparison reported
997: depguard 124, errcheck 68, forbidigo 15, goconst 187, gocritic 21,
gocyclo 240, gosec 89, ineffassign 3, revive 134, staticcheck 15, and unused 101.

The cleanup separates complex operations into smaller functions, checks
previously ignored test/setup errors, removes unused code, names repeated
protocol values, and makes numeric bounds and intentional bit encodings
explicit. Focused regressions cover enum overflow, encoded time bits,
checkpoint identity, FOM bounds and decimal rates, and gRPC connection readiness.
Callback ordering, lock ownership, save/restore rollback and receipt recovery
were reviewed against the pre-cleanup source.

The lint profile still enables the same linters and complexity threshold and
compares changes with `origin/main`. Generated-code and clock-wrapper paths
now match the refactored tree. The dependency rules distinguish standalone
SDK libraries from the federate client, which requires the protocol codecs
and callback receipt profile. Other server internals remain forbidden in
production SDK packages, with an additional AST-based architecture test.
Integration tests may import their server harnesses. Line-specific exceptions
explain intentional signed bit encodings, checked conversions that the analyzer
cannot prove, and wall-clock measurements in transport tests; there is no
package-wide suppression of these checks.

The final Linux Go, lint, Python, Java, IVCT and static checks passed. These
are local executions of the validation commands, not a hosted GitHub Actions
result. The workflows remain manually triggered. Publication is confined to
`codex/go-python-java-validation`; the original development branch and remote
`main` remain unchanged.

## Reproduction and evidence

From a Linux checkout with the listed toolchain:

```sh
python -m pip install -e './pysdk[dev]' -r scripts/validation-requirements.txt
buf generate
python scripts/validate_sdks.py python java ivct --artifacts /tmp/gorti-sdk-validation
go test -race -count=1 -timeout=5m ./rti/... ./tests/... ./examples/... \
  ./verification/gorti-go/... ./verification/gorti-go-fair/...
go test -race -tags=callback_async -count=1 -timeout=5m \
  ./rti/pkg/federate/... ./rti/internal/application/server/... \
  ./rti/internal/adapters/grpc/...
python -m ruff check pysdk/
(cd pysdk && python -m mypy --strict rti1516e pyjevsim_bridge)
make go-lint
golangci-lint run --max-issues-per-linter=0 --max-same-issues=0 \
  ./rti/... ./tests/... ./examples/... ./verification/gorti-go/... \
  ./verification/gorti-go-fair/...
```

Set `GORTI_COMPLETION_DDM_ENDPOINT` to a freshly built local RTI listener
when running the Go external-server DDM tests. Logs, JUnit XML, binaries,
save bundles, and the compiled documentation site remain outside the repo:

- `D:/workspace/gorti/sdk-publication-20261019/`
- `/home/cbchoi/gorti-publication-20261019/lint-clean-evidence/go-final/`
- `/home/cbchoi/gorti-publication-20261019/lint-clean-evidence/sdks/`
- `/home/cbchoi/gorti-publication-20261019/lint-clean-evidence/`

The earlier failed Go run remains in `clean-go/`, and the pre-cleanup lint
report remains in `lint-unlimited.json`. Final totals count each test once;
repeat runs are not additional independent test cases.
