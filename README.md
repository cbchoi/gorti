# gorti

[![Manual validation](https://github.com/cbchoi/gorti/actions/workflows/ci.yml/badge.svg)](https://github.com/cbchoi/gorti/actions/workflows/ci.yml)
[![Documentation](https://github.com/cbchoi/gorti/actions/workflows/docs.yml/badge.svg)](https://github.com/cbchoi/gorti/actions/workflows/docs.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![DOI](https://zenodo.org/badge/DOI/10.5281/zenodo.23619049.svg)](https://doi.org/10.5281/zenodo.23619049)

gorti is an open-source Run-Time Infrastructure (RTI) written in Go, targeting
the IEEE 1516-2010 High Level Architecture (HLA) family. It connects Go, Python,
and Java federates through a standalone server.

Federated simulation connects independently developed simulators without
combining them into one program. gorti provides language interfaces, model
adapters, and examples for this integration. Service modules and design
documents support development and teaching.

[Documentation](https://gorti.readthedocs.io/en/latest/) |
[Release 1.0.0](https://github.com/cbchoi/gorti/releases/tag/1.0.0) |
[Installation](docs/installation.md) |
[Examples](examples/README.md)

## Quickstart

Install Git and Go 1.22 or later:

```bash
git clone --branch 1.0.0 https://github.com/cbchoi/gorti.git
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

The launcher starts an RTI server and two independent federates. One requests
time 5 while its peer stays at time 0 for three seconds. It checks that the
grant waits for the peer, then stops its child processes. A successful run
ends with `go-tar-wait: PASS`.

See the [quickstart guide](docs/quickstart.md) for separate-terminal commands.

## Services and SDKs

One authoritative `rtid` process manages local or remote federates. Services
include federation, declaration, object, time, ownership, DDM, synchronization,
save/restore, MOM, and Support. Federations use Float64 or Integer64 logical time.

| SDK | Interface |
|---|---|
| [Go](rti/pkg/federate/) | Native federate API, LocalLRC, and confirmed calls |
| [Python](pysdk/README.md) | Async API, ambassador adapter, and pyjevsim bridge |
| [Java](javasdk/README.md) | `hla.rti1516e` provider, evoked callbacks, and encoding helpers |

The [runtime guide](docs/user-guide.md) covers FOMs, callbacks, and the SDK
lifecycle. [Operations](docs/operations.md) covers configuration, security,
save storage, and the optional audit/replay plugin.

## Connect existing models

The [Python-Go queue example](docs/model-integration.md) reuses pyjevsim source
and sink models and connects them to a Go FCFS workstation. Timestamped
interactions carry jobs and completions. The runner checks each trajectory
against an independent reference.

From an activated Python 3.11+ environment:

```bash
python -m pip install -e './pysdk[dev,pyjevsim]'
python -m rti1516e._proto
bash examples/multilanguage-queue/run.sh --output /tmp/gorti-model-results
```

On Windows, replace the last command with:

```powershell
.\examples\multilanguage-queue\run.ps1 --output D:\results\gorti-model-results
```

Use a new output directory outside the repository. The guide explains the
FOM, encodings, time coordination, and recorded model results.

## Tests and performance

[Requirement-derived tests](docs/test-cases.md) link reviewed IEEE clauses
to inputs, expected observations, and execution results.
The [verification report](docs/verification.md) lists the assessed source,
test counts, skips, and unresolved records.

[Performance results](docs/performance.md) compare Portico/Java and gorti/Go
using DEVStone-HLA. The results include timing boundaries, execution conditions,
and confidence intervals. [Reproducibility](docs/reproducibility.md) gives
commands for model runs, test reports, and performance experiments.

## Development and education

The [architecture guide](docs/architecture.md) describes service, application,
and adapter responsibilities. The [education guide](docs/education.md)
provides exercises in model integration, time management, and test design.

| Document | Contents |
|---|---|
| [SRS](engineering/specifications/current/SRS.md) | Requirements and constraints |
| [SDD](engineering/specifications/current/SDD.md) | Architecture and algorithms |
| [IDD](engineering/specifications/current/IDD.md) | Service and wire interfaces |
| [STD](engineering/specifications/current/STD.md) | Test design and expected results |

See [CONTRIBUTING.md](CONTRIBUTING.md) for development tools and checks.

## Repository layout

| Path | Contents |
|---|---|
| [`rti/`](rti/) | Server, HLA services, adapters, and Go SDK |
| [`pysdk/`](pysdk/) / [`javasdk/`](javasdk/) | Python and Java SDKs |
| [`examples/`](examples/) | Federations and model adapters |
| [`tests/`](tests/) / [`verification/`](verification/) | Tests and service comparison tools |
| [`benchmark/`](benchmark/) | Performance experiments |
| [`docs/`](docs/) | User documentation |
| [`engineering/`](engineering/) | Specifications, traceability, and technical reports |

Report problems through [GitHub Issues](https://github.com/cbchoi/gorti/issues).
Citation metadata is in [CITATION.cff](CITATION.cff).
The project license is [MIT](LICENSE); third-party components retain their own
licenses.
