# Shared verification records

The live Java/Python adapters use the compact `kind`, `service`, `event`,
`actor`, and `data` envelope documented in `verification/README.md`.
`project_semantics.py` validates implementation-specific evidence and emits
the byte-identical cross-runtime transcript; `compare_logs.py` compares it and
`compare_performance.py` reports commensurable metrics separately.

The typed `gorti.verification/v1` utilities below are a stricter reusable API
for future verification drivers. They are tested independently and are not
used to weaken or replace logical-time checks in the live projection.

`verification.common` is the canonical interchange layer for semantic and
performance verification. Each file is UTF-8 NDJSON: one `schema.json` record
per line, no blank lines, and a final LF. `dumps_ndjson` emits sorted, compact
JSON so identical records are byte-identical.

Semantic records use `service` values `FM`, `DM`, `OM`, and `TM`. Comparison is
strict for service/event order and payload structure after only these volatile
values are normalized:

- runtime-assigned `*handle` and `*handles` values are mapped by handle family
  and first appearance, preserving equality and aliasing relationships;
- wall-clock/timestamp fields become `<TIMESTAMP>`;
- duration, elapsed, and latency fields become `<TIMING>`;
- logical/HLA/simulation/requested/granted time remains exact.

Top-level `runtime` and `timing` objects are evidence only and do not take part
in semantic equality. Performance samples are aligned in log order and require
exact metric identity, unit, direction, and dimensions. Values are checked by
`PerformancePolicy`; the default has zero tolerance.

## Fixed-seed payload rule

Payload bytes are a random-access SHA-256 counter stream. Inputs are an unsigned
64-bit seed, UTF-8 stream name, unsigned 64-bit event index, and byte length.
Every digest block hashes the versioned domain separator plus those inputs and
an unsigned 64-bit block index, all integers big-endian. Generation never uses
global RNG state, process state, call order, timestamps, or platform encoding.

Use a distinct stream name for each scenario role or message class and use the
semantic event index as `index`. `payload_envelope` is the canonical JSON-safe
base64 form and includes decoded size and SHA-256 integrity metadata.

Run focused checks from the repository root:

```text
python -m pytest verification/common/tests
python -m ruff check verification/common
python -m verification.common.compare expected.ndjson actual.ndjson
```
