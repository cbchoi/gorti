"""pytest fixtures shared across the M4 spec test suite."""

from __future__ import annotations

from collections.abc import Iterable
from pathlib import Path

import pytest

from ._fakes import (
    FakeRtiServer,
    StubCoupledModel,
    Vector,
    load_all_vectors,
    load_composite_vectors,
    load_primitive_vectors,
)

# Re-export for ergonomic test imports (`from .conftest import ...`).
__all__ = [
    "FakeRtiServer",
    "StubCoupledModel",
    "Vector",
    "all_vectors",
    "composite_vectors",
    "fake_rti",
    "primitive_vectors",
    "stub_coupled",
]


@pytest.fixture
def fake_rti() -> Iterable[FakeRtiServer]:
    """Fresh FakeRtiServer per test. Reset implicit via construction."""
    yield FakeRtiServer()


@pytest.fixture
def stub_coupled() -> Iterable[StubCoupledModel]:
    """Fresh StubCoupledModel per test, no preloaded schedule."""
    yield StubCoupledModel()


def primitive_vectors() -> list[Vector]:
    """Use as: @pytest.mark.parametrize('vec', primitive_vectors(), ids=lambda v: v.id)."""
    return load_primitive_vectors()


def composite_vectors() -> list[Vector]:
    """Use as: @pytest.mark.parametrize('vec', composite_vectors(), ids=lambda v: v.id)."""
    return load_composite_vectors()


def all_vectors() -> list[Vector]:
    """All non-disabled vectors from encoding_vectors.json."""
    return load_all_vectors()


# --- Repository roots for tests that need to read fixtures off-tree ---------

REPO_ROOT = Path(__file__).resolve().parents[4]
CONFORMANCE_DIR = REPO_ROOT / "tests" / "conformance"
FOMS_GOOD_DIR = CONFORMANCE_DIR / "foms" / "good"
FOMS_BAD_DIR = CONFORMANCE_DIR / "foms" / "bad"
ENCODING_VECTORS_PATH = CONFORMANCE_DIR / "encoding_vectors.json"
