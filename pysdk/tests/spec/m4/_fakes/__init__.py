"""Test doubles for M4 spec tests. Orchestrator-owned scaffolding.

- vector_loader: parametrize-able loader for tests/conformance/encoding_vectors.json
- fake_rti_server: in-process pure-Python double of the RTI gRPC surface
- stub_coupled_model: pyjevsim CoupledModel substitute for bridge tests
"""

from __future__ import annotations

from .fake_rti_server import FakeRtiServer
from .stub_coupled_model import StubCoupledModel
from .vector_loader import (
    Vector,
    load_all_vectors,
    load_composite_vectors,
    load_primitive_vectors,
)

__all__ = [
    "FakeRtiServer",
    "StubCoupledModel",
    "Vector",
    "load_all_vectors",
    "load_composite_vectors",
    "load_primitive_vectors",
]
