# Runner logging convention

All cross-process examples follow the same logging shape, established in
the 2026-05-10 update.

## What you see

Every example's `runner.py` spawns rtid + the federate processes as
real subprocesses. Their stdout+stderr are tee'd to TWO sinks:

1. **The parent's stderr** — each line is prefixed with the
   subprocess's name in square brackets:
   ```
   [rtid]      time=... level=INFO msg="rtid serving" grpc=[::]:8442 ...
   [producer]  producer_main: emitted seq=3
   [consumer]  consumer_main: received seq=3
   [runner]    workdir: examples/pyjevsim/.run/20260510-002226
   ```

2. **A file under the working directory** — raw subprocess bytes,
   no prefix, suitable for `grep` and post-mortem inspection:
   ```
   examples/pyjevsim/.run/20260510-002226/
     ├── rtid.log                     # rtid's stdout+stderr
     ├── producer.log                 # producer federate's stdout+stderr
     ├── consumer.log                 # consumer federate's stdout+stderr
     ├── producer-result.json         # per-federate result
     ├── consumer-result.json
     ├── eventlog/                    # rtid's --log-dir output (per-federation event logs)
     └── saves/                       # rtid's --save-dir output (federation save bundles)
   ```

## Common flags

Every runner accepts:

  - `--workdir DIR` — override the working directory. Default:
    `<example>/.run/<timestamp>/`.
  - `--no-keep-workdir` — delete the workdir after the run finishes
    (default: keep, so you can inspect the artifacts).
  - `--log-level LEVEL` — passed to rtid as `--log-level`. One of
    `debug`, `info` (default), `warn`, `error`.
  - `--rtid-binary PATH` — point at a pre-built rtid binary; otherwise
    the runner builds `<repo>/bin/rtid` via `go build` on first use.
  - `--federate-timeout SECS` — per-federate exit deadline.

## Why every example is "managed by rtid"

There are no in-process examples. Every example spawns a real `rtid`
subprocess and federates connect over `grpc://127.0.0.1:<port>`. The
go-pingpong example is a thin shim that runs rtid in `-mode=pingpong-demo`
(in-process federates inside the rtid process); rtid is still the
canonical owner of state.

## Implementation

The shared tee helper lives at `examples/_log_tee.py`. Each runner
imports `LogTee` and wires it after `Popen(stdout=PIPE,
stderr=STDOUT)`. The pump thread is a daemon, so a runner exiting
won't hang on a stuck subprocess pipe.

## Inspecting a run

```bash
# Run with default INFO logging:
python3 examples/pyjevsim/runner.py

# Run with DEBUG logging tee'd to console:
python3 examples/pyjevsim/runner.py --log-level debug

# Use a fixed working directory so subsequent runs overwrite:
python3 examples/pyjevsim/runner.py --workdir examples/pyjevsim/.run/latest

# Discard the workdir after the run (CI-friendly):
python3 examples/pyjevsim/runner.py --no-keep-workdir
```
