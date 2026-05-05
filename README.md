# gorti

Open-source [IEEE 1516-2010 (HLA Evolved)](https://standards.ieee.org/ieee/1516/4118/) Run-Time Infrastructure in Go, with a Python federate SDK that wraps [pyjevsim](https://github.com/cbchoi/pyjevsim) DEVS coupled models.

**Status**: MVP achieved (tag [`mvp`](https://github.com/cbchoi/gorti/releases/tag/mvp)). M0..M5 all DONE per [`docs/srs.md`](docs/srs.md) §10.2. See [`CHANGELOG-MASTERPLAN.md`](CHANGELOG-MASTERPLAN.md) for the full milestone history.

What's in the box:
- **`rtid`** — Go RTI server with full federation lifecycle, declaration management, object registry, time management (NER + LBTS + stall timeout), event log, and reproducible determinism (byte-identical event logs across runs).
- **`rti1516e`** — Python federate SDK with idiomatic asyncio API (Layer 1) and a 1516-shaped ambassador adapter (Layer 2).
- **`pyjevsim_bridge`** — DEVS↔HLA bridge so pyjevsim coupled models join HLA federations.
- **Cross-language byte-identical encoding** — Python and Go encoders agree on every entry of `tests/conformance/encoding_vectors.json` (94 vectors).
- **Same FOM diagnostics on both sides** — FOM-001..FOM-101 codes match between the Go and Python parsers.

## Quickstart

### Run the Go reference example

```bash
git clone https://github.com/cbchoi/gorti
cd gorti
go run ./examples/go-pingpong              # 1000 ping-pong interactions, in-process
go run ./examples/go-timed -ticks=10       # 3 federates with NER over 10 logical ticks
```

### Run the Python+RTI cross-language example

```bash
# Install Python deps (Python 3.11+)
cd pysdk && pip install -e '.[dev]' pyjevsim==2.0.1
make py-codegen                            # regenerate gRPC stubs into _generated/

# Two-Python smoke against a real rtid (subprocess-spawned)
cd ..
python3 examples/pyjevsim/runner.py        # in-process via FakeRtiServer
pytest pysdk/tests/spec/m5/test_spec_m5_cross_language.py  # cross-process via gRPC
```

### Build rtid yourself

```bash
go build -o bin/rtid ./rti/cmd/rtid
./bin/rtid --listen :8442 --metrics-listen :9090
```

Federate connects with `grpc://localhost:8442`. (TLS: `--tls-cert/--tls-key` pinned for production; M6 hardening track.)

## Performance

Reference baseline ([`docs/reports/M5/agent-a.md`](docs/reports/M5/agent-a.md), 12th Gen i7-12700, 20 cores, 10s/size, in-process):

| Federation size | Throughput (interactions/sec) | p50 (ms) | p99 (ms) |
|---:|---:|---:|---:|
| 2   | 3,035,333 |  0.044 |  0.125 |
| 5   | 1,253,641 | 10.16  | 15.04  |
| 25  |   184,191 |  2.90  | 23.22  |
| 100 |    48,863 |  3.22  | 33.81  |

Reproduce: `go run -tags=perf ./rti/cmd/perf-baseline`.

## Repo layout

| Path | Purpose |
|---|---|
| `proto/rti/v1/` | gRPC contracts (orchestrator-frozen) |
| `rti/` | Go RTI server + encoder + FOM parser |
| `pysdk/` | Python federate SDK + pyjevsim bridge |
| `examples/` | Reference federates (go-pingpong, go-timed, pyjevsim) |
| `tests/conformance/` | Cross-language conformance vectors + FOM fixtures |
| `tests/spec/M1/`, `rti/spec/M{2,3,5}/`, `pysdk/tests/spec/m{4,5}/` | Per-milestone specification tests (orchestrator-frozen) |
| `docs/` | SRS, SDD, IDD, agent briefs, dispatch plans, status reports |
| `CHANGELOG-MASTERPLAN.md` | Full milestone-by-milestone history |

## Standards conformance

IEEE 1516-2010 only — not 1516-2000, not 1.3, not 1516-2025. Cut-1 scope is the walking-skeleton MVP per [`docs/srs.md`](docs/srs.md). M6+ work (cross-language handle alignment, gRPC TLS hardening, EventLog Writer concurrency, real-pyjevsim structural-hierarchy adapter) tracked in [`docs/reports/M5/agent-c.md`](docs/reports/M5/agent-c.md) "M6 follow-ups."

## For contributors

Read the design and operating manuals in this order:

1. [`docs/srs.md`](docs/srs.md) — Software Requirements Specification
2. [`docs/sdd.md`](docs/sdd.md) — Software Design Document
3. [`docs/idd.md`](docs/idd.md) — Interface Design Document
4. [`docs/AGENTS.md`](docs/AGENTS.md) — operating manual (architecture + role decomposition)
5. [`docs/ORTHOGONALITY.md`](docs/ORTHOGONALITY.md) — path-ownership table (per-file owner)
6. [`docs/DISPATCH.md`](docs/DISPATCH.md) — task-dispatch protocol
7. [`docs/TDD.md`](docs/TDD.md) — test-first commit pattern
8. [`docs/WORKFLOW.md`](docs/WORKFLOW.md) — git + PR rules
9. [`docs/CODING_CONVENTIONS.md`](docs/CODING_CONVENTIONS.md) — strict rules (no `time.Now()`, sorted iteration, etc.)
10. `docs/M{2,3,4,5}_DISPATCH_PLAN.md` — per-milestone wave models (reference for M6+ planning)

## Build & test

```bash
make verify       # fmt + lint + test + determinism (run before any PR)
make fmt          # gofmt + goimports + ruff --fix
make lint         # golangci-lint + ruff + buf lint
make test         # go test + pytest
make determinism  # 10x determinism harness on core packages
make proto        # regenerate Go gRPC bindings (requires buf)
make py-codegen   # regenerate Python gRPC bindings (requires grpcio-tools)
make py-test      # pytest pysdk/
make py-lint      # ruff check pysdk/
make py-typecheck # mypy --strict pysdk/
```

Milestone status: `./scripts/check-milestones.sh` (auto-detects M0..M5 state from the working tree).

## License

MIT — see [`LICENSE`](LICENSE).
