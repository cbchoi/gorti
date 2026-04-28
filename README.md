# gorti

Open-source IEEE 1516-2010 (HLA Evolved) Run-Time Infrastructure in Go, with a Python federate SDK that wraps pyjevsim DEVS coupled models.

**Status**: pre-MVP. M0 (contracts + scaffolding) in progress. See `docs/srs.md` §10.2 for the milestone plan.

## Standard

IEEE 1516-2010 only — not 1516-2000, not 1.3, not 1516-2025.

## Repo layout

See `docs/AGENTS.md` §3 for the canonical layout. Quick index:

| Path | Purpose |
|---|---|
| `proto/rti/v1/` | gRPC contracts (frozen, orchestrator-owned) |
| `rti/` | Go RTI server (Agents A & B) |
| `pysdk/` | Python federate SDK + pyjevsim bridge (Agent C) |
| `examples/` | Reference federates |
| `tests/spec/M<x>/` | Orchestrator-written specification tests for milestone M<x> |
| `tests/conformance/` | Cross-language conformance suite |
| `docs/` | SRS, SDD, IDD, AGENTS.md, CODING_CONVENTIONS, TDD, WORKFLOW, per-agent briefs |

## Documents (read these first)

In order:

1. `docs/srs.md` — Software Requirements Specification
2. `docs/sdd.md` — Software Design Document
3. `docs/idd.md` — Interface Design Document
4. `docs/CODING_CONVENTIONS.md` — strict code rules
5. `docs/TDD.md` — Test-Driven Development playbook
6. `docs/WORKFLOW.md` — git workflow + PR rules
7. `docs/AGENTS.md` — operating manual for the three coding agents
8. Your per-agent brief: `docs/agent-{a,b,c}-*.md`

## Build & test

```bash
make verify       # fmt + lint + test + determinism
make fmt          # gofmt + goimports + black + ruff --fix
make lint         # golangci-lint + ruff + buf lint
make test         # go test + pytest
make determinism  # 10x determinism harness on core packages
make proto        # regenerate gRPC bindings (requires buf)
```

## License

MIT — see `LICENSE`.
