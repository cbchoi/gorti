# Go timed federation

Three cross-process Go federates run TAR cycles against a real `rtid` daemon.
The `fast`, `normal`, and `slow` federates use lookaheads 0.5, 1.0, and 2.0,
respectively. Each cycle waits for a time-advance grant and emits a timestamped
`Tick` at `grant + lookahead`.

## Prerequisites

- Go 1.22 or later on `PATH`.
- Python 3 for the Bash result verifier. PowerShell reads JSON natively.

## Run and verify

From any working directory:

```bash
bash examples/go-timed/run.sh
```

```powershell
.\examples\go-timed\run.ps1
```

The entry script builds temporary binaries, starts `rtid` on an isolated
example port, launches all three federates concurrently, verifies their JSON
results, and stops every process on success, failure, or interruption. The
default checks that every federate receives ten grants, no federate's logical
time regresses, and the per-cycle minimum grant never regresses.

Use `CYCLES`, `TICK_STEP`, and `RTID_PORT` with `run.sh`; PowerShell exposes
`-Cycles`, `-TickStep`, and `-RtidPort`. `TICK_STEP` must remain greater than
the slow federate's 2.0 lookahead.

## Manual terminals

The original per-process helpers remain useful for observing each participant:

```bash
cd examples/go-timed
./rtid_run.sh       # terminal 1
./fast_run.sh       # terminal 2
./normal_run.sh     # terminal 3
./slow_run.sh       # terminal 4
./verify_run.sh     # after all federates exit
```

They share defaults in `_run_common.sh`. Set `RESULT_DIR`, `RTID_LISTEN_PORT`,
`CYCLES`, `TICK_STEP`, or `PRIMITIVE` to customize a manual run. The manual
flow uses TAR; manager-level tests cover the stricter NER boundary behavior.

The automated Go integration test remains available on Unix-like systems:

```bash
go test -run TestGoTimedEndToEnd -timeout 60s ./examples/go-timed
```
