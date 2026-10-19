# Development guide

Run commands from the repository root.

## Architecture

Federates use Go, Python, or Java SDKs to call the standalone `rtid` server.
Go LocalLRC performs local admission and queues eligible receive-order traffic.
The server validates operations and owns authoritative federation state.

| Path | Responsibility |
|---|---|
| `proto/rti/v1/` | Wire schema |
| `rti/cmd/rtid/` | Executable entry point |
| `rti/internal/hla/` | HLA service state, algorithms, and typed callbacks |
| `rti/internal/application/` | Configuration, composition, lifecycle, and bootstrap |
| `rti/internal/adapters/grpc/` | RPC validation, service invocation, error mapping, callback streams |
| `rti/internal/adapters/protobuf/` | Generated Go wire types |
| `rti/internal/adapters/delivery/` | Atomic recipient reservation, outboxes, batching, backpressure |
| `rti/internal/adapters/eventlog/` and `savebundle/` | Journal and snapshot formats and storage |
| `rti/internal/adapters/sdkstate/` | SDK checkpoint validation and identity translation |
| `rti/internal/compatibility/` and `rti/experiments/` | Research configuration and non-default algorithms |
| `rti/pkg/federate/` | Go SDK, LocalLRC, callback dispatch and reporting |
| `pysdk/` | Python SDK and pyjevsim bridge |
| `javasdk/` | IEEE Java API, generated wire module, and provider |
| `rti/tests/integration/` and `tests/` | Integration, conformance, and architecture tests |
| `benchmark/` and `verification/` | Workloads, comparison runners, and analysis |

HLA packages cannot import gRPC, generated Protobuf, filesystem, HTTP, or
metrics packages. `tests/architecture` checks that dependency rule. Unit tests
also live beside their owning Go packages; the refactoring did not move every
test into a separate tree.

Read the current [SDD](specifications/current/SDD.md) and
[IDD](specifications/current/IDD.md) before changing a service. Preserve:

- federation-generation and member-incarnation checks;
- atomic recipient reservation before committing multi-recipient delivery;
- required timestamp-order callbacks before a time grant;
- explicit local admission, server completion, and callback execution boundaries;
- callback recovery state and compatible snapshot decoding.

## Setup and build

Use Go 1.22+, Python 3.11+, and Buf. Java work requires JDK 11+ and Maven 3.9.
Make targets use POSIX utilities; run them in Linux, macOS, WSL, or Git Bash.
Direct Go, Python, and Maven commands also work in PowerShell.

```bash
python -m venv .venv
. .venv/bin/activate
python -m pip install -e "./pysdk[dev,rl]" -r scripts/validation-requirements.txt
buf generate
go build ./...
mvn -f javasdk/pom.xml verify
```

For the locked Python environment, use the [Python SDK commands](../pysdk/README.md#verification).
`make build` writes `bin/rtid` and `bin/rti-top`. Experimental DDS builds use
`make build-dds` and require Cyclone DDS.

## Protocol generation

Edit `proto/`, then run:

```bash
make proto
buf lint
buf breaking --against ".git#branch=main"
git diff -- rti/internal/adapters/protobuf
mvn -f javasdk/pom.xml verify
```

| Output | Generator | Git policy |
|---|---|---|
| `rti/internal/adapters/protobuf/` | Buf | Commit with the schema |
| `pysdk/rti1516e/_generated/` | Buf or `python -m rti1516e._proto` | Ignored; regenerate before tests or packaging |
| `javasdk/wire/target/` | Maven | Ignored |

`make proto` runs both generators. C++ generation is no longer part of Buf or
the default validation commands. Retained C++ source and reports are historical.
The Go plugin import mappings in `buf.gen.yaml` place every wire type in the
adapter package without rewriting historical `go_package` descriptor options.
Add a mapping when adding a schema file; the configuration test checks the set.

## Tests

| Command | Purpose |
|---|---|
| `go test ./rti/internal/hla/time -run TestName` | Focused service regression |
| `go test ./tests/architecture` | Source dependency rules |
| `go test ./...` | Module-wide Go tests |
| `make go-test` | Race-enabled maintained Go package roots |
| `make determinism` | Repeated deterministic execution |
| `make py-test` | Python HLA inventory, including live-server cases |
| `make py-lint` / `make py-typecheck` | Ruff / mypy |
| `mvn -f javasdk/pom.xml verify` | Java unit and transport tests |
| `make java-test` | Java unit, transport, live-server and Python interoperation tests |
| `make sdk-test` | Python/Java tests and internal IVCT-derived subset |
| `make validation-test` | Workflow, generation, runner and version metadata tests |
| `bash scripts/ci-gates.sh` | `stubs go python java static ivct` stages |
| `make docs` | Strict MkDocs build |

Enable live-server and fixture settings from the SDK guides when running
integration campaigns. The [full service report](verification/ieee2010-full-service-rerun-post-sync-20261004.md)
records exact test selections and prerequisites. The
[requirements inventory](verification/requirements2010/README.md) connects
service requirements to assertions and execution results.

`make verify` runs formatting, lint, tests, and determinism. It can modify
formatting and skips optional lint tools that are absent. Test prerequisites
must be installed. Run type checking, documentation, and protocol checks
separately when affected.

For PowerShell or a focused SDK run, use:

```sh
python scripts/validate_sdks.py python java ivct
```

The runner builds current `rtid` source, selects that binary for live tests,
starts and stops the Java test server, and writes reports outside the repository.
Use `--artifacts PATH` to retain them at a chosen location. `--maven PATH` or
`GORTI_MAVEN` selects Maven; `GORTI_MAVEN_REPO` selects its local cache.
The HLA inventory explicitly excludes legacy C++, RL/PPO and standalone model
applications. The `rl` extra is needed for whole-package mypy checks, not HLA
service execution. Java source-archive/schema diagnostics and cross-binding
checkpoint fixture tests require their separately supplied inputs; JUnit reports
show them as skipped when those inputs are absent.

## Callback performance builds

The default Go build confirms callback reports synchronously. Build both the
server and Go application with `-tags=callback_async` to enable negotiated
local callback entry and pipelined reporting for eligible receive-order data.
Use `DispatchEvent` and `FlushCallbackInvocationReports` to preserve entry,
completion, and recovery accounting. See [LocalLRC](../docs/local-lrc.md).

## CI and release

| Workflow | Trigger | Checks |
|---|---|---|
| `docs.yml` | Documentation PRs, main pushes, manual | Strict MkDocs build and Pages artifact; no deployment job |
| `ci.yml` | Manual | Go, Python, Java, metadata, benchmark contract, and Protobuf checks |
| `conformance.yml` | Manual | Go race, Python/Java live SDK, architecture and internal IVCT-derived checks |
| `release.yml` | `v*` tag | Release preflight and GoReleaser |
| `pypi.yml` | `v*` tag or manual build | Generated bindings and Python packaging |

The manual Go job checks that regeneration matches the committed Go bindings.
Its configured lint version is 1.61.0. The Java job builds generated wire types
with Maven and checks the generated ambassador facade. CI and release preflight
use the same SDK runner as local validation.

For documentation edits, follow [documentation publishing](maintenance/documentation.md).
For releases, follow the [release guide](maintenance/release.md). Keep raw
execution logs and local paths out of Git.
