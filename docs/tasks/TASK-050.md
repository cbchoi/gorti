# TASK-050: Python integer primitive codecs + pysdk skeleton

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-019 |
| Blocks | TASK-051, TASK-052, TASK-053, TASK-054, TASK-055, TASK-056, TASK-057, TASK-058, TASK-059, TASK-063 |

## Goal

Bootstrap the `pysdk/` package and implement Python integer codecs that produce byte-identical output to TASK-010 across all integer vectors in `tests/conformance/encoding_vectors.json`.

## Scope (in)

- Create `pysdk/pyproject.toml` (project metadata; runtime deps `grpcio`, `protobuf`, `pyjevsim` (exact version pin per `docs/agent-c-pysdk.md` §4.4); dev deps `pytest`, `mypy`, `ruff`).
- Create `pysdk/README.md` (one paragraph: what the SDK is, link to `docs/agent-c-pysdk.md`).
- Create `pysdk/rti1516e/__init__.py` (empty skeleton with package docstring).
- Create `pysdk/rti1516e/encoding/__init__.py` (empty; `dispatch.py` populated by TASK-059).
- Create `pysdk/rti1516e/encoding/integer.py`: 6 integer codec classes using `struct`.
- Create `pysdk/tests/__init__.py`.
- Create `pysdk/tests/test_encoding_integer.py`: parametrized over the integer vectors.

## Scope (out)

- Other primitive codecs.
- FOM model/parser.
- SDK API.
- Bridge.

## Implements

- Requirements: FR-ENC-2 (Python).
- Spec tests: M4 spec tests under `tests/spec/M4/` (orchestrator pre-work) reference `pysdk/rti1516e/encoding/integer` for cross-language byte equality.

## TDD entry point

- Start with: `pysdk/tests/test_encoding_integer.py` parametrized over JSON vectors. Red until `integer.py` is implemented.

## Acceptance criteria

- [ ] Every integer vector encodes/decodes byte-identical to the JSON file.
- [ ] `mypy --strict pysdk/rti1516e/encoding/integer.py` clean.
- [ ] `ruff check pysdk/` clean.
- [ ] `pytest pysdk/tests/test_encoding_integer.py` green.
- [ ] `make verify` green.

## Notes / hints

- Use `struct.pack(">i", v)` etc. — no manual bit-twiddling.
- `pysdk/pyproject.toml` should pin Python ≥3.11.
