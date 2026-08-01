"""Unit tests for HLAfixedArray and HLAvariableArray (TASK-054 and TASK-055).

Covers:

- octet_boundary semantics (inherits element / max(4, element))
- byte-identical round-trip for every array vector in
  tests/conformance/encoding_vectors.json
- decode at non-zero offset (used by composite codecs)
- short-buffer raises EncodingInsufficientBytes
- wrong-typed input raises EncodingTypeMismatch (cardinality mismatch
  for fixed_array, non-list-or-tuple for both)
- determinism: encode is a pure function of value
- inter-element padding for variable_array with float64 (4-byte length
  prefix + 4 bytes pad to reach 8-byte float boundary)
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

from rti1516e.encoding._base import Codec
from rti1516e.encoding.byte_codec import HLAoctet
from rti1516e.encoding.fixed_array import HLAfixedArray
from rti1516e.encoding.float_codec import HLAfloat64BE
from rti1516e.encoding.integer import HLAinteger16BE, HLAinteger32BE
from rti1516e.encoding.variable_array import HLAvariableArray
from rti1516e.errors import EncodingInsufficientBytes, EncodingTypeMismatch

# --- vector loader ---------------------------------------------------------

REPO_ROOT = Path(__file__).resolve().parents[2]
ENCODING_VECTORS_PATH = REPO_ROOT / "tests" / "conformance" / "encoding_vectors.json"


def _primitive_codec(name: str) -> Codec:
    """Resolve a primitive type-name string to a freshly-constructed codec.

    W3 will replace this with the real dispatcher; here we only need the
    primitives that actually appear as `element` in the array vectors.
    """
    table: dict[str, type[Codec]] = {
        "HLAinteger16BE": HLAinteger16BE,
        "HLAinteger32BE": HLAinteger32BE,
        "HLAfloat64BE": HLAfloat64BE,
        "HLAoctet": HLAoctet,
    }
    return table[name]()


def _load_array_vectors(kind: str) -> list[dict[str, Any]]:
    with ENCODING_VECTORS_PATH.open() as f:
        data = json.load(f)
    out: list[dict[str, Any]] = []
    for vec in data["vectors"]:
        t = vec.get("type")
        if isinstance(t, dict) and t.get("kind") == kind:
            out.append(vec)
    return out


FIXED_ARRAY_VECTORS = _load_array_vectors("HLAfixedArray")
VARIABLE_ARRAY_VECTORS = _load_array_vectors("HLAvariableArray")


# --- HLAfixedArray ---------------------------------------------------------


def test_fixed_array_octet_boundary_inherits_element() -> None:
    assert HLAfixedArray(HLAoctet(), 3).octet_boundary == 1
    assert HLAfixedArray(HLAinteger32BE(), 2).octet_boundary == 4
    assert HLAfixedArray(HLAfloat64BE(), 5).octet_boundary == 8


def test_fixed_array_negative_cardinality_rejected() -> None:
    with pytest.raises(ValueError, match="negative cardinality"):
        HLAfixedArray(HLAinteger32BE(), -1)


def test_fixed_array_zero_cardinality_round_trips() -> None:
    codec = HLAfixedArray(HLAinteger32BE(), 0)
    assert codec.encode([]) == b""
    decoded, n = codec.decode(b"")
    assert decoded == []
    assert n == 0


@pytest.mark.parametrize("vec", FIXED_ARRAY_VECTORS, ids=lambda v: v["id"])
def test_fixed_array_vector_round_trips(vec: dict[str, Any]) -> None:
    spec = vec["type"]
    codec = HLAfixedArray(
        _primitive_codec(spec["element"]),
        spec["cardinality"],
    )
    expected = bytes.fromhex(vec["bytes"])
    encoded = codec.encode(vec["value"])
    assert encoded == expected, (
        f"vector {vec['id']}: encoded != expected\n"
        f"  expected: {expected.hex()}\n"
        f"  got:      {encoded.hex()}"
    )
    decoded, n = codec.decode(expected)
    assert n == len(expected)
    assert decoded == vec["value"]


def test_fixed_array_decode_at_non_zero_offset() -> None:
    codec = HLAfixedArray(HLAinteger32BE(), 3)
    payload = codec.encode([1, 2, 3])
    buf = b"\xff\xee\xdd" + payload + b"\x00\x00"
    decoded, n = codec.decode(buf, offset=3)
    assert n == len(payload)
    assert decoded == [1, 2, 3]


def test_fixed_array_cardinality_mismatch_raises() -> None:
    codec = HLAfixedArray(HLAinteger32BE(), 3)
    with pytest.raises(EncodingTypeMismatch, match="expected 3"):
        codec.encode([1, 2])
    with pytest.raises(EncodingTypeMismatch, match="expected 3"):
        codec.encode([1, 2, 3, 4])


def test_fixed_array_rejects_non_sequence() -> None:
    codec = HLAfixedArray(HLAinteger32BE(), 2)
    for bad in ("ab", b"\x00\x00", None, 42, {1: 2}):
        with pytest.raises(EncodingTypeMismatch):
            codec.encode(bad)


def test_fixed_array_decode_short_buffer_raises() -> None:
    codec = HLAfixedArray(HLAinteger32BE(), 3)
    # Only 2 of 3 ints present.
    with pytest.raises(EncodingInsufficientBytes):
        codec.decode(b"\x00\x00\x00\x01\x00\x00\x00\x02")


def test_fixed_array_accepts_tuple() -> None:
    codec = HLAfixedArray(HLAinteger32BE(), 3)
    assert codec.encode((1, 2, 3)) == codec.encode([1, 2, 3])


def test_fixed_array_encode_is_deterministic() -> None:
    codec = HLAfixedArray(HLAinteger32BE(), 3)
    a = codec.encode([10, 20, 30])
    b = codec.encode([10, 20, 30])
    assert a == b


def test_fixed_array_int16_padding_between_elements() -> None:
    """int16 has 2-byte boundary; 2 ints encoded back-to-back fit exactly,
    no padding required. Sanity that boundary handling is correct."""
    codec = HLAfixedArray(HLAinteger16BE(), 2)
    encoded = codec.encode([1, 2])
    assert encoded == b"\x00\x01\x00\x02"


# --- HLAvariableArray ------------------------------------------------------


def test_variable_array_octet_boundary_max_4_with_element() -> None:
    assert HLAvariableArray(HLAoctet()).octet_boundary == 4
    assert HLAvariableArray(HLAinteger32BE()).octet_boundary == 4
    assert HLAvariableArray(HLAfloat64BE()).octet_boundary == 8


def test_variable_array_empty_encodes_just_length_prefix() -> None:
    codec = HLAvariableArray(HLAinteger32BE())
    assert codec.encode([]) == b"\x00\x00\x00\x00"
    decoded, n = codec.decode(b"\x00\x00\x00\x00")
    assert decoded == []
    assert n == 4


@pytest.mark.parametrize("vec", VARIABLE_ARRAY_VECTORS, ids=lambda v: v["id"])
def test_variable_array_vector_round_trips(vec: dict[str, Any]) -> None:
    spec = vec["type"]
    codec = HLAvariableArray(_primitive_codec(spec["element"]))
    expected = bytes.fromhex(vec["bytes"])
    encoded = codec.encode(vec["value"])
    assert encoded == expected, (
        f"vector {vec['id']}: encoded != expected\n"
        f"  expected: {expected.hex()}\n"
        f"  got:      {encoded.hex()}"
    )
    decoded, n = codec.decode(expected)
    assert n == len(expected)
    assert decoded == vec["value"]


def test_variable_array_pads_to_element_boundary() -> None:
    """float64 element: 4-byte length prefix + 4-byte pad + 8-byte double."""
    codec = HLAvariableArray(HLAfloat64BE())
    encoded = codec.encode([1.0])
    # 4 bytes length (=1) + 4 bytes pad + 8 bytes double 1.0
    assert encoded == bytes.fromhex("00000001") + b"\x00\x00\x00\x00" + bytes.fromhex(
        "3ff0000000000000"
    )


def test_variable_array_decode_at_non_zero_offset() -> None:
    codec = HLAvariableArray(HLAinteger32BE())
    payload = codec.encode([7, 8])
    buf = b"\xff\xee\xdd" + payload + b"\x00"
    decoded, n = codec.decode(buf, offset=3)
    assert n == len(payload)
    assert decoded == [7, 8]


def test_variable_array_decode_short_length_prefix_raises() -> None:
    codec = HLAvariableArray(HLAinteger32BE())
    with pytest.raises(EncodingInsufficientBytes):
        codec.decode(b"\x00\x00\x00")


def test_variable_array_decode_short_payload_raises() -> None:
    codec = HLAvariableArray(HLAinteger32BE())
    # Says 2 elements but only carries 1.
    with pytest.raises(EncodingInsufficientBytes):
        codec.decode(b"\x00\x00\x00\x02\x00\x00\x00\x01")


def test_variable_array_rejects_non_sequence() -> None:
    codec = HLAvariableArray(HLAinteger32BE())
    for bad in ("ab", b"\x00\x00", None, 42, {1: 2}):
        with pytest.raises(EncodingTypeMismatch):
            codec.encode(bad)


def test_variable_array_accepts_tuple() -> None:
    codec = HLAvariableArray(HLAinteger32BE())
    assert codec.encode((1, 2)) == codec.encode([1, 2])


def test_variable_array_encode_is_deterministic() -> None:
    codec = HLAvariableArray(HLAinteger32BE())
    a = codec.encode([10, 20, 30])
    b = codec.encode([10, 20, 30])
    assert a == b


# --- nested arrays ---------------------------------------------------------


def test_fixed_array_of_variable_array_round_trip() -> None:
    """Composability: HLAfixedArray of HLAvariableArray should compose."""
    inner = HLAvariableArray(HLAinteger32BE())
    outer = HLAfixedArray(inner, 2)
    value = [[1, 2], [3]]
    encoded = outer.encode(value)
    decoded, n = outer.decode(encoded)
    assert n == len(encoded)
    assert decoded == value


def test_variable_array_of_fixed_array_round_trip() -> None:
    """And the dual: HLAvariableArray of HLAfixedArray."""
    inner = HLAfixedArray(HLAinteger32BE(), 2)
    outer = HLAvariableArray(inner)
    value = [[1, 2], [3, 4], [5, 6]]
    encoded = outer.encode(value)
    decoded, n = outer.decode(encoded)
    assert n == len(encoded)
    assert decoded == value
