"""Unit tests for ``rti1516e.encoding.dispatch`` (TASK-059).

Spec-level conformance lives under ``tests/spec/m4/`` (orchestrator-frozen).
This file exercises the dispatcher's input-validation branches and any
agent-internal helpers that aren't reachable from the conformance vectors
alone, per the M4 dispatch plan §6 hard rule on agent-owned tests.
"""

from __future__ import annotations

import pytest

from rti1516e.encoding import codec_for, parse_type_spec
from rti1516e.encoding.byte_codec import HLAoctet
from rti1516e.encoding.fixed_array import HLAfixedArray
from rti1516e.encoding.fixed_record import HLAfixedRecord
from rti1516e.encoding.float_codec import HLAfloat64BE
from rti1516e.encoding.integer import HLAinteger32BE
from rti1516e.encoding.opaque import HLAopaqueData
from rti1516e.encoding.variable_array import HLAvariableArray
from rti1516e.encoding.variant_record import HLAvariantRecord
from rti1516e.errors import EncodingTypeMismatch


class TestParseTypeSpec:
    """parse_type_spec: string -> {kind: ...}, dict pass-through, type errors."""

    def test_string_normalizes_to_kind_dict(self) -> None:
        assert parse_type_spec("HLAinteger32BE") == {"kind": "HLAinteger32BE"}

    def test_dict_with_kind_passes_through(self) -> None:
        spec = {"kind": "HLAfixedArray", "element": "HLAoctet", "cardinality": 4}
        assert parse_type_spec(spec) == spec

    def test_dict_without_kind_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="missing 'kind'"):
            parse_type_spec({"element": "HLAoctet"})

    def test_non_str_non_dict_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="must be str or dict"):
            parse_type_spec(123)  # type: ignore[arg-type]


class TestCodecForPrimitive:
    """codec_for accepts both bare-string and {'kind': ...} forms for
    primitives and returns the matching codec class instance."""

    def test_bare_string_int32be(self) -> None:
        c = codec_for("HLAinteger32BE")
        assert isinstance(c, HLAinteger32BE)

    def test_kind_dict_float64be(self) -> None:
        c = codec_for({"kind": "HLAfloat64BE"})
        assert isinstance(c, HLAfloat64BE)

    def test_unknown_kind_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="unknown type kind"):
            codec_for("NotARealCodec")


class TestCodecForFixedArray:
    def test_constructs_fixed_array(self) -> None:
        c = codec_for(
            {"kind": "HLAfixedArray", "element": "HLAoctet", "cardinality": 5}
        )
        assert isinstance(c, HLAfixedArray)
        assert c.cardinality == 5
        assert isinstance(c.element, HLAoctet)

    def test_missing_element_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="missing 'element'"):
            codec_for({"kind": "HLAfixedArray", "cardinality": 1})

    def test_missing_cardinality_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="missing 'cardinality'"):
            codec_for({"kind": "HLAfixedArray", "element": "HLAoctet"})

    def test_negative_cardinality_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="negative"):
            codec_for(
                {"kind": "HLAfixedArray", "element": "HLAoctet", "cardinality": -1}
            )

    def test_float_cardinality_accepted_when_integral(self) -> None:
        c = codec_for(
            {"kind": "HLAfixedArray", "element": "HLAoctet", "cardinality": 3.0}
        )
        assert isinstance(c, HLAfixedArray)
        assert c.cardinality == 3

    def test_fractional_cardinality_rejected(self) -> None:
        with pytest.raises(EncodingTypeMismatch):
            codec_for(
                {"kind": "HLAfixedArray", "element": "HLAoctet", "cardinality": 2.5}
            )

    def test_bool_cardinality_rejected(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="bool"):
            codec_for(
                {"kind": "HLAfixedArray", "element": "HLAoctet", "cardinality": True}
            )

    def test_recursive_element(self) -> None:
        c = codec_for(
            {
                "kind": "HLAfixedArray",
                "element": {
                    "kind": "HLAvariableArray",
                    "element": "HLAinteger32BE",
                },
                "cardinality": 2,
            }
        )
        assert isinstance(c, HLAfixedArray)
        assert isinstance(c.element, HLAvariableArray)
        assert isinstance(c.element.element, HLAinteger32BE)


