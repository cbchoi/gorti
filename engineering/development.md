# Development guide

Commands in this file run from the repository root unless noted otherwise.

## Architecture at a glance

gorti separates federate processes from the authoritative RTI process:

```text
federate model
    -> Go, Python, or C++ SDK
    -> confirmed RPC or pipelined stream
    -> rti/internal/transport/grpc
    -> federation service managers
    -> event log and per-federate outboxes
    -> ordered callback stream
```

The important ownership boundaries are:

| Path | Responsibility |
|---|---|
| `proto/rti/v1/` | Shared wire contract and source for generated bindings |
| `rti/cmd/rtid/` | Process lifecycle, configuration, and composition root |
| `rti/internal/core/` | Shared domain contracts, errors, clocks, and event interfaces |
| `rti/internal/federation/` | Federation generations and membership |
| `rti/internal/declaration/` | Publication and subscription state |
| `rti/internal/object/` | Object registry, interactions, validation, and fanout |
| `rti/internal/time/` | Logical time, TSO queues, LBTS, requests, and grants |
| `rti/internal/ownership/` | Attribute ownership transitions |
| `rti/internal/ddm/` | Dimensions, regions, and routing predicates |
| `rti/internal/mom/` | Management Object Model state and requests |
| `rti/internal/savepoint/` | Save/restore orchestration and storage |
| `rti/internal/eventlog/` | Ordered persistence, reading, and replay |
| `rti/internal/transport/grpc/` | RPC handlers, streams, ACKs, and callbacks |
| `rti/internal/research/` | Opt-in strategy registration without coupling service packages |
| `rti/pkg/federate/` | Standalone public Go federate client |
| `pysdk/` | Python SDK and pyjevsim bridge |
| `cppsdk/` | C++17 DLC-shaped SDK and conformance fixtures |
| `tests/` | Cross-package specification and conformance scenarios |
| `verification/` | Comparison harnesses and reproducibility evidence |

Service managers own mutable state by federation name and generation. The
transport validates and translates wire requests, but service rules belong in
the manager packages. Public client code under `rti/pkg/` must not import
`rti/internal/`; `.golangci.yml` enforces that boundary.

Before changing a service path, read the current
[SDD](specifications/current/SDD.md) and the package's `doc.go`. Preserve these
cross-cutting invariants:

- generation evidence fences stale work from a replacement federation;
- recipient capacity is reserved before a multi-recipient transition commits;
- timestamp-order callbacks are visible before the corresponding time grant;
- ACK, callback, persistence, and backpressure boundaries remain explicit; and
- committed ordering is deterministic and uses `core.Clock` where wall time
  would otherwise enter behavior.

## Toolchain and bootstrap

The common path needs Go 1.22 or later, Python 3.11 or later, Buf, and a POSIX
shell with Make. Make recipes use tools such as `find`, `xargs`, and `rm`, so
Windows contributors should run them in WSL or Git Bash. Direct Go and Python
commands can be run in PowerShell.

```bash
python -m venv .venv
. .venv/bin/activate
python -m pip install --upgrade pip
python -m pip install -e "./pysdk[dev]"

buf generate
make help
```

Buf uses remote plugins from `buf.gen.yaml`, so first-time generation requires
network access. C++ work additionally requires a C++17 compiler, CMake 3.18 or
later, Conan 2, and the gRPC, protobuf, and GoogleTest packages resolved by
`cppsdk/conanfile.txt`. Go lint uses golangci-lint 1.61.0 in CI.

## Build paths

The portable, DDS-free binaries are the default build:

```bash
make build
```

This writes `bin/rtid` and `bin/rti-top`. Use `make rti-top` when changing only
the TUI. The experimental DDS adapter is opt-in and requires Cyclone DDS:

```bash
make build-dds
make test-dds
```

For the C++ SDK, generate stubs first, then reproduce the CI dependency and
build path:

```bash
buf generate
conan profile detect --force
conan install cppsdk --output-folder=cppsdk/build --build=missing \
  -s build_type=Release -s compiler.cppstd=17
cmake -S cppsdk -B cppsdk/build \
  -DCMAKE_TOOLCHAIN_FILE=cppsdk/build/conan_toolchain.cmake \
  -DCMAKE_BUILD_TYPE=Release
cmake --build cppsdk/build -j
ctest --test-dir cppsdk/build --output-on-failure
```

The Linux conformance lane is the maintained C++ CI environment. See
[`cppsdk/README.md`](../cppsdk/README.md) for system-package builds and public
header layout.

## Protocol code generation

