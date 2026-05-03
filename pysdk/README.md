# rti1516e — Python SDK for the gorti RTI

Python federate SDK for the [gorti](https://github.com/cbchoi/gorti) Run-Time
Infrastructure (IEEE 1516-2010 / HLA Evolved). Plus a `pyjevsim_bridge` adapter
that lets pyjevsim coupled models join HLA federations.

This package is M4 of the gorti project. See `docs/agent-c-pysdk.md` for the
SDK contract and `docs/M4_DISPATCH_PLAN.md` for the wave model driving the
implementation.

## Layout

- `rti1516e/encoding/` — IEEE 1516.2 encoding rules (byte-identical to Go side)
- `rti1516e/fom/` — FOM XML parser + dataclass model
- `rti1516e/connection.py`, `declaration.py`, `object.py`, `interaction.py`,
  `events.py`, `errors.py` — Layer 1 idiomatic asyncio API
- `rti1516e/standard.py` — Layer 2 1516-2010-shaped ambassador adapter
- `rti1516e/_generated/` — generated gRPC stubs (gitignored; `make py-codegen`)
- `pyjevsim_bridge/` — DEVS↔HLA bridge (port mapping, time advance, select)
- `tests/spec/m4/` — orchestrator-frozen specification tests
- `tests/test_*.py` — agent-owned unit tests

## Pre-dispatch state

This is the orchestrator-frozen pre-work. All public-API stubs raise
`NotImplementedError`. Spec tests fail RED until Agent C wires the M4 waves.
