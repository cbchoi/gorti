"""Unit tests for the integer codec family (TASK-050, M4 W1A).

These tests cover edge cases and error paths that are NOT exercised by
``tests/conformance/encoding_vectors.json`` (which is the cross-language
byte-identical gate). The conformance suite asserts the happy round-trip
for representative values; this file asserts:

- per-class octet_boundary
- decode at non-zero offset (used by composite codecs)
- decode short-buffer raises EncodingInsufficientBytes
- encode rejects out-of-range / wrong-type values
- encode accepts JSON-style integral floats (parity with Go's toIntNN)
- encode rejects bool (HLAboolean has its own codec; conflation is a bug)
- encode rejects fractional / non-finite floats
"""

from __future__ import annotations

import struct
from typing import Any

import pytest

from rti1516e.encoding.integer import (
    HLAinteger16BE,
    HLAinteger16LE,
    HLAinteger32BE,
    HLAinteger32LE,
    HLAinteger64BE,
    HLAinteger64LE,
)
from rti1516e.errors import EncodingInsufficientBytes, EncodingTypeMismatch

# ----- (codec_class, width_bytes, low, high, octet_boundary) ----------------

INT16_RANGE = (-32768, 32767)
INT32_RANGE = (-2147483648, 2147483647)
INT64_RANGE = (-9223372036854775808, 9223372036854775807)

ALL_INT_CODECS: list[tuple[type, int, int, int, int]] = [
    (HLAinteger16BE, 2, INT16_RANGE[0], INT16_RANGE[1], 2),
    (HLAinteger16LE, 2, INT16_RANGE[0], INT16_RANGE[1], 2),
    (HLAinteger32BE, 4, INT32_RANGE[0], INT32_RANGE[1], 4),
    (HLAinteger32LE, 4, INT32_RANGE[0], INT32_RANGE[1], 4),
    (HLAinteger64BE, 8, INT64_RANGE[0], INT64_RANGE[1], 8),
    (HLAinteger64LE, 8, INT64_RANGE[0], INT64_RANGE[1], 8),
]


@pytest.mark.parametrize(
    ("codec_cls", "_width", "_low", "_high", "boundary"), ALL_INT_CODECS
)
def test_octet_boundary_matches_width(
    codec_cls: type, _width: int, _low: int, _high: int, boundary: int
) -> None:
    codec = codec_cls()
    assert codec.octet_boundary == boundary


@pytest.mark.parametrize(
    ("codec_cls", "width", "low", "high", "_boundary"), ALL_INT_CODECS
)
def test_round_trip_at_extremes_and_zero(
    codec_cls: type, width: int, low: int, high: int, _boundary: int
) -> None:
    codec = codec_cls()
    for value in (0, 1, -1, low, high):
        encoded = codec.encode(value)
        assert len(encoded) == width
        decoded, n = codec.decode(encoded)
        assert n == width
        assert decoded == value


@pytest.mark.parametrize(
    ("codec_cls", "width", "_low", "_high", "_boundary"), ALL_INT_CODECS
)
def test_decode_at_non_zero_offset_skips_prefix(
    codec_cls: type, width: int, _low: int, _high: int, _boundary: int
) -> None:
    """Composite codecs decode mid-buffer — make sure offset works."""
    codec = codec_cls()
    encoded = codec.encode(42)
    # Wrap with three leading garbage bytes; decode must ignore them.
    buf = b"\xff\xee\xdd" + encoded + b"\x00\x00"
    decoded, n = codec.decode(buf, offset=3)
    assert n == width
    assert decoded == 42


@pytest.mark.parametrize(
    ("codec_cls", "width", "_low", "_high", "_boundary"), ALL_INT_CODECS
)
def test_decode_short_buffer_raises_insufficient_bytes(
    codec_cls: type, width: int, _low: int, _high: int, _boundary: int
) -> None:
    codec = codec_cls()
    with pytest.raises(EncodingInsufficientBytes):
        codec.decode(b"\x00" * (width - 1))
    # Same when offset puts us past the end.
    full = codec.encode(0)
    with pytest.raises(EncodingInsufficientBytes):
        codec.decode(full, offset=1)


@pytest.mark.parametrize(
    ("codec_cls", "_width", "low", "high", "_boundary"), ALL_INT_CODECS
)
def test_encode_rejects_out_of_range(
    codec_cls: type, _width: int, low: int, high: int, _boundary: int
) -> None:
    codec = codec_cls()
    with pytest.raises(OverflowError):
        codec.encode(high + 1)
    with pytest.raises(OverflowError):
        codec.encode(low - 1)


