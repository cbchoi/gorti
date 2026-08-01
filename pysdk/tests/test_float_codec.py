"""Tests for HLAfloat32/64 BE and LE codecs (TASK-051, M4 W1B).

These tests complement the specification suite under
``pysdk/tests/spec/m4/``. They exercise the float codecs in isolation,
without depending on Wave 3's ``codec_for`` dispatch:

  1. Round-trip every float vector from
     ``tests/conformance/encoding_vectors.json`` directly through the
     concrete codec class — covers the same byte-identical contract as
     the spec test will once ``codec_for`` is wired.
  2. Spot-check ``EncodingInsufficientBytes`` for short input.
  3. Spot-check ``encode`` accepts ``int``/``bool`` (Python's natural
     ``float()`` widening) — mirrors Go's ``asFloat64`` policy.
  4. Spot-check ``encode`` rejects non-numeric input with ``TypeError``.
  5. Spot-check ``offset`` decoding from the middle of a buffer.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

from rti1516e.encoding._base import Codec
from rti1516e.encoding.float_codec import (
    HLAfloat32BE,
    HLAfloat32LE,
    HLAfloat64BE,
    HLAfloat64LE,
)
from rti1516e.errors import EncodingInsufficientBytes

# --- Vector loading (no dependency on the spec _fakes loader) ----------------

_REPO_ROOT = Path(__file__).resolve().parents[2]
_VECTORS_PATH = _REPO_ROOT / "tests" / "conformance" / "encoding_vectors.json"

_FLOAT_TYPES: dict[str, Codec] = {
    "HLAfloat32BE": HLAfloat32BE(),
    "HLAfloat32LE": HLAfloat32LE(),
    "HLAfloat64BE": HLAfloat64BE(),
    "HLAfloat64LE": HLAfloat64LE(),
}


def _load_float_vectors() -> list[tuple[str, str, float, bytes]]:
    raw = json.loads(_VECTORS_PATH.read_text(encoding="utf-8"))
    entries = raw["vectors"] if isinstance(raw, dict) else raw
    out: list[tuple[str, str, float, bytes]] = []
    for v in entries:
        if v.get("_disabled"):
            continue
        t = v.get("type")
        if not isinstance(t, str) or t not in _FLOAT_TYPES:
            continue
        out.append((v["id"], t, float(v["value"]), bytes.fromhex(v["bytes"].replace(" ", ""))))
    return out


_VECTORS = _load_float_vectors()


@pytest.mark.parametrize(
    ("vec_id", "type_name", "value", "expected_bytes"),
    _VECTORS,
    ids=[v[0] for v in _VECTORS],
)
def test_float_vector_round_trip(
    vec_id: str, type_name: str, value: float, expected_bytes: bytes
) -> None:
    """Each conformance vector encodes byte-identically and decodes
    back to the same float value (for exactly-representable cases)."""
    codec = _FLOAT_TYPES[type_name]
    encoded = codec.encode(value)
    assert encoded == expected_bytes, (
        f"vector {vec_id}: encoded != expected\n"
        f"  expected: {expected_bytes.hex()}\n"
        f"  got:      {encoded.hex()}"
    )
    decoded, n = codec.decode(expected_bytes)
    expected_n = len(expected_bytes)
    assert n == expected_n, f"vector {vec_id}: decode consumed {n}, expected {expected_n}"
    assert decoded == value, f"vector {vec_id}: decoded {decoded!r} != {value!r}"


def test_float_vectors_loaded() -> None:
    """Sanity: at least one vector per float type is loaded."""
    by_type: dict[str, int] = dict.fromkeys(_FLOAT_TYPES, 0)
    for _, t, _, _ in _VECTORS:
        by_type[t] += 1
    for t, n in by_type.items():
        assert n > 0, f"no vectors loaded for {t}"


# --- OctetBoundary -----------------------------------------------------------


def test_octet_boundaries() -> None:
    assert HLAfloat32BE().octet_boundary == 4
    assert HLAfloat32LE().octet_boundary == 4
    assert HLAfloat64BE().octet_boundary == 8
    assert HLAfloat64LE().octet_boundary == 8


# --- Short-input handling ----------------------------------------------------


@pytest.mark.parametrize(
    ("codec", "min_len"),
    [
        (HLAfloat32BE(), 4),
        (HLAfloat32LE(), 4),
        (HLAfloat64BE(), 8),
        (HLAfloat64LE(), 8),
    ],
)
def test_decode_short_buffer_raises(codec: Codec, min_len: int) -> None:
    short = b"\x00" * (min_len - 1)
    with pytest.raises(EncodingInsufficientBytes):
        codec.decode(short)


def test_decode_offset_past_end_raises() -> None:
    codec = HLAfloat64BE()
    buf = b"\x00" * 8
    with pytest.raises(EncodingInsufficientBytes):
        codec.decode(buf, offset=1)


# --- Type-acceptance policy --------------------------------------------------


@pytest.mark.parametrize("codec", list(_FLOAT_TYPES.values()))
def test_encode_accepts_int(codec: Codec) -> None:
    """``int`` widens to ``float`` per Go's asFloat32/64 policy."""
    encoded_int = codec.encode(1)
    encoded_float = codec.encode(1.0)
    assert encoded_int == encoded_float


@pytest.mark.parametrize("codec", list(_FLOAT_TYPES.values()))
def test_encode_accepts_bool(codec: Codec) -> None:
    """``bool`` is a subclass of ``int``; ``float(True) == 1.0``."""
    assert codec.encode(True) == codec.encode(1.0)
    assert codec.encode(False) == codec.encode(0.0)


@pytest.mark.parametrize("codec", list(_FLOAT_TYPES.values()))
def test_encode_rejects_non_numeric(codec: Codec) -> None:
    with pytest.raises(TypeError):
        codec.encode("1.0")
    with pytest.raises(TypeError):
        codec.encode(None)
    with pytest.raises(TypeError):
        codec.encode([1.0])


# --- Offset decoding (composite-codec callers) -------------------------------


def test_decode_at_offset_consumes_only_payload() -> None:
    codec = HLAfloat32BE()
    payload = codec.encode(0.5)  # 4 bytes
    framed = b"\xaa\xbb" + payload + b"\xcc"
    value, n = codec.decode(framed, offset=2)
    assert value == 0.5
    assert n == 4


def test_decode_64_at_offset_consumes_only_payload() -> None:
    codec = HLAfloat64LE()
    payload = codec.encode(2.0)  # 8 bytes
    framed = b"\x01\x02\x03" + payload + b"\xff"
    value, n = codec.decode(framed, offset=3)
    assert value == 2.0
    assert n == 8


# --- Determinism -------------------------------------------------------------


@pytest.mark.parametrize("codec", list(_FLOAT_TYPES.values()))
def test_encode_is_deterministic(codec: Codec) -> None:
    """Two encodes of the same value yield identical bytes."""
    value: Any = 0.25
    assert codec.encode(value) == codec.encode(value)
