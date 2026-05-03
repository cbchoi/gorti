"""HLAASCIIstring, HLAunicodeString. Agent C implements per TASK-053.

Both are length-prefixed: 4-byte HLAinteger32BE length, then the bytes
(ASCII for the first, UTF-16BE for the second).

OctetBoundary: 4 (the length prefix is 4-byte aligned).
"""

from __future__ import annotations

from typing import Any

from rti1516e.encoding._base import Codec, OctetBoundary


class HLAASCIIstring(Codec):
    """ASCII-encoded string with HLAinteger32BE length prefix. OctetBoundary 4."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 4

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-053")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-053")


class HLAunicodeString(Codec):
    """UTF-16BE-encoded string with HLAinteger32BE length prefix.

    Length is the number of UTF-16 code units (NOT bytes); the byte length
    on the wire is 4 + 2*length. OctetBoundary 4.
    """

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 4

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-053")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-053")
