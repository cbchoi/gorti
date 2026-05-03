"""Cross-language byte-identical encoding conformance — the M4 encoding gate.

For every non-disabled vector in tests/conformance/encoding_vectors.json:
  1. Encode the value through rti1516e.encoding.codec_for(type) and
     assert the bytes match exactly.
  2. Decode the bytes back and assert the resulting value matches.

Implements: FR-ENC-2 (Python); M4 exit criterion #3.
"""

from __future__ import annotations

import pytest

from rti1516e.encoding import codec_for

from .conftest import all_vectors

ALL_VECTORS = all_vectors()


@pytest.mark.spec
@pytest.mark.parametrize("vec", ALL_VECTORS, ids=lambda v: v.id)
def test_spec_m4_vector_round_trip(vec):  # type: ignore[no-untyped-def]
    codec = codec_for(vec.type)
    encoded = codec.encode(vec.value)
    assert encoded == vec.bytes, (
        f"vector {vec.id}: encoded != expected\n"
        f"  expected: {vec.bytes.hex()}\n"
        f"  got:      {encoded.hex()}"
    )
    decoded, n = codec.decode(vec.bytes)
    assert n == len(vec.bytes), f"vector {vec.id}: decode consumed {n}, expected {len(vec.bytes)}"
    assert decoded == vec.value, (
        f"vector {vec.id}: decoded != expected\n"
        f"  expected: {vec.value!r}\n"
        f"  got:      {decoded!r}"
    )


@pytest.mark.spec
def test_spec_m4_vectors_loaded() -> None:
    """Sanity: encoding_vectors.json was found and loaded."""
    assert len(ALL_VECTORS) > 0, "no vectors loaded — check tests/conformance/encoding_vectors.json"
