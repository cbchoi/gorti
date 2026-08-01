"""Unit tests for HLAopaqueData (TASK-058).

Covers the conformance vectors plus boundary/error cases. The cross-language
byte-identical conformance gate lives in
pysdk/tests/spec/m4/test_spec_m4_encoding_conformance.py — those run once
W3 wires ``codec_for``. These tests exercise the codec directly so regressions
fail loudly even before the dispatcher is wired.
"""

from __future__ import annotations

import pytest

from rti1516e.encoding.opaque import HLAopaqueData
from rti1516e.errors import EncodingInsufficientBytes


@pytest.fixture
def codec() -> HLAopaqueData:
    return HLAopaqueData()


def test_octet_boundary_is_4(codec: HLAopaqueData) -> None:
    assert codec.octet_boundary == 4


# --- Encode -----------------------------------------------------------------


def test_encode_empty_bytes(codec: HLAopaqueData) -> None:
    assert codec.encode(b"") == b"\x00\x00\x00\x00"


def test_encode_empty_string(codec: HLAopaqueData) -> None:
    # Conformance vector ``opaque-empty`` uses the empty string.
    assert codec.encode("") == b"\x00\x00\x00\x00"


def test_encode_none_treated_as_empty(codec: HLAopaqueData) -> None:
    assert codec.encode(None) == b"\x00\x00\x00\x00"


def test_encode_hex_string_deadbeef(codec: HLAopaqueData) -> None:
    # Conformance vector ``opaque-deadbeef``.
    assert codec.encode("deadbeef") == bytes.fromhex("00000004deadbeef")


def test_encode_hex_string_three_bytes(codec: HLAopaqueData) -> None:
    # Conformance vector ``opaque-three-bytes``.
    assert codec.encode("010203") == bytes.fromhex("00000003010203")


def test_encode_bytes_payload(codec: HLAopaqueData) -> None:
    assert codec.encode(b"\xde\xad\xbe\xef") == bytes.fromhex("00000004deadbeef")


def test_encode_bytearray_payload(codec: HLAopaqueData) -> None:
    assert codec.encode(bytearray(b"\x01\x02\x03")) == bytes.fromhex("00000003010203")


def test_encode_memoryview_payload(codec: HLAopaqueData) -> None:
    assert codec.encode(memoryview(b"\x01\x02")) == bytes.fromhex("0000000201" + "02")


def test_encode_invalid_hex_raises(codec: HLAopaqueData) -> None:
    with pytest.raises(TypeError, match="hex"):
        codec.encode("not-hex-zz")


def test_encode_unsupported_type_raises(codec: HLAopaqueData) -> None:
    with pytest.raises(TypeError, match="HLAopaqueData"):
        codec.encode(12345)


# --- Decode -----------------------------------------------------------------


def test_decode_empty_payload(codec: HLAopaqueData) -> None:
    value, n = codec.decode(b"\x00\x00\x00\x00")
    assert value == b""
    assert n == 4


def test_decode_deadbeef(codec: HLAopaqueData) -> None:
    value, n = codec.decode(bytes.fromhex("00000004deadbeef"))
    assert value == b"\xde\xad\xbe\xef"
    assert n == 8


def test_decode_three_bytes(codec: HLAopaqueData) -> None:
    value, n = codec.decode(bytes.fromhex("00000003010203"))
    assert value == b"\x01\x02\x03"
    assert n == 7


def test_decode_with_offset(codec: HLAopaqueData) -> None:
    # Pretend the opaque payload starts after 3 unrelated leading bytes.
    buf = b"\x99\x99\x99" + bytes.fromhex("00000003010203") + b"\xff"
    value, n = codec.decode(buf, offset=3)
    assert value == b"\x01\x02\x03"
    assert n == 7


def test_decode_short_length_prefix_raises(codec: HLAopaqueData) -> None:
    with pytest.raises(EncodingInsufficientBytes, match="length prefix"):
        codec.decode(b"\x00\x00")


def test_decode_short_payload_raises(codec: HLAopaqueData) -> None:
    # Length says 4 but only 2 payload bytes present.
    with pytest.raises(EncodingInsufficientBytes, match="payload"):
        codec.decode(b"\x00\x00\x00\x04\xde\xad")


def test_decode_returns_independent_copy(codec: HLAopaqueData) -> None:
    """Mutating the source buffer must not change the decoded value."""
    src = bytearray(b"\x00\x00\x00\x02\xaa\xbb")
    value, _ = codec.decode(bytes(src))
    src[4] = 0x00
    assert value == b"\xaa\xbb"


# --- Round-trip -------------------------------------------------------------


@pytest.mark.parametrize(
    "payload",
    [b"", b"\x00", b"\xff" * 1, b"hello, world", bytes(range(256))],
)
def test_round_trip(codec: HLAopaqueData, payload: bytes) -> None:
    encoded = codec.encode(payload)
    decoded, n = codec.decode(encoded)
    assert decoded == payload
    assert n == len(encoded)
