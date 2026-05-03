"""Reference example for the pyjevsim DEVS bridge.

Two coupled-model substitutes (``Producer`` + ``Consumer``) wired through
the in-process ``HLAFederate`` bridge, exchanging ``ProducerOutput``
interactions described by ``tests/conformance/foms/good/pyjevsim-bridge.xml``.

Run end-to-end with::

    python examples/pyjevsim/runner.py

The harness is intentionally pure Python (no real ``rtid`` binary) — it
uses the same in-memory ``FakeRtiServer`` the spec tests drive so the
example also serves as the M4 determinism witness (TASK-074).
"""
