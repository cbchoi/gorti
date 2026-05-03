"""TASK-056 + TASK-057 internal tests for record-family codecs.

Exercises HLAfixedRecord and HLAvariantRecord directly (not through
codec_for, which is W3 / TASK-059). Constructs each record manually from
the JSON vector spec, then verifies byte-identical encode + round-trip
decode. Mirrors the agent-owned pattern used by W1 (e.g. test_byte_codec).

Reads the record rows from tests/conformance/encoding_vectors.json and
filters to vectors whose `type.kind` is HLAfixedRecord or
HLAvariantRecord.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

from rti1516e.encoding._base import Codec
from rti1516e.encoding.byte_codec import HLAoctet
from rti1516e.encoding.fixed_record import HLAfixedRecord
from rti1516e.encoding.float_codec import HLAfloat64BE
from rti1516e.encoding.integer import HLAinteger32BE
from rti1516e.encoding.variant_record import HLAvariantRecord
from rti1516e.errors import EncodingInsufficientBytes, EncodingTypeMismatch

_REPO_ROOT = Path(__file__).resolve().parents[2]
_VECTORS_PATH = _REPO_ROOT / "tests" / "conformance" / "encoding_vectors.json"


# Map of primitive type names to constructor — local mini-dispatcher used
# only by these tests so we don't depend on W3's full dispatch.codec_for.
_PRIM: dict[str, Codec] = {
    "HLAoctet": HLAoctet(),
    "HLAfloat64BE": HLAfloat64BE(),
    "HLAinteger32BE": HLAinteger32BE(),
}


def _build_codec(spec: Any) -> Codec:
    """Build a Codec from a JSON-vector type spec (string OR dict)."""
    if isinstance(spec, str):
        if spec not in _PRIM:
            raise KeyError(f"primitive {spec!r} not registered for record tests")
        return _PRIM[spec]
    if isinstance(spec, dict):
        kind = spec.get("kind")
        if kind == "HLAfixedRecord":
            fields = [(f["name"], _build_codec(f["type"])) for f in spec["fields"]]
            return HLAfixedRecord(fields)
        if kind == "HLAvariantRecord":
            disc_codec = _build_codec(spec["discriminator"])
            # Vectors model alternatives as a list; collapse to a dict keyed
            # by discriminant value. The variant "name" field of the codec
            # constructor mirrors the JSON-vector dict key ("value"), since
            # all alternatives share that key in encoding_vectors.json.
            variants: dict[Any, tuple[str, Codec]] = {}
            for alt in spec["alternatives"]:
                key = alt["discriminant"]
                variants[key] = ("value", _build_codec(alt["type"]))
            return HLAvariantRecord(
                discriminant=("discriminator", disc_codec),
                variants=variants,
            )
        raise KeyError(f"composite kind {kind!r} not supported by record tests")
    raise TypeError(f"unsupported type spec: {spec!r}")


def _load_record_vectors() -> list[dict[str, Any]]:
    with _VECTORS_PATH.open("r", encoding="utf-8") as f:
        data = json.load(f)
    raw = data["vectors"] if isinstance(data, dict) else data
    out: list[dict[str, Any]] = []
    for row in raw:
        if row.get("_disabled"):
            continue
        spec = row.get("type")
        if not isinstance(spec, dict):
            continue
        if spec.get("kind") not in ("HLAfixedRecord", "HLAvariantRecord"):
            continue
        out.append(row)
    return out


_VECTORS = _load_record_vectors()


def _id(row: dict[str, Any]) -> str:
    return str(row.get("id", "<unknown>"))


def _normalize_disc_value(v: Any) -> Any:
    """Coerce discriminator values from JSON (float) to int for stable
    dict equality. The JSON loader gives us 1.0 for an int32 disc; the
    HLAinteger32BE.decode emits a Python int. The vector's expected
    `discriminator` in the value dict is also a Python number — make
    them match by canonicalizing through the codec, which is what
    HLAvariantRecord itself does.
    """
    if isinstance(v, float) and v.is_integer():
        return int(v)
    return v


@pytest.mark.parametrize("row", _VECTORS, ids=_id)
def test_record_round_trip(row: dict[str, Any]) -> None:
    codec = _build_codec(row["type"])
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

    # Decoded discriminator is a canonical Python int (HLAinteger32BE
    # decode -> int); the JSON vector's `value` dict may have either
    # form. Normalize both sides for comparison.
    if isinstance(value, dict) and "discriminator" in value:
        norm_value = dict(value)
        norm_value["discriminator"] = _normalize_disc_value(value["discriminator"])
        assert decoded == norm_value, (
            f"vector {row['id']}: decoded != expected\n"
            f"  expected: {norm_value!r}\n"
            f"  got:      {decoded!r}"
        )
    else:
        assert decoded == value, (
            f"vector {row['id']}: decoded != expected\n"
            f"  expected: {value!r}\n"
            f"  got:      {decoded!r}"
        )


def test_record_vectors_loaded() -> None:
    assert len(_VECTORS) > 0, (
        "no record vectors loaded — check tests/conformance/encoding_vectors.json"
    )


# --- HLAfixedRecord direct properties ---------------------------------------


def test_fixed_record_octet_boundary_max_of_fields() -> None:
    codec = HLAfixedRecord(
        [("a", HLAoctet()), ("b", HLAfloat64BE()), ("c", HLAinteger32BE())]
    )
    # max(1, 8, 4) = 8
    assert codec.octet_boundary == 8


def test_fixed_record_empty_has_boundary_one_and_empty_bytes() -> None:
    codec = HLAfixedRecord([])
    assert codec.octet_boundary == 1
    assert codec.encode({}) == b""
    decoded, n = codec.decode(b"")
    assert decoded == {}
    assert n == 0


def test_fixed_record_missing_field_raises_type_mismatch() -> None:
    codec = HLAfixedRecord([("x", HLAoctet()), ("y", HLAoctet())])
    with pytest.raises(EncodingTypeMismatch):
        codec.encode({"x": 1})


def test_fixed_record_non_dict_input_raises_type_mismatch() -> None:
    codec = HLAfixedRecord([("x", HLAoctet())])
    with pytest.raises(EncodingTypeMismatch):
        codec.encode([1])


def test_fixed_record_short_buffer_raises_insufficient_bytes() -> None:
    # Two int32 fields would need 8 bytes; give 4.
    codec = HLAfixedRecord(
        [("x", HLAinteger32BE()), ("y", HLAinteger32BE())]
    )
    with pytest.raises(EncodingInsufficientBytes):
        codec.decode(b"\x00\x00\x00\x01")


def test_fixed_record_decode_with_offset() -> None:
    # Two-int32 record; place at offset 0 of a buffer that has 4 leading
    # bytes of garbage. The record begins at offset 4, which is already
    # aligned for int32 — so consumed = 8.
    codec = HLAfixedRecord(
        [("x", HLAinteger32BE()), ("y", HLAinteger32BE())]
    )
    data = b"\xff\xff\xff\xff\x00\x00\x00\x01\xff\xff\xff\xff"
    decoded, n = codec.decode(data, offset=4)
    assert decoded == {"x": 1, "y": -1}
    assert n == 8


# --- HLAvariantRecord direct properties -------------------------------------


def test_variant_record_octet_boundary_max_of_disc_and_alts() -> None:
    codec = HLAvariantRecord(
        discriminant=("discriminator", HLAinteger32BE()),
        variants={
            1: ("value", HLAoctet()),
            2: ("value", HLAfloat64BE()),
        },
    )
    # max(4, 1, 8) = 8
    assert codec.octet_boundary == 8


def test_variant_record_unknown_discriminator_on_encode_raises() -> None:
    codec = HLAvariantRecord(
        discriminant=("discriminator", HLAinteger32BE()),
        variants={1: ("value", HLAoctet())},
    )
    with pytest.raises(EncodingTypeMismatch):
        codec.encode({"discriminator": 99, "value": 0})


def test_variant_record_unknown_discriminator_on_decode_raises() -> None:
    codec = HLAvariantRecord(
        discriminant=("discriminator", HLAinteger32BE()),
        variants={1: ("value", HLAoctet())},
    )
    # Encoded discriminator = 7, no alt for it.
    with pytest.raises(EncodingTypeMismatch):
        codec.decode(b"\x00\x00\x00\x07\x00")


def test_variant_record_missing_keys_raises_type_mismatch() -> None:
    codec = HLAvariantRecord(
        discriminant=("discriminator", HLAinteger32BE()),
        variants={1: ("value", HLAoctet())},
    )
    with pytest.raises(EncodingTypeMismatch):
        codec.encode({"discriminator": 1})  # missing "value"
    with pytest.raises(EncodingTypeMismatch):
        codec.encode({"value": 5})  # missing "discriminator"


def test_variant_record_non_dict_input_raises_type_mismatch() -> None:
    codec = HLAvariantRecord(
        discriminant=("discriminator", HLAinteger32BE()),
        variants={1: ("value", HLAoctet())},
    )
    with pytest.raises(EncodingTypeMismatch):
        codec.encode("not a dict")


def test_variant_record_short_buffer_raises_insufficient_bytes() -> None:
    codec = HLAvariantRecord(
        discriminant=("discriminator", HLAinteger32BE()),
        variants={2: ("value", HLAfloat64BE())},
    )
    # Discriminator decodes (= 2) but only 4 bytes of float64 follow
    # the 4 padding bytes — total 8, but record needs 16.
    short = b"\x00\x00\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00"
    with pytest.raises(EncodingInsufficientBytes):
        codec.decode(short)


def test_variant_record_decode_with_offset() -> None:
    codec = HLAvariantRecord(
        discriminant=("discriminator", HLAinteger32BE()),
        variants={1: ("value", HLAoctet())},
    )
    # Wire: [garbage 0xff x4, disc=1 (4 bytes), value=0xab]; record
    # starts at offset 4 (already int32-aligned).
    data = b"\xff\xff\xff\xff\x00\x00\x00\x01\xab"
    decoded, n = codec.decode(data, offset=4)
    assert decoded == {"discriminator": 1, "value": 0xAB}
    assert n == 5
