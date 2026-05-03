"""Orchestrator-frozen specification tests for milestone M4 — Python SDK
+ pyjevsim bridge. See docs/srs.md §10.2 for the M4 milestone gate.

These tests encode the M4 contract Agent C must satisfy. They:

  - import the concrete rti1516e + pyjevsim_bridge packages
  - drive their public APIs
  - use the test doubles in _fakes/ (FakeRtiServer, StubCoupledModel,
    vector loader) — Agent C may NOT modify the spec tests but MAY
    extend the fakes if a new test requires it (back-compat only)
  - assert observable behavior, never internal state

Per docs/TDD.md §5, these tests are committed RED before the milestone
is dispatched. Agent C turns them green incrementally per the M4 wave
model (see docs/M4_DISPATCH_PLAN.md).

Agent C may ADD their own tests under pysdk/tests/test_*.py but must
NEVER weaken or delete spec tests in this directory. Doing so trips a
verification finding at the M4 gate.
"""
