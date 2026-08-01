# IEEE 1516e typed-handle smoke

This Python example exercises the IEEE 1516-2010 typed handle and collection
surface. It resolves object, attribute, interaction, and parameter handles;
publishes with `AttributeHandleSet`; updates with
`AttributeHandleValueMap`; and sends with `ParameterHandleValueMap`.

## Prerequisites

- Go 1.22 or later.
- Python 3.11 or later with `grpcio`, `protobuf`, and `pytest`. The repository
  development install provides them:

```bash
python -m pip install -e "./pysdk[dev]"
```

- Generated gRPC bindings. When absent, the scripts generate them with the
  `grpcio-tools` included in `pysdk[dev]`.

## Run and verify

From any working directory:

```bash
bash examples/ieee1516e-typed-smoke/run.sh
```

```powershell
.\examples\ieee1516e-typed-smoke\run.ps1
```

The default integration flow builds and starts `rtid` on free ports, runs the
typed federate, verifies every handle is nonzero and of the expected type,
checks reservation and synchronization callbacks, and tears down the daemon.
Set `PYTHON` or use PowerShell's `-Python` parameter to choose an interpreter.

For publisher output without assertions against an existing daemon:

```bash
python examples/ieee1516e-typed-smoke/smoke_federate.py grpc://127.0.0.1:8080
```
