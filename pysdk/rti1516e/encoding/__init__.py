"""IEEE 1516.2-2010 encoding rules — Python mirror of Agent B's Go encoder.

Public API:

    from rti1516e.encoding import Codec, codec_for
    from rti1516e.encoding.dispatch import parse_type_spec

Per-codec modules expose the concrete classes (one per HLA encoding type).
The full byte-identical contract against Go is tested by
pysdk/tests/spec/m4/test_spec_m4_encoding_conformance.py iterating
tests/conformance/encoding_vectors.json.

Agent C wires the implementations across TASK-050..059. This __init__ is
FROZEN — Agent C may extend the public exports list but must not rename
the existing names.
"""

from __future__ import annotations

from rti1516e.encoding._base import Codec, OctetBoundary
from rti1516e.encoding.dispatch import codec_for, parse_type_spec

__all__ = [
    "Codec",
    "OctetBoundary",
    "codec_for",
    "parse_type_spec",
]
