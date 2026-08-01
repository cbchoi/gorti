"""Specification tests for milestone M4 — Python SDK
+ pyjevsim bridge. See docs/srs.md §10.2 for the M4 milestone gate.

These tests encode the M4 contract. They:

  - import the concrete rti1516e + pyjevsim_bridge packages
  - drive their public APIs
  - use the test doubles in _fakes/ (FakeRtiServer, StubCoupledModel,
    vector loader), which may be extended compatibly when new tests require it
  - assert observable behavior, never internal state

New tests may extend this suite, but existing assertions must not be weakened
or deleted.
"""