class TestCodecForVariableArray:
    def test_constructs_variable_array(self) -> None:
        c = codec_for({"kind": "HLAvariableArray", "element": "HLAfloat64BE"})
        assert isinstance(c, HLAvariableArray)
        assert isinstance(c.element, HLAfloat64BE)

    def test_missing_element_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="missing 'element'"):
            codec_for({"kind": "HLAvariableArray"})


class TestCodecForFixedRecord:
    def test_constructs_record(self) -> None:
        c = codec_for(
            {
                "kind": "HLAfixedRecord",
                "fields": [
                    {"name": "a", "type": "HLAoctet"},
                    {"name": "b", "type": "HLAfloat64BE"},
                ],
            }
        )
        assert isinstance(c, HLAfixedRecord)
        assert [name for name, _ in c.fields] == ["a", "b"]
        assert isinstance(c.fields[0][1], HLAoctet)
        assert isinstance(c.fields[1][1], HLAfloat64BE)

    def test_missing_fields_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="missing or non-list"):
            codec_for({"kind": "HLAfixedRecord"})

    def test_field_missing_name_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="missing or non-string"):
            codec_for(
                {"kind": "HLAfixedRecord", "fields": [{"type": "HLAoctet"}]}
            )

    def test_field_missing_type_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="missing 'type'"):
            codec_for(
                {"kind": "HLAfixedRecord", "fields": [{"name": "a"}]}
            )


class TestCodecForVariantRecord:
    def test_constructs_variant(self) -> None:
        c = codec_for(
            {
                "kind": "HLAvariantRecord",
                "discriminator": "HLAinteger32BE",
                "alternatives": [
                    {"discriminant": 1, "type": "HLAoctet"},
                    {"discriminant": 2, "type": "HLAfloat64BE"},
                ],
            }
        )
        assert isinstance(c, HLAvariantRecord)
        # Variant-record uses literal "discriminator"/"value" field names
        # in the Python value-shape (matches Go side).
        assert c.discriminant_name == "discriminator"
        assert set(c.variants.keys()) == {1, 2}
        for _, (variant_name, _) in c.variants.items():
            assert variant_name == "value"

    def test_string_discriminator_keys_coerced_for_int_disc(self) -> None:
        c = codec_for(
            {
                "kind": "HLAvariantRecord",
                "discriminator": "HLAinteger32BE",
                "alternatives": [
                    {"discriminant": "1", "type": "HLAoctet"},
                    {"discriminant": "2", "type": "HLAfloat64BE"},
                ],
            }
        )
        assert isinstance(c, HLAvariantRecord)
        assert set(c.variants.keys()) == {1, 2}

    def test_missing_discriminator_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="discriminator"):
            codec_for(
                {
                    "kind": "HLAvariantRecord",
                    "alternatives": [{"discriminant": 1, "type": "HLAoctet"}],
                }
            )

    def test_unknown_discriminator_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="not a primitive"):
            codec_for(
                {
                    "kind": "HLAvariantRecord",
                    "discriminator": "NotAPrimitive",
                    "alternatives": [{"discriminant": 1, "type": "HLAoctet"}],
                }
            )

    def test_missing_alternatives_raises(self) -> None:
        with pytest.raises(EncodingTypeMismatch, match="alternatives"):
            codec_for(
                {
                    "kind": "HLAvariantRecord",
                    "discriminator": "HLAinteger32BE",
                }
            )


class TestCodecForOpaque:
    def test_constructs_opaque_adapter_with_hex_decode(self) -> None:
        c = codec_for({"kind": "HLAopaqueData"})
        # The wrapper still wires through the real opaque codec on encode.
        encoded = c.encode("deadbeef")
        assert encoded == b"\x00\x00\x00\x04\xde\xad\xbe\xef"
        decoded, n = c.decode(encoded)
        assert n == len(encoded)
        assert decoded == "deadbeef"

    def test_underlying_opaque_still_returns_bytes(self) -> None:
        # Sanity: the W1D-frozen codec is unchanged; only dispatch wraps it.
        raw = HLAopaqueData()
        decoded, _ = raw.decode(b"\x00\x00\x00\x02\x01\x02")
        assert decoded == b"\x01\x02"
