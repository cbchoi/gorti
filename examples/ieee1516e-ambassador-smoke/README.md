# IEEE 1516e ambassador smoke

The federates use only the Python IEEE 1516-2010 service-style ambassador API.
The default run starts a publisher and subscriber against a live
`rtid`, then checks federation lifecycle, handle lookup, declarations, object
discovery, attribute reflection, interaction delivery, name reservation, and
synchronization callbacks.

## Prerequisites

- Go 1.22 or later.
- Python 3.11 or later with the SDK development dependencies:

```bash
python -m pip install -e "./pysdk[dev]"
```

- Generated gRPC bindings. When absent, the entry script generates them with
  the `grpcio-tools` included in `pysdk[dev]`.

## Run and verify

From any working directory:

```bash
bash examples/ieee1516e-ambassador-smoke/run.sh
```

```powershell
.\examples\ieee1516e-ambassador-smoke\run.ps1
```

Set `PYTHON` to choose an interpreter, or pass `-Python C:\path\python.exe` to
PowerShell. The live test builds `rtid`, chooses free listener ports, starts
both federates in the required order, verifies their callbacks, and always
terminates the daemon. A passing run reports one pytest test passed.

For a publisher-only diagnostic against an already-running RTI:

```bash
python examples/ieee1516e-ambassador-smoke/smoke_federate.py grpc://127.0.0.1:8080
```
