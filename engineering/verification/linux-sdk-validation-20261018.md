# Linux SDK Validation, 18 October 2026

## Scope and environment

Validated the current working tree on branch `codex/ieee2010-feature-completion`
(base commit `4852206`), including the Go/Python/Java workflow cleanup.
This is a working-tree result, not a result for the base commit alone.

- Ubuntu 24.04 under WSL 2, native Linux filesystem for builds and service tests.
- Go 1.22.2 with the race detector; Python 3.12.3 in an isolated environment.
- Temurin JDK 11.0.32.1 and Maven 3.9.16.
- Buf 1.71.0; Go/Python bindings generated with Buf, Java bindings with Maven.
- Servers built from current source, loopback endpoints, external log/save directories.
- No C++ compiler, bindings, or interoperability tests were used.

## Results

| Check | Result |
|---|---|
| Go default build, full race suite | 79 test packages passed; 3,512 top-level tests and 7,005 subtests passed |
| Go optional `callback_async` race suite | 3 packages passed; 969 top-level tests and 2,357 subtests passed |
| Python HLA inventory, including live services | 2,127 passed, no skips |
| Java clean build, SDK and live services | 581 passed, 7 skipped, no failures or errors |
| Internal IVCT-derived subset | 35 passed |
| Validation runner and configuration tests | 18 passed |
| Go vet, Go formatting, Buf lint | Passed after corrections |
| Linux-generated Go bindings versus local generated bindings | Identical |
| Descriptor/RPC compatibility tests | Passed, including the generator-metadata-only projection |
| Ruff for `pysdk/rti1516e` and changed validation helpers | Passed |
| Strict mypy for `rti1516e` | Passed, 46 source files |
| Release metadata, shell syntax, changed-file whitespace | Passed |
| Full configured `golangci-lint` job | Failed; 306 reported findings |
| Ruff across the complete Python working tree | Failed; 346 findings in separate RL code/tests |
| Strict mypy across both Python packages | Failed; 796 errors in 19 separate RL files |

Go's default run skipped 16 tests for the optional asynchronous callback build
and 3 external FDD-schema oracles. The asynchronous tests were exercised in
the separate tagged run. Eleven Go packages had no test files; they are not
counted as passed test packages. The final default run supplied a freshly
built server to the DDM tests that require `GORTI_COMPLETION_DDM_ENDPOINT`.

The seven Java skips comprise one external IEEE Java API archive comparison,
three external FDD-schema checks, and three externally supplied cross-binding
fixture checks. The ordinary live Java/Python interoperability tests ran.

## Corrections made during validation

1. Replaced four uses of `testing.B.Loop` with timed `b.N` loops, preserving
   compatibility with the declared Go 1.22 minimum.
2. Added Python setup and SDK dependencies to the Go CI job, whose integration
   tests launch a Python federate. The first Linux run failed because `grpcio`
   was missing from that subprocess environment.
3. Removed a developer-specific Windows PDF path from order-completion evidence
   recording. All eight service scenarios still run without the private PDF.
   An optional `GORTI_IEEE1516_1_SOURCE_PDF` verifies the reviewed document hash;
   missing or incorrect explicitly supplied files fail. Four new tests cover
   this behavior. Evidence states whether the document was verified in that run.
4. Aligned `callback_report.proto`'s `go_package` metadata with the other files.
   Buf's existing `M` mappings continue to generate imports for
   `internal/adapters/protobuf`. A projection test reproduces the previous
   descriptor hash by restoring only that option; RPC signatures are unchanged.
5. Named the `Lower`/`Upper` fields in 115 DDM test literals and used `proto.Clone`
   instead of copying a Protobuf message containing internal synchronization state.
6. Applied missing Go formatting and simplification. Increased the Makefile's
   configurable Go test timeout to five minutes; the full server package took
   about 61 seconds on Linux, exceeding the previous 60-second limit.

## Remaining publication checks

The supported SDK service tests pass, but the configured CI is not all green.
The Go lint report contains dependency-rule findings, unchecked errors,
complexity/duplication warnings, unused code, and other diagnostics in the
existing implementation/refactoring changes. Per-linter reporting limits were
left enabled, so 306 is the reported count, not an exhaustive debt inventory.
The lint configuration also retains pre-refactoring generated-code and clock
paths; those rules need review against the current package boundaries.

The Python whole-tree failures are in uncommitted RL continuation and model
work, not the HLA SDK. That work was not modified or bundled with this task.

The current CI changes depend on a substantial uncommitted HLA refactoring,
service implementation, and the new Java SDK. Publishing only the CI files
would omit those dependencies. Commit scope confirmation was requested; no
commit or push was performed while that scope and the remaining lint failures
were unresolved. Existing staged changes and local artifacts were preserved.

## Evidence and reproduction

Logs and JUnit reports are outside the repository:

- Windows copy: `D:/workspace/gorti/sdk-validation-20261018/`.
- Linux workspace and results: `/home/cbchoi/gorti-validation-20261018/`.
- `linux-go-final.jsonl`, `linux-go-async.jsonl`, `linux-go-vet.log`.
- `linux-python-final.xml`, `linux-python-retry.log`.
- `linux-java-final-reports/`, `linux-java-final.log`, `linux-ivct-final.xml`.
- `linux-golangci.json`, `linux-ruff.log`, `linux-mypy.log`.

From a Linux checkout with Go, Python dependencies, Java 11, Maven, and Buf:

```sh
buf generate
python scripts/validate_sdks.py python java ivct --artifacts /tmp/gorti-sdk-validation
go test -race -count=1 -timeout=5m ./rti/... ./tests/... ./examples/... \
  ./verification/gorti-go/... ./verification/gorti-go-fair/...
go test -race -tags=callback_async -count=1 -timeout=5m \
  ./rti/pkg/federate/... ./rti/internal/application/server/... \
  ./rti/internal/adapters/grpc/...
go vet ./rti/... ./tests/... ./examples/... \
  ./verification/gorti-go/... ./verification/gorti-go-fair/...
```

For the external-server DDM cases, set `GORTI_COMPLETION_DDM_ENDPOINT` to a
current local `rtid` listener before running Go tests. Optional IEEE documents
and schema artifacts remain separate from the reproducible SDK service suite.