@pytest.mark.parametrize(
    ("codec_cls", "_width", "_low", "_high", "_boundary"), ALL_INT_CODECS
)
def test_encode_accepts_integral_float(
    codec_cls: type, _width: int, _low: int, _high: int, _boundary: int
) -> None:
    """JSON-loaded vector values arrive as float; integral floats must work."""
    codec = codec_cls()
    encoded_int = codec.encode(7)
    encoded_float = codec.encode(7.0)
    assert encoded_int == encoded_float


@pytest.mark.parametrize(
    ("codec_cls", "_width", "_low", "_high", "_boundary"), ALL_INT_CODECS
)
def test_encode_rejects_fractional_float(
    codec_cls: type, _width: int, _low: int, _high: int, _boundary: int
) -> None:
    codec = codec_cls()
    with pytest.raises(EncodingTypeMismatch):
        codec.encode(1.5)


@pytest.mark.parametrize(
    ("codec_cls", "_width", "_low", "_high", "_boundary"), ALL_INT_CODECS
)
def test_encode_rejects_non_finite_float(
    codec_cls: type, _width: int, _low: int, _high: int, _boundary: int
) -> None:
    codec = codec_cls()
    for bad in (float("nan"), float("inf"), float("-inf")):
        with pytest.raises(EncodingTypeMismatch):
            codec.encode(bad)


@pytest.mark.parametrize(
    ("codec_cls", "_width", "_low", "_high", "_boundary"), ALL_INT_CODECS
)
def test_encode_rejects_bool(
    codec_cls: type, _width: int, _low: int, _high: int, _boundary: int
) -> None:
    """bool is an int subclass in Python — but HLAboolean has its own
    codec; accepting bool here would mask a category error at composite
    record sites."""
    codec = codec_cls()
    with pytest.raises(EncodingTypeMismatch):
        codec.encode(True)
    with pytest.raises(EncodingTypeMismatch):
        codec.encode(False)


@pytest.mark.parametrize(
    ("codec_cls", "_width", "_low", "_high", "_boundary"), ALL_INT_CODECS
)
def test_encode_rejects_non_numeric(
    codec_cls: type, _width: int, _low: int, _high: int, _boundary: int
) -> None:
    codec = codec_cls()
    for bad in ("42", b"\x00\x00", None, [1], object()):
        with pytest.raises(EncodingTypeMismatch):
            codec.encode(bad)


# ----- BE vs LE byte-order spot-checks --------------------------------------
# Cross-checks against struct.pack independent of the codec's own struct
# constants — defends against an accidental endianness swap.


@pytest.mark.parametrize(
    ("codec_cls", "fmt", "value"),
    [
        (HLAinteger16BE, ">h", 0x1234),
        (HLAinteger16LE, "<h", 0x1234),
        (HLAinteger32BE, ">i", 0x12345678),
        (HLAinteger32LE, "<i", 0x12345678),
        (HLAinteger64BE, ">q", 0x123456789ABCDEF0),
        (HLAinteger64LE, "<q", 0x123456789ABCDEF0),
    ],
)
def test_byte_order_matches_struct(codec_cls: type, fmt: str, value: int) -> None:
    codec = codec_cls()
    assert codec.encode(value) == struct.pack(fmt, value)


def test_be_le_pair_differ_for_multibyte_value() -> None:
    """Sanity: BE and LE produce mirrored bytes for non-palindromic input."""
    be = HLAinteger32BE().encode(0x01020304)
    le = HLAinteger32LE().encode(0x01020304)
    assert be == bytes.fromhex("01020304")
    assert le == bytes.fromhex("04030201")


def test_decode_returns_python_int() -> None:
    """Decoded scalar should be a plain int (not numpy / bytes / etc)."""
    for codec_cls, _w, _lo, _hi, _b in ALL_INT_CODECS:
        codec = codec_cls()
        decoded, _ = codec.decode(codec.encode(1))
        assert isinstance(decoded, int)
        assert not isinstance(decoded, bool)


# ----- Determinism gate (echoes Codec ABC docstring) ------------------------


@pytest.mark.parametrize(
    ("codec_cls", "_width", "_low", "_high", "_boundary"), ALL_INT_CODECS
)
def test_encode_is_deterministic(
    codec_cls: type, _width: int, _low: int, _high: int, _boundary: int
) -> None:
    codec = codec_cls()
    value: Any = 12345
    out_a = codec.encode(value)
    out_b = codec.encode(value)
    assert out_a == out_b
