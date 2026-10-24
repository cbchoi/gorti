# gorti

[![Manual validation](https://github.com/cbchoi/gorti/actions/workflows/ci.yml/badge.svg)](https://github.com/cbchoi/gorti/actions/workflows/ci.yml)
[![Documentation](https://github.com/cbchoi/gorti/actions/workflows/docs.yml/badge.svg)](https://github.com/cbchoi/gorti/actions/workflows/docs.yml)
[![Release](https://img.shields.io/github/v/release/cbchoi/gorti?display_name=tag)](https://github.com/cbchoi/gorti/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

gorti is an open-source Run-Time Infrastructure (RTI) written in Go, targeting
the IEEE 1516-2010 High Level Architecture (HLA) family. It connects Go, Python,
and Java federates through one standalone server.

Simulation models often outlive the languages and applications in which they
were developed. Federated simulation lets those models participate in a shared
experiment while retaining their domain logic and execution environments.
gorti provides the middleware, language interfaces, and runnable examples for
building that integration.

**Start with the [quickstart](docs/quickstart.md), the
[model integration example](docs/model-integration.md), or the
[documentation index](docs/index.md).**

## Why gorti?

gorti adds a Go-based implementation to the open-source RTI choices available
to simulation developers. Its focus is reusable software:

- **Connect existing models.** Go and Python interfaces and a Java
  `hla.rti1516e` provider share federation services and a versioned
  Protobuf/gRPC protocol.
- **Inspect and extend the implementation.** HLA services are separate from
  transport, delivery, and persistence adapters. Versioned requirements,
  design, interface, and test documents accompany the code.
- **Study distributed simulation.** Examples expose publication, subscription,
  callbacks, logical-time coordination, and the work needed to adapt an
  existing simulator.
- **Test service behavior.** Requirement-derived test cases connect reviewed
  IEEE clauses to inputs, expected observations, test code, and recorded
  executions.

[Portico](https://github.com/openlvc/portico) is another open-source RTI.
The [performance comparison](docs/performance.md) reports measurements of
complete Portico/Java and gorti/Go stacks, including conditions under which
their rankings changed.

## Run your first federation

Install Git and Go 1.22 or later, then clone the repository:

```bash
git clone https://github.com/cbchoi/gorti.git
cd gorti
```

Linux:

```bash
bash examples/go-tar-wait/run.sh
```

Windows PowerShell:

```powershell
.\examples\go-tar-wait\run.ps1
```

The launcher builds temporary binaries and starts one RTI server and two
independent federates. One federate requests time 5 while its peer stays at
time 0 for three seconds. It checks that the grant waits for the peer, then
stops its child processes. A successful run ends with:

```text
go-tar-wait: PASS - the peer delay held and then released TAR(5)
```

The [quickstart](docs/quickstart.md) also provides separate-terminal commands.
See [installation](docs/installation.md) for server builds and optional Python
and Java tools. All commands above run from the directory containing `go.mod`.

## Reuse a model across languages

The [Python-Go queue example](examples/multilanguage-queue/README.md) reuses
the existing pyjevsim source and sink models without changing their transition
functions. A separate Go federate runs an FCFS workstation. Timestamped HLA
interactions carry jobs and completions; both federates advance through RTI
time grants.

```bash
python -m pip install -e './pysdk[dev,pyjevsim]'
python -m rti1516e._proto
bash examples/multilanguage-queue/run.sh --output /tmp/gorti-model-results
```

On Windows, replace the last command with:

```powershell
.\examples\multilanguage-queue\run.ps1 --output D:\results\gorti-model-results
```

Use a new output directory outside the repository. The runner compares all
job trajectories with an independent queue reference. The retained campaign
matched all 900 jobs across nine executions with zero logical-time trajectory
error. These are functional model results, not network-speed measurements.

The [integration guide](docs/model-integration.md) explains the FOM mapping,
encodings, callback handling, results, and how to replace a model.
The [example catalog](examples/README.md) includes Java object/interaction
exchange and smaller service examples.

## Services and SDKs

The deployment uses one authoritative `rtid` process with local or remote
federates. Services include federation, declaration, object, time, ownership,
DDM, synchronization, save/restore, MOM, and Support. Float64 and Integer64
logical time are supported.

| SDK | Entry point |
|---|---|
| Go | [Federate package](rti/pkg/federate/) and [runtime guide](docs/user-guide.md#go-sdk) |
| Python | [Async API, ambassador adapter, and simulator bridge](pysdk/README.md) |
| Java | [`hla.rti1516e` provider, evoked callbacks, and encoding helpers](javasdk/README.md) |

The default HLA core profile does not enable audit journaling. Receive-order
LocalLRC admission, confirmed calls, and callback reporting have different
completion boundaries; see [LocalLRC](docs/local-lrc.md).
Read [operations](docs/operations.md) for endpoint security, configuration,
save storage, and the optional audit/replay plugin.

## Requirements and test cases

The test collection supports conformance assessment and regression testing;
it is not a conformance certificate. The latest retained full Linux campaign,
on 19 October 2026, used assessed commit
`4b11da478f423c0776f6ba6ae580f1670b61f2a9`:

| Selection | Passed | Skipped |
|---|---:|---:|
| Go default build, race detector | 3,533 | 19 |
| Go asynchronous build, race detector | 972 | 0 |
| Python SDK and selected HLA services | 2,127 | 0 |
| Java SDK and live services | 581 | 7 |
| Internal IVCT-derived cases | 35 | 0 |

No final-suite failures or errors were recorded. Selections overlap and must
not be added as unique requirements. The [verification page](docs/verification.md)
explains skips, counting units, source identity, and execution settings.

The 23 October traceability review contains 712 records: 635 applicable records
with passing linked invocations only, four applicable records with skips,
48 not applicable, and 25 unresolved. Behavioral tests, API/structure checks,
and a reporting guard are classified separately.
The [test-case guide](docs/test-cases.md) shows how a requirement becomes an
executable test and links the detailed inventory and result tables.

## Performance and reproducibility

The repository maintains experiment code, configurations, methods, and result
summaries. Raw execution logs and machine-local output remain outside Git.

- [Performance](docs/performance.md): October Portico comparison, confidence
  intervals, timing boundaries, and runtime profiles.
- [DEVStone-HLA benchmark](docs/benchmark.md): Windows/Linux execution commands.
- [Reproducibility](docs/reproducibility.md): model reruns, requirement-table
  regeneration, service tests, and benchmark designs.

The October performance campaigns used earlier builds than the 19 October
validation baseline. They are historical measurements, not a speed claim for
every later checkout.

## Develop and teach

Start with [architecture](docs/architecture.md) and the
[education guide](docs/education.md). HLA service packages can be studied
alongside their interfaces, adapters, and contract tests.

| Engineering document | Purpose |
|---|---|
| [SRS](engineering/specifications/current/SRS.md) | Requirements and operating constraints |
| [SDD](engineering/specifications/current/SDD.md) | Architecture, state ownership, and algorithms |
| [IDD](engineering/specifications/current/IDD.md) | Service and wire interfaces |
| [STD](engineering/specifications/current/STD.md) | Test design and expected observations |

[Historical specifications](engineering/specifications/history/) preserve
earlier versions. See [CONTRIBUTING.md](CONTRIBUTING.md) for development tools
and checks.

## Repository map

| Path | Contents |
|---|---|
| [`rti/`](rti/) | Server, HLA services, adapters, and Go SDK |
| [`pysdk/`](pysdk/) / [`javasdk/`](javasdk/) | Python and Java SDKs |
| [`examples/`](examples/) | Runnable federations and model adapters |
| [`tests/`](tests/) | Go service, architecture, integration, and conformance tests |
| [`verification/`](verification/) | Service comparisons and shared verification tools |
| [`benchmark/`](benchmark/) | Performance experiments |
| [`docs/`](docs/) | Read the Docs/MkDocs source |
| [`engineering/`](engineering/) | Specifications, traceability, and dated technical reports |

Report problems through [GitHub Issues](https://github.com/cbchoi/gorti/issues).
Citation metadata is in [CITATION.cff](CITATION.cff) and
[codemeta.json](codemeta.json). The project license is [MIT](LICENSE);
third-party components retain their own licenses.
