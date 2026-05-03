"""Type-string -> Codec dispatcher. Agent C implements per TASK-059.

The conformance test in pysdk/tests/spec/m4/test_spec_m4_encoding_conformance.py
iterates tests/conformance/encoding_vectors.json and calls codec_for(vec.type)
for each vector. The test passes when 100% of vectors round-trip
byte-identical to Go.

Two entry points:

- ``codec_for(spec)`` — accepts either a primitive type-name string
  (e.g. "HLAinteger32BE") or a parsed type spec dict
  (e.g. {"kind": "HLAfixedArray", "element": "HLAfloat64BE",
  "cardinality": 3}). Returns a configured Codec instance.

- ``parse_type_spec(raw)`` — normalize raw input (string OR dict) into a
  canonical type-spec dict. Useful for tests that need to introspect
  the resolved spec separately from constructing the codec.

Until Agent C wires this, both functions raise NotImplementedError.
"""

from __future__ import annotations

from typing import Any

from rti1516e.encoding._base import Codec


def codec_for(spec: str | dict[str, Any]) -> Codec:
    """Return a configured Codec for the given type spec.

    Examples:
        codec_for("HLAinteger32BE")
        codec_for({"kind": "HLAfixedArray", "element": "HLAoctet", "cardinality": 4})
        codec_for({"kind": "HLAvariantRecord",
                   "discriminant": {"name": "tag", "type": "HLAinteger32BE"},
                   "variants": {"1": {"name": "v1", "type": "HLAfloat64BE"}}})

    Raises NotImplementedError until TASK-059 wires the dispatch.
    """
    raise NotImplementedError("TASK-059 — codec_for dispatch not yet wired")


def parse_type_spec(raw: str | dict[str, Any]) -> dict[str, Any]:
    """Normalize a type spec (string or dict) into the canonical dict form.

    Strings normalize to ``{"kind": <name>}``; dicts pass through after
    field-name validation.

    Raises NotImplementedError until TASK-059 wires the parser.
    """
    raise NotImplementedError("TASK-059 — parse_type_spec not yet wired")
