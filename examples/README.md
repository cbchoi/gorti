# Examples

Each directory below contains a runnable federation, a short README, and two
entry points: `run.sh` for Bash and `run.ps1` for PowerShell. Both scripts find
the repository from their own location, so they can be run from any working
directory.

```bash
bash examples/go-pingpong/run.sh
```

```powershell
.\examples\go-pingpong\run.ps1
```

## Catalog

| Directory | Scenario | Needs |
|---|---|---|
| `go-pingpong/` | Two in-process Go federates exchange interactions | Go |
| `go-tar-wait/` | One federate waits for its peer before receiving a time grant | Go |
| `go-timed/` | Three Go processes advance with different lookaheads | Go; Python for Bash verification |
| `cpp-ieee1516e-smoke/` | C++ publisher and cross-language delivery | Go, C++ toolchain |
| `ieee1516e-ambassador-smoke/` | Python ambassador lifecycle and callbacks | Go, Python |
| `ieee1516e-chat-parity/` | Object and interaction behavior from the IEEE Chat pattern | Go, Python |
| `ieee1516e-typed-smoke/` | Typed Python handles and collections | Go, Python |
| `pyjevsim/` | Producer and consumer exchange interactions in separate processes | Go, Python |
| `pyjevsim-dashboard/` | Direct SDK object registration and attribute reflection | Go, Python |
| `pyjevsim-dashboard-bridged/` | The dashboard flow with declarations owned by model hooks | Go, Python |
| `pyjevsim-object-tracking/` | Time-managed vehicle updates drive tracker models | Go, Python |
| `pyjevsim-real-model/` | Actual pyjevsim `BehaviorModel` subclasses exchange pulses | Go, Python with the `pyjevsim` extra |
| `pyjevsim-relay-cross-process/` | Generator, bounded buffer, and processor accounting | Go, Python |
| `pyjevsim-sync-points/` | Three federates rendezvous at two synchronization points | Go, Python |
| `pyjevsim-time-advance/` | Python federates run NER cycles with different lookaheads | Go, Python |

The README in each directory gives the expected result and available runner
options.

## Setup

Go examples require Go 1.22 or later. Python examples require Python 3.11 or
later and locally generated gRPC bindings:

```bash
python -m pip install -e "./pysdk[dev]"
python -m rti1516e._proto
```

Install `./pysdk[dev,pyjevsim]` instead for `pyjevsim-real-model`.

The C++ example needs a C++17 compiler, CMake 3.18 or later, protobuf, and
gRPC++. See [`cppsdk/README.md`](../cppsdk/README.md) for dependency setup. The
C++ SDK is maintained on Linux and macOS; its PowerShell launcher reports
missing prerequisites but does not imply Windows C++ support.

Most cross-process examples build and start `rtid` themselves. Pass an
existing binary through the runner option documented by the example when Go
is not available.

## Launcher rules

The paired launchers run the same default scenario. They return zero only
after the result has been checked, propagate child-process failures, and stop
the processes they start. They do not install dependencies or use
machine-specific paths.

Check all example entry points without running the federations:

```bash
bash scripts/check-example-entrypoints.sh
```

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\check-example-entrypoints.ps1
```

The validator checks required files, path handling, shell syntax, line
endings, and hard-coded local paths.
