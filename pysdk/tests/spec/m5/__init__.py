"""Specification tests for milestone M5 — hardening
+ modes + perf + cross-language end-to-end. See docs/srs.md §10.2 for
the milestone gate.

These tests encode the Python side of the M5 contract. They:

  - drive the SDK + bridge against a real rtid binary (subprocess) where
    cross-language behavior is required
  - use the test doubles in pysdk/tests/spec/m4/_fakes/ where in-process
    behavior is sufficient
  - assert observable behavior, never internal state

New tests may extend this suite, but existing assertions must not be weakened
or deleted.
"""
