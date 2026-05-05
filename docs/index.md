# gorti

Open-source [IEEE 1516-2010 (HLA Evolved)](https://standards.ieee.org/ieee/1516/4118/) Run-Time Infrastructure in Go, with a Python federate SDK that wraps [pyjevsim](https://github.com/cbchoi/pyjevsim) DEVS coupled models.

## What's in the box

- **`rtid`** — Go RTI server with full federation lifecycle, declaration management, object registry, time management (NER + LBTS + stall timeout), event log, and reproducible determinism (byte-identical event logs across runs).
- **`rti1516e`** — Python federate SDK with idiomatic asyncio API (Layer 1) and a 1516-shaped ambassador adapter (Layer 2).
- **`pyjevsim_bridge`** — DEVS↔HLA bridge so pyjevsim coupled models join HLA federations.
- **Cross-language byte-identical encoding** — Python and Go encoders agree on every entry of `tests/conformance/encoding_vectors.json` (94 vectors).
- **Same FOM diagnostics on both sides** — FOM-001..FOM-101 codes match between the Go and Python parsers.

## Status

| Cut | Milestones | Status |
|---|---|---|
| Cut 1 (MVP) | M0..M5 | DONE — tag `mvp` |
| Cut 2 (production-grade RTI) | M6..M11 | DONE |
| Cut 3 (gRPC + Python exposure) | M12 | DONE |
| Research-platform refactor | Phases 0..4 | DONE |

See [Changelog](changelog.md) for the milestone-by-milestone history.

## Where to go next

- **New here?** Start with [Quickstart](quickstart.md) — run a 3-federate NER demo in under a minute.
- **Want to understand the design?** Read [SRS](srs.md) → [SDD](sdd.md) → [IDD](idd.md) in that order.
- **Working on gorti?** [Test-Driven Discipline](TDD.md), [Coding Conventions](CODING_CONVENTIONS.md), [Multi-Agent Workflow](WORKFLOW.md), and [Dispatch Model](DISPATCH.md) define the contributor process.
- **Researching HLA/RTI algorithms?** Start with the [Research Platform Design](research-platform.md) and the [How-to](research-platform-howto.md) for adding an alternative strategy implementation.

## Performance

Reference baseline (12th Gen i7-12700, 20 cores, 5–10 s/size, in-process):

| Federation size | Throughput (interactions/sec) | p50 (ms) | p99 (ms) |
|---:|---:|---:|---:|
|   5 | 1,956,160 | 0.03 | 0.14 |
|  25 |   966,753 | 0.04 | 1.76 |
| 100 |   259,624 | 0.41 | 4.84 |

Reproduce: `go test -tags=perfcompare -run "TestThroughput_Size25$" -v ./rti/internal/perf/`.
Full optimization writeup: [Performance optimization pass](reports/perf/M12-optimization-pass.md).

## Standards conformance

IEEE 1516-2010 only — not 1516-2000, not 1.3, not 1516-2025. Cut-1 scope is the walking-skeleton MVP per [SRS](srs.md). Cut-2 closes the production service surface (sync points, ownership, MOM, DDM, save/restore). Cut-3 wires gRPC handlers + Python SDK exposure for those cut-2 service groups.
