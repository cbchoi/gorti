# Runner logs

Cross-process Python examples write the same information to the terminal and
to a per-run directory. Terminal lines include the process name:

```text
[rtid] time=... level=INFO msg="rtid serving" grpc=127.0.0.1:...
[producer] producer_main: emitted seq=3
[consumer] consumer_main: received seq=3
[runner] workdir: examples/pyjevsim/.run/20260510-002226
```

The run directory keeps the unprefixed process output and result files:

```text
examples/pyjevsim/.run/20260510-002226/
  rtid.log
  producer.log
  consumer.log
  producer-result.json
  consumer-result.json
  eventlog/
  saves/
```

The directory is retained after a normal run so a failure can be inspected.
Pass `--no-keep-workdir` to remove it after verification.

## Common options

The Python runners share these options:

| Option | Meaning |
|---|---|
| `--workdir DIR` | Use a specific run directory instead of `.run/<timestamp>/` |
| `--no-keep-workdir` | Remove the run directory after collecting the result |
| `--log-level LEVEL` | Set the `rtid` log level: `debug`, `info`, `warn`, or `error` |
| `--rtid-binary PATH` | Use an existing server binary instead of building one |
| `--federate-timeout SECS` | Set the deadline for each federate process |

For example:

```bash
python examples/pyjevsim/runner.py --log-level debug
python examples/pyjevsim/runner.py --workdir examples/pyjevsim/.run/latest
python examples/pyjevsim/runner.py --no-keep-workdir
```

`examples/_log_tee.py` contains the shared output pump. The runner starts a
real `rtid` process unless the scenario uses `rtid -mode=pingpong-demo`; Python
federates always connect over a loopback gRPC listener.
