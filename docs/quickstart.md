# Quickstart

## Prerequisites

- Go 1.22+
- Python 3.11+ (for the Python SDK + cross-language tests)
- `make`, `git`

## Run the Go reference examples

```bash
git clone https://github.com/cbchoi/gorti
cd gorti

# 1000 ping-pong interactions, in-process
go run ./examples/go-pingpong

# 3 federates with NER over 10 logical ticks
go run ./examples/go-timed -ticks=10
```

Expected output:

```text
go-pingpong: 1000 rounds in 311ms
go-timed: 10 ticks in 3.03ms
```

## Build the rtid server

```bash
go build -o bin/rtid ./rti/cmd/rtid
./bin/rtid --listen :8442 --metrics-listen :9090
```

Federates connect with `grpc://localhost:8442`. TLS via `--tls-cert / --tls-key` is pinned for production deployments.

## Run the Python+RTI cross-language example

```bash
# Install Python deps (Python 3.11+)
cd pysdk
pip install -e '.[dev]' pyjevsim==1.3.1
cd ..

# Generate gRPC stubs
make py-codegen

# Two-Python smoke against an in-process FakeRtiServer
python3 examples/pyjevsim/runner.py

# Cross-process via real gRPC against a subprocess-spawned rtid
pytest pysdk/tests/spec/m5/test_spec_m5_cross_language.py
```

## Run the conformance suite

```bash
# Go side: M0..M12 spec tests, examples, perf, race-clean
go test -race -count=1 ./...

# Python side: 498 unit + integration tests
python3 -m pytest pysdk/tests/

# Cross-language M5 + M12 over real subprocess rtid
python3 -m pytest pysdk/tests/spec/m5/ pysdk/tests/spec/m12/
```

## Run with an alternative algorithm

The research platform lets you swap algorithms via a TOML config. See the [research how-to](research-platform-howto.md) for a worked example.

```bash
cat > research.toml <<EOF
determinism = "per-impl-opt-in"

[ownership]
negotiation = "random-acquirer"
EOF

./bin/rtid --listen :8442 --research-config research.toml
```

The strict-mode gate rejects non-preserving impls at boot:

```bash
sed -i 's/per-impl-opt-in/strict/' research.toml
./bin/rtid --listen :8442 --research-config research.toml
# error: research: strict-mode rejects non-preserving ownership.negotiation strategy "random-acquirer"
```

## Build the documentation locally

```bash
pip install -r docs/requirements.txt
mkdocs serve
# Browse at http://127.0.0.1:8000/
```

## Repo layout

| Path | Purpose |
|---|---|
| `proto/rti/v1/` | gRPC contracts (orchestrator-frozen) |
| `rti/` | Go RTI server, encoder, FOM parser |
| `rti/internal/research/` | Strategy registry + TOML config + determinism gate |
| `pysdk/` | Python federate SDK + pyjevsim bridge |
| `examples/` | Reference federates (go-pingpong, go-timed, pyjevsim) |
| `tests/conformance/` | Cross-language conformance vectors + FOM fixtures |
| `tests/spec/M1/`, `rti/spec/M{2,3,5,7,8,9,10,11,12}/`, `pysdk/tests/spec/m{4,5,12}/` | Per-milestone specification tests |
| `docs/` | This documentation site |
| `CHANGELOG-MASTERPLAN.md` | Full milestone-by-milestone history |
