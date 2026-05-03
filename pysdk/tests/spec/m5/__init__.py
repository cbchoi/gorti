"""Orchestrator-frozen specification tests for milestone M5 — Hardening
+ modes + perf + cross-language end-to-end. See docs/srs.md §10.2 for
the milestone gate.

These tests encode the M5 contract Agent C must satisfy on the Python
side. They:

  - drive the SDK + bridge against a real rtid binary (subprocess) where
    cross-language behavior is required
  - use the test doubles in pysdk/tests/spec/m4/_fakes/ where in-process
    behavior is sufficient
  - assert observable behavior, never internal state

Per docs/TDD.md §5, these tests are committed RED before the milestone
is dispatched. Agent C turns them green incrementally per the M5 wave
model (see docs/M5_DISPATCH_PLAN.md).

Agent C may ADD new tests under pysdk/tests/test_*.py but must NEVER
weaken or delete spec tests in this directory.
"""
