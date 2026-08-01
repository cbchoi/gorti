# DEVStone-HLA workload and delivery plan

The request used the spelling **DEVSOne**. This directory resolves it to
**DEVStone**, the published synthetic benchmark for DEVS simulation engines.
The implementation follows the LI and HI structures and corrected event-count
equations described by Risco-Martin et al. and the original benchmark by
Wainer, Glinsky, and Gutierrez-Alcaraz.

- [Corrected DEVStone analysis](https://doi.org/10.1177/0037549717690447)
- [Original DEVStone benchmark](https://doi.org/10.1177/0037549710395649)

## Scope

This is a **DEVStone-derived HLA/RTI traffic profile**, not a DEVS simulator
kernel score. It flattens an LI or HI graph, propagates each injected event
through that graph, and projects every resulting atomic event-value delivery
to one receive-order object update and one receive-order interaction. The
paired projection exercises both OM data paths with identical delivery bytes
for Portico and gorti.

For width `w`, depth `d`, and `N` injected events:

| Topology | Atomic nodes | Atomic event-value deliveries |
| --- | ---: | ---: |
| LI | `(w - 1)(d - 1) + 1` | `N * ((w - 1)(d - 1) + 1)` |
| HI | `(w - 1)(d - 1) + 1` | `N * ((w(w - 1)/2)(d - 1) + 1)` |

The plan generator does not trust these formulas as its execution source. It
starts with the ordered `external_input` couplings, propagates their
multiplicities through the flattened acyclic `internal` couplings, rejects
cycles, and checks the graph-derived count against `expected_counts`.

Records are emitted in this fixed order:

1. one-based injected-event sequence;
2. `atomic_nodes` document order;
3. zero-based occurrence ordinal for that event and target node.

## Two identities

`topology_identity.digest` is the immutable SHA-256 used by binary plans. Its
input is compact UTF-8 JSON with recursively sorted object keys and array order
preserved. The canonical identity object contains exactly:

- identity version `devstone-hla-topology/v1`;
- topology type, width, depth, and external-event count;
- synthetic external and internal transition delays;
- ordered `atomic_nodes` and ordered `directed_couplings`;
- the complete `hla_mapping` profile.

The runtime seed, `injected_events`, and their example payload bytes are not in
this identity. Changing a runtime seed therefore changes plan payloads and the
plan file SHA-256 without changing the topology identity. The separate
`identity.digest` still identifies the complete human-readable workload JSON,
including its default seed and example injection payloads.

## Binary plan format

All integers are unsigned and big-endian. A plan has a 52-byte header followed
by `record_count` fixed 32-byte records. No generated `*.dvshla` file is kept in
the repository.

Header:

| Offset | Size | Value |
| ---: | ---: | --- |
| 0 | 8 | ASCII magic `DVSHLA1\0` |
| 8 | 4 | record count (`uint32`) |
| 12 | 8 | runtime seed (`uint64`) |
| 20 | 32 | raw topology-identity SHA-256 |

Record:

| Offset | Size | Value |
| ---: | ---: | --- |
| 0 | 4 | zero-based delivery index |
| 4 | 4 | one-based injected-event sequence |
| 8 | 4 | zero-based target ordinal in `atomic_nodes` |
| 12 | 4 | zero-based occurrence ordinal for the event/target |
| 16 | 8 | attribute payload |
| 24 | 8 | interaction payload |

Each payload is the first eight bytes of SHA-256 over the following byte
sequence, with no padding or implicit separators:

```text
ASCII "gorti.devstone-hla.payload" + 0x00
8 bytes  ASCII "DVSHLA1" followed by 0x00         # plan version
ASCII channel name ("attribute" or "interaction") + 0x00
uint64   runtime seed
uint32   injected-event sequence
uint32   UTF-8 target-node-id byte length
bytes    UTF-8 target-node-id
uint32   target-node ordinal
uint32   occurrence ordinal
uint32   delivery index
32 bytes raw topology-identity SHA-256
```

The channel name supplies domain separation between the two OM payloads.
`tests/golden-vector-v1.json` contains a complete one-record header, record,
payload, topology identity, file size, and file SHA for Java and Go reader
tests.

## Generate the workload

The checked-in `workload.json` uses HI, width 10, depth 10, 100 external
events, and default seed 1516. Generation uses only the Python standard
library.

```bash
python benchmark/devstone/workload/generate.py
```

Parameters may be selected explicitly:

```bash
python benchmark/devstone/workload/generate.py --topology LI --width 40 --depth 200 --external-events 1 --seed 1516 --output benchmark/devstone/workload/workload.json
```

## Materialize and validate one runtime seed

Write generated plans to a benchmark output directory outside the repository.

```bash
python benchmark/devstone/workload/plan.py materialize --workload benchmark/devstone/workload/workload.json --seed 1516 --output /tmp/gorti-benchmark/devstone-1516.dvshla
python benchmark/devstone/workload/plan.py validate --workload benchmark/devstone/workload/workload.json --input /tmp/gorti-benchmark/devstone-1516.dvshla --seed 1516
```

PowerShell example:

```powershell
python benchmark/devstone/workload/plan.py materialize --workload benchmark/devstone/workload/workload.json --seed 1516 --output "$env:TEMP\gorti-benchmark\devstone-1516.dvshla"
python benchmark/devstone/workload/plan.py validate --workload benchmark/devstone/workload/workload.json --input "$env:TEMP\gorti-benchmark\devstone-1516.dvshla" --seed 1516
```

The API exposes `materialize_plan`, `parse_plan`, `validate_plan`,
`graph_delivery_multiplicities`, `plan_sha256`, and `file_sha256`. Validation
recomputes every coordinate and both payloads; it rejects wrong topology,
seed, ordering, count, corruption, truncation, and trailing bytes.

## Test

```bash
python -m unittest discover -s benchmark/devstone/workload/tests -v
```
