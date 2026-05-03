"""TASK-052 internal test for byte-family codecs.

Exercises HLAoctet, HLAoctetPairBE/LE, HLAboolean, HLAASCIIchar,
HLAunicodeChar directly (not through codec_for, which is W3 / TASK-059).

Reads the byte-family rows from tests/conformance/encoding_vectors.json
and confirms byte-identical encode + round-trip decode == original value.

This file is agent-owned per TASK-052 dispatch — NOT under
tests/spec/m4/, so it does not violate the spec-test freeze.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

from rti1516e.encoding._base import Codec
from rti1516e.encoding.byte_codec import (
    HLAASCIIchar,
    HLAboolean,
    HLAoctet,
    HLAoctetPairBE,
    HLAoctetPairLE,
    HLAunicodeChar,
)
from rti1516e.errors import EncodingInsufficientBytes, EncodingTypeMismatch

_REPO_ROOT = Path(__file__).resolve().parents[2]
_VECTORS_PATH = _REPO_ROOT / "tests" / "conformance" / "encoding_vectors.json"

_BYTE_FAMILY_TYPES = {
    "HLAoctet": HLAoctet(),
    "HLAoctetPairBE": HLAoctetPairBE(),
    "HLAoctetPairLE": HLAoctetPairLE(),
    "HLAboolean": HLAboolean(),
    "HLAASCIIchar": HLAASCIIchar(),
    "HLAunicodeChar": HLAunicodeChar(),
}


def _load_byte_family_vectors() -> list[dict[str, Any]]:
    with _VECTORS_PATH.open("r", encoding="utf-8") as f:
        data = json.load(f)
    raw_vectors = data["vectors"] if isinstance(data, dict) else data
    out: list[dict[str, Any]] = []
    for row in raw_vectors:
        if row.get("_disabled"):
            continue
        if not isinstance(row.get("type"), str):
            continue
        if row["type"] not in _BYTE_FAMILY_TYPES:
            continue
        out.append(row)
    return out


_VECTORS = _load_byte_family_vectors()


def _id(row: dict[str, Any]) -> str:
    val = row.get("id", "<unknown>")
    return str(val)


@pytest.mark.parametrize("row", _VECTORS, ids=_id)
def test_byte_family_round_trip(row: dict[str, Any]) -> None:
    codec: Codec = _BYTE_FAMILY_TYPES[row["type"]]
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


def test_byte_family_vectors_loaded() -> None:
    assert len(_VECTORS) > 0, (
        "no byte-family vectors loaded — check tests/conformance/encoding_vectors.json"
    )


# --- Short-buffer behavior --------------------------------------------------


def test_octet_short_buffer() -> None:
    with pytest.raises(EncodingInsufficientBytes):
        HLAoctet().decode(b"")


def test_octet_pair_be_short_buffer() -> None:
    with pytest.raises(EncodingInsufficientBytes):
        HLAoctetPairBE().decode(b"\x01")


def test_octet_pair_le_short_buffer() -> None:
    with pytest.raises(EncodingInsufficientBytes):
        HLAoctetPairLE().decode(b"\x01")


def test_boolean_short_buffer() -> None:
    with pytest.raises(EncodingInsufficientBytes):
        HLAboolean().decode(b"\x00\x00\x00")


def test_ascii_char_short_buffer() -> None:
    with pytest.raises(EncodingInsufficientBytes):
        HLAASCIIchar().decode(b"")


def test_unicode_char_short_buffer() -> None:
    with pytest.raises(EncodingInsufficientBytes):
        HLAunicodeChar().decode(b"\x00")


# --- Range / type checks ----------------------------------------------------


def test_octet_out_of_range() -> None:
    with pytest.raises(EncodingTypeMismatch):
        HLAoctet().encode(256)
    with pytest.raises(EncodingTypeMismatch):
        HLAoctet().encode(-1)


def test_boolean_rejects_non_bool() -> None:
    with pytest.raises(EncodingTypeMismatch):
        HLAboolean().encode(1)
    with pytest.raises(EncodingTypeMismatch):
        HLAboolean().encode("true")


def test_boolean_decodes_nonzero_as_true() -> None:
    decoded, n = HLAboolean().decode(b"\x00\x00\x00\x05")
    assert decoded is True
    assert n == 4


def test_ascii_char_rejects_non_ascii() -> None:
    with pytest.raises(EncodingTypeMismatch):
        HLAASCIIchar().encode("Ω")  # U+03A9
    with pytest.raises(EncodingTypeMismatch):
        HLAASCIIchar().decode(b"\x80")


def test_unicode_char_rejects_surrogate() -> None:
    with pytest.raises(EncodingTypeMismatch):
        HLAunicodeChar().decode(b"\xd8\x00")


def test_unicode_char_rejects_non_bmp() -> None:
    # U+1F600 (😀) is outside BMP and cannot be a single Python char of len 1
    # in the way HLAunicodeChar requires here. ord("😀") > 0xFFFF.
    with pytest.raises(EncodingTypeMismatch):
        HLAunicodeChar().encode("\U0001F600")


# --- Boundary properties ----------------------------------------------------


def test_octet_boundaries() -> None:
    assert HLAoctet().octet_boundary == 1
    assert HLAoctetPairBE().octet_boundary == 2
    assert HLAoctetPairLE().octet_boundary == 2
    assert HLAboolean().octet_boundary == 4
    assert HLAASCIIchar().octet_boundary == 1
    assert HLAunicodeChar().octet_boundary == 2


# --- Offset + alignment behavior --------------------------------------------


def test_octet_decode_with_offset() -> None:
    decoded, n = HLAoctet().decode(b"\xaa\xbb\xcc", offset=1)
    assert decoded == 0xBB
    assert n == 1


def test_boolean_decode_with_offset_and_alignment() -> None:
    # data is [pad, pad, pad, pad, 00, 00, 00, 01]; offset=2 is unaligned —
    # aligned_offset(2, 4) == 4 — so payload starts at byte 4. Bytes
    # consumed = (4 - 2) + 4 = 6.
    data = b"\xff\xff\xff\xff\x00\x00\x00\x01"
    decoded, n = HLAboolean().decode(data, offset=2)
    assert decoded is True
    assert n == 6


def test_octet_pair_be_decode_with_offset() -> None:
    decoded, n = HLAoctetPairBE().decode(b"\x00\x00\xab\xcd", offset=2)
    assert decoded == [0xAB, 0xCD]
    assert n == 2


def test_octet_pair_le_decode_with_offset() -> None:
    # LE: wire bytes [0xCD, 0xAB] decode to logical [0xAB, 0xCD].
    decoded, n = HLAoctetPairLE().decode(b"\x00\x00\xcd\xab", offset=2)
    assert decoded == [0xAB, 0xCD]
    assert n == 2
