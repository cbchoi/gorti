# IEEE 1516e Chat parity

This two-federate scenario reproduces the observable contract shared by the
reference IEEE 1516-2010 `chat-cpp` and `chat-java` samples without copying or
compiling their source. It covers participant discovery and name reflection,
communication delivery, reservation, federation lifecycle, and object removal
caused by resign-with-delete.

## Prerequisites

- Go 1.22 or later.
- Python 3.11 or later with the SDK development dependencies:

```bash
python -m pip install -e "./pysdk[dev]"
```

- Generated gRPC bindings. When absent, the scripts generate them with the
  `grpcio-tools` included in `pysdk[dev]`.

## Run and verify

From any working directory:

```bash
bash examples/ieee1516e-chat-parity/run.sh
```

```powershell
.\examples\ieee1516e-chat-parity\run.ps1
```

The default flow builds `rtid`, chooses free ports, runs the publisher and
subscriber, validates the machine-readable contract in
`tests/reference_examples/contracts/chat-1516e.json`, and always terminates the
daemon. The contract checks required events, payloads, object identity, and
only the required happens-before relations; callback scheduling need not form
one total order.

Set `PYTHON` or pass PowerShell's `-Python` parameter to select an interpreter.
For an unverified trace against an existing RTI, run:

```bash
python examples/ieee1516e-chat-parity/chat_scenario.py grpc://127.0.0.1:8080
```