`proto/` is the only hand-edited protocol source. The canonical command is:

```bash
make proto
```

`make proto` runs `buf generate` and updates three consumers:

| Output | Version-control policy | Used by |
|---|---|---|
| `rti/internal/genproto/` | Committed | Go server, tools, and Go SDK |
| `pysdk/rti1516e/_generated/` | Ignored | Python runtime, tests, wheel, and sdist |
| `cppsdk/_generated/` | Ignored | C++ library and tests |

After a protocol change:

```bash
make proto
buf lint
buf breaking --against ".git#branch=main"
git diff -- rti/internal/genproto
```

Commit the Go binding changes with the `.proto` files. Regenerate ignored
Python and C++ bindings in every clean build environment. Do not hand-edit any
generated tree. The separate `make py-codegen` target uses the protobuf-7
compatible `grpcio-tools` version from the Python development extra.

## Test and quality gates

Start with the smallest command that covers the change, then broaden before a
pull request.

| Command | Scope |
|---|---|
| `go test ./rti/internal/time -run TestName` | Focused Go package or test |
| `make go-test` | Race-enabled Go tests for maintained package roots |
| `go test ./...` | All Go packages in the module; used by release preflight |
| `make determinism` | Ten race-enabled determinism runs |
| `make py-test` | Python SDK pytest suite |
| `make py-lint` | Ruff over `pysdk/` |
| `make py-typecheck` | Strict mypy over `pysdk/` |
| `make docs` | Strict MkDocs build into `site/` |
| `bash scripts/ci-gates.sh` | Full generated-stub, Go, C++, static, sweep, and IVCT gate |

The conformance script accepts ordered stage names: `stubs`, `go`, `cpp`,
`lockfile`, `static`, `sweep`, and `ivct`. The `cpp` stage needs the Conan
toolchain above; `sweep` and `ivct` need `bin/rtid`, so include the `go` stage
or build it first. `sweep` compares every fixture in both directions against
`cppsdk/tests/dlc/conformance/EXPECTED_VERDICTS.txt`; an undocumented
improvement fails just like a regression until the expected verdict is
reviewed.

Formatting and aggregate targets are:

```bash
make go-fmt-check
make lint
make typecheck
make verify
```

`make verify` runs `fmt`, `lint`, `test`, and `determinism`. It mutates files
through `gofmt`, optional `goimports`, optional Black, and optional Ruff fixes,
so inspect the resulting diff. It also skips tools that are optional in its
recipes and does not include `typecheck`, docs, protocol breaking checks, or
the C++ conformance gate. Run those explicitly when relevant.

The pre-commit configuration runs repository scripts that reject emojis in
source and debug `fmt.Print*` or Python `print()` calls. Go lint also protects
deterministic clock use, the public/internal import boundary, and newly
introduced lint findings relative to `origin/main`.

## CI map

The workflow trigger matters because not every lane runs on each pull request.

| Workflow | Trigger | What it checks |
|---|---|---|
| `.github/workflows/docs.yml` | Documentation-related PRs and pushes to `main`; manual | Strict MkDocs build, `site/index.html`, Pages artifact |
| `.github/workflows/ci.yml` | Manual | Go format/lint/coverage/determinism, Python Ruff/mypy/pytest/metadata, Buf lint/breaking |
| `.github/workflows/conformance.yml` | Manual | All `scripts/ci-gates.sh` stages on Ubuntu 24.04 |
| `.github/workflows/release.yml` | Pushed `v*` tag | Metadata, Go/Python/docs preflight, then GoReleaser |
| `.github/workflows/pypi.yml` | Pushed `v*` tag; manual build | Generated-stub package check and trusted PyPI publishing on tags |

For protocol changes, the manual validation workflow regenerates Go bindings
and fails if the committed output differs. Its Buf breaking check compares
against the local `main` branch from full checkout history.

## Change paths

- Go service change: update the owning manager package and focused tests, then
  run `make go-test`, `make determinism` when ordering is involved, and any
  affected cross-process scenario.
- SDK change: run that SDK's unit tests and a live-`rtid` integration path.
- Protocol change: update `proto/`, regenerate all languages, commit Go output,
  run Buf lint/breaking checks, and test every affected SDK.
- C++ surface change: run the CMake tests, lockfile stage, affected fixture,
  and full sweep before changing an expected verdict.
- Documentation change: follow the
  [documentation guide](maintenance/documentation.md) and run `make docs`.
- Release preparation: follow the [release guide](maintenance/release.md);
  never infer release readiness from a single aggregate target.
