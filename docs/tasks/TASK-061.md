# TASK-061: Python FOM XML parser with same FOM-NNN diagnostics

| Field | Value |
|---|---|
| Status | DISPATCHED |
| Assignee | agent-c |
| Milestone | M4 |
| Created | 2026-04-29 |
| Updated | 2026-04-29 |
| Depends-on | TASK-060 |
| Blocks | none |

## Goal

Python FOM parser using same diagnostic codes (FOM-001 etc.) and same exit criteria as Agent B's Go parser. Uses fixtures in `tests/conformance/foms/` (read-only).

## Scope (in)

- Create `pysdk/rti1516e/fom/parser.py`.
- Create `pysdk/tests/test_fom_parser.py`: parametrized over both good and bad fixtures.

## Scope (out)

- N/A.

## Implements

- Requirements: FR-FOM-1 (Python side).

## TDD entry point

- Start with: parse `tests/conformance/foms/good/minimal.xml` → no diagnostics.

## Acceptance criteria

- [ ] All bad fixtures rejected with matching codes.
- [ ] All good fixtures accept.
- [ ] mypy/ruff clean.
- [ ] `make verify` green.

## Notes / hints

- Use `xml.etree.ElementTree` from stdlib; no third-party XML deps.
