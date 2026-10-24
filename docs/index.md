# gorti documentation

gorti is an open-source RTI written in Go, targeting the IEEE 1516-2010 HLA
family. One server coordinates federates written in Go, Python, and Java.
Models retain their own logic; federate adapters connect their inputs,
outputs, and execution steps to HLA services.

## Start here

| Task | Guide |
|---|---|
| Install the server and an SDK | [Installation](installation.md) |
| Run a server and two time-coordinated federates | [Quickstart](quickstart.md) |
| Connect existing Python models to a Go model | [Model integration](model-integration.md) |
| Write or adapt a federate | [Runtime and SDKs](user-guide.md) |
| Configure a server for other hosts | [Operations and security](operations.md) |
| Study or change a service algorithm | [Architecture](architecture.md) and [education](education.md) |

The [example catalog](https://github.com/cbchoi/gorti/blob/main/examples/README.md)
provides Windows and Linux launchers, including Java object and interaction
exchange.

## Services and deployment

The supported topology is one authoritative `rtid` process with local or remote
federates. It provides federation, declaration, object, time, ownership, DDM,
synchronization, save/restore, MOM, and Support services. Federations select
Float64 or Integer64 logical time. The default HLA core profile runs without
the optional audit journal.

For a first run, bind all endpoints to `127.0.0.1`. The unqualified federate
and metrics defaults, `:8442` and `:9090`, listen on all interfaces; the admin
default is plaintext on `localhost:8443`.
Read [operations](operations.md) before exposing them.

## Test cases and experiments

The project supplies reusable, requirement-derived test cases for conformance
assessment and regression testing. The [test-case guide](test-cases.md)
walks from a reviewed requirement to an executable assertion.
[Verification results](verification.md) distinguish service behavior,
API/structure checks, skipped cases, and unresolved records.

The [Python-Go model example](model-integration.md) checks simulation
trajectories against an independent reference. The
[performance page](performance.md) reports historical Portico comparisons,
including their uncertainty and measurement boundaries.
[Reproducibility](reproducibility.md) explains how to rerun experiments or
regenerate tables from retained reports.

## Project links

- [Source and issue tracker](https://github.com/cbchoi/gorti)
- [Releases](https://github.com/cbchoi/gorti/releases)
- [Citation metadata](citation.md)
- [Development roadmap](roadmap.md)
