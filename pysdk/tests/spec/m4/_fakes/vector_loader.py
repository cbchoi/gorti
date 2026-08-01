"""Loader for tests/conformance/encoding_vectors.json.

This file is the cross-language source of truth: every vector
encode-decodes byte-identical on Go (rti/pkg/encoding) AND on Python
(rti1516e.encoding).  M4 conformance gate is "100% of vectors
round-trip" against this file.

The loader normalizes the JSON structure into a uniform Vector dataclass
so spec tests can parametrize with @pytest.mark.parametrize("vec", ...)
without re-parsing every test.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

# Resolve the conformance vectors file relative to the repo root. The
# spec test rootdir is pysdk/, so the path is ../tests/conformance/...
_REPO_ROOT = Path(__file__).resolve().parents[5]
_VECTORS_PATH = _REPO_ROOT / "tests" / "conformance" / "encoding_vectors.json"


@dataclass(frozen=True)
class Vector:
    """One conformance vector — what to encode/decode and what bytes
    to expect."""

    id: str
    type: str | dict[str, Any]  # primitive name OR composite spec dict
    value: Any
    hex_bytes: str
    notes: str = ""

    @property
    def bytes(self) -> bytes:
        """Decoded hex_bytes ready for assertion."""
        return bytes.fromhex(self.hex_bytes.replace(" ", ""))


def _load_raw() -> list[dict[str, Any]]:
    """Read encoding_vectors.json and return the raw vectors array."""
    if not _VECTORS_PATH.is_file():
        raise FileNotFoundError(f"encoding_vectors.json not found at {_VECTORS_PATH}")
    with _VECTORS_PATH.open("r", encoding="utf-8") as f:
        data = json.load(f)
    if isinstance(data, dict) and "vectors" in data:
        return [v for v in data["vectors"] if not v.get("_disabled", False)]
    if isinstance(data, list):
        return [v for v in data if not v.get("_disabled", False)]
    raise ValueError(f"unexpected JSON structure in {_VECTORS_PATH}")


def _to_vector(raw: dict[str, Any]) -> Vector:
    """Normalize one raw entry to a Vector."""
    return Vector(
        id=raw.get("id", "<unknown>"),
        type=raw["type"],
        value=raw.get("value"),
        hex_bytes=raw.get("bytes", ""),
        notes=raw.get("notes", ""),
    )


def load_all_vectors() -> list[Vector]:
    """All non-disabled vectors (primitive + composite). Used by the
    full conformance test."""
    return [_to_vector(r) for r in _load_raw()]


def load_primitive_vectors() -> list[Vector]:
    """Primitive vectors only (vec.type is a string). Used by per-codec
    spec tests when present."""
    return [v for v in load_all_vectors() if isinstance(v.type, str)]


def load_composite_vectors() -> list[Vector]:
    """Composite vectors only (vec.type is a dict with 'kind')."""
    return [v for v in load_all_vectors() if isinstance(v.type, dict)]
