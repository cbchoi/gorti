"""TASK-053 internal test for string codecs.

Exercises HLAASCIIstring + HLAunicodeString directly (not through
codec_for, which is W3 / TASK-059).

Reads the string-family rows from tests/conformance/encoding_vectors.json
and confirms byte-identical encode + round-trip decode == original value.

This file is agent-owned per TASK-053 dispatch — NOT under
tests/spec/m4/, so it does not violate the spec-test freeze.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

from rti1516e.encoding._base import Codec
from rti1516e.encoding.string_codec import HLAASCIIstring, HLAunicodeString
from rti1516e.errors import EncodingInsufficientBytes, EncodingTypeMismatch

_REPO_ROOT = Path(__file__).resolve().parents[2]
_VECTORS_PATH = _REPO_ROOT / "tests" / "conformance" / "encoding_vectors.json"

_STRING_FAMILY_TYPES: dict[str, Codec] = {
    "HLAASCIIstring": HLAASCIIstring(),
    "HLAunicodeString": HLAunicodeString(),
}


def _load_string_family_vectors() -> list[dict[str, Any]]:
    with _VECTORS_PATH.open("r", encoding="utf-8") as f:
        data = json.load(f)
    raw_vectors = data["vectors"] if isinstance(data, dict) else data
    out: list[dict[str, Any]] = []
    for row in raw_vectors:
        if row.get("_disabled"):
            continue
        if not isinstance(row.get("type"), str):
            continue
        if row["type"] not in _STRING_FAMILY_TYPES:
            continue
        out.append(row)
    return out


_VECTORS = _load_string_family_vectors()


def _id(row: dict[str, Any]) -> str:
    val = row.get("id", "<unknown>")
    return str(val)


@pytest.mark.parametrize("row", _VECTORS, ids=_id)
def test_string_family_round_trip(row: dict[str, Any]) -> None:
    codec: Codec = _STRING_FAMILY_TYPES[row["type"]]
    expected_bytes = bytes.fromhex(row["bytes"].replace(" ", ""))
    value = row["value"]

    encoded = codec.encode(value)
    assert encoded == expected_bytes, (
        f"vector {row['id']}: encoded != expected\n"
        f"  expected: {expected_bytes.hex()}\n"
        f"  got:      {encoded.hex()}"
    )

    decoded, n = codec.decode(expected_bytes)
    assert n == len(expected_bytes), (
        f"vector {row['id']}: decode consumed {n}, expected {len(expected_bytes)}"
    )
    assert decoded == value, (
        f"vector {row['id']}: decoded != expected\n"
        f"  expected: {value!r}\n"
        f"  got:      {decoded!r}"
    )


def test_string_family_vectors_loaded() -> None:
    assert len(_VECTORS) > 0, (
        "no string-family vectors loaded — check tests/conformance/encoding_vectors.json"
    )


# --- Boundary properties ----------------------------------------------------


def test_ascii_string_boundary() -> None:
    assert HLAASCIIstring().octet_boundary == 4


def test_unicode_string_boundary() -> None:
    assert HLAunicodeString().octet_boundary == 4


# --- ASCII type / range checks ---------------------------------------------


def test_ascii_string_rejects_non_str() -> None:
    with pytest.raises(EncodingTypeMismatch):
        HLAASCIIstring().encode(123)
    with pytest.raises(EncodingTypeMismatch):
        HLAASCIIstring().encode(b"hello")
    with pytest.raises(EncodingTypeMismatch):
        HLAASCIIstring().encode(None)


def test_ascii_string_rejects_non_ascii_chars() -> None:
    with pytest.raises(EncodingTypeMismatch):
        HLAASCIIstring().encode("Ω")
    with pytest.raises(EncodingTypeMismatch):
        HLAASCIIstring().encode("hi\x80")


def test_ascii_string_decode_rejects_non_ascii_payload() -> None:
    # length prefix says 1 byte, payload is 0x80 (high bit set).
    with pytest.raises(EncodingTypeMismatch):
        HLAASCIIstring().decode(b"\x00\x00\x00\x01\x80")


# --- Unicode type checks ----------------------------------------------------


def test_unicode_string_rejects_non_str() -> None:
    with pytest.raises(EncodingTypeMismatch):
        HLAunicodeString().encode(123)
    with pytest.raises(EncodingTypeMismatch):
        HLAunicodeString().encode(b"hello")


def test_unicode_string_handles_supplementary_plane() -> None:
    # A non-BMP code point encodes as a UTF-16 surrogate pair (2 code units,
    # 4 bytes). The count prefix MUST be 2, not 1.
    s = "\U0001F600"  # GRINNING FACE
    encoded = HLAunicodeString().encode(s)
    assert encoded[:4] == b"\x00\x00\x00\x02"
    assert len(encoded) == 4 + 4
    decoded, n = HLAunicodeString().decode(encoded)
    assert decoded == s
    assert n == 8


# --- Short-buffer behavior --------------------------------------------------


def test_ascii_string_short_buffer_prefix() -> None:
    with pytest.raises(EncodingInsufficientBytes):
        HLAASCIIstring().decode(b"\x00\x00\x00")


def test_ascii_string_short_buffer_payload() -> None:
    # Length prefix says 5 bytes, but only 3 bytes follow.
    with pytest.raises(EncodingInsufficientBytes):
        HLAASCIIstring().decode(b"\x00\x00\x00\x05abc")


def test_unicode_string_short_buffer_prefix() -> None:
    with pytest.raises(EncodingInsufficientBytes):
        HLAunicodeString().decode(b"\x00\x00\x00")


def test_unicode_string_short_buffer_payload() -> None:
    # Length prefix says 3 code units (6 bytes), but only 4 follow.
    with pytest.raises(EncodingInsufficientBytes):
        HLAunicodeString().decode(b"\x00\x00\x00\x03\x00h\x00e")


# --- Offset / alignment behavior --------------------------------------------


def test_ascii_string_decode_with_offset_alignment() -> None:
    # Offset 2 is not 4-aligned; aligned_offset(2, 4) == 4. Then a 1-char
    # ASCII payload at byte 4. Bytes consumed = (4 - 2) + 4 + 1 = 7.
    data = b"\xff\xff\xff\xff\x00\x00\x00\x01A"
    decoded, n = HLAASCIIstring().decode(data, offset=2)
    assert decoded == "A"
    assert n == 7


def test_unicode_string_decode_with_offset_alignment() -> None:
    # Same alignment story: skip 2 padding bytes, then consume 4 + 2*1 = 6.
    data = b"\xff\xff\xff\xff\x00\x00\x00\x01\x00A"
    decoded, n = HLAunicodeString().decode(data, offset=2)
    assert decoded == "A"
    assert n == (4 - 2) + 6


# --- Empty-string edge cases ------------------------------------------------


def test_ascii_string_empty_round_trip() -> None:
    encoded = HLAASCIIstring().encode("")
    assert encoded == b"\x00\x00\x00\x00"
    decoded, n = HLAASCIIstring().decode(encoded)
    assert decoded == ""
    assert n == 4


def test_unicode_string_empty_round_trip() -> None:
    encoded = HLAunicodeString().encode("")
    assert encoded == b"\x00\x00\x00\x00"
    decoded, n = HLAunicodeString().decode(encoded)
    assert decoded == ""
    assert n == 4
