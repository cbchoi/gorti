"""HLAoctet, HLAoctetPairBE/LE, HLAboolean, HLAASCIIchar, HLAunicodeChar.

Agent C implements per TASK-052.

- HLAoctet: 1 byte, OctetBoundary 1
- HLAoctetPairBE/LE: 2 bytes, OctetBoundary 2
- HLAboolean: encoded as HLAinteger32BE (per IEEE 1516.2 §4.12), OctetBoundary 4
- HLAASCIIchar: 1 byte, OctetBoundary 1
- HLAunicodeChar: UTF-16BE 2 bytes, OctetBoundary 2
"""

from __future__ import annotations

from typing import Any

from rti1516e.encoding._base import Codec, OctetBoundary


class HLAoctet(Codec):
    """Single octet (uint8). OctetBoundary 1."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 1

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-052")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-052")


class HLAoctetPairBE(Codec):
    """Pair of octets in big-endian order. OctetBoundary 2."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 2

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-052")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-052")


class HLAoctetPairLE(Codec):
    """Pair of octets in little-endian order. OctetBoundary 2."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 2

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-052")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-052")


class HLAboolean(Codec):
    """Boolean encoded as HLAinteger32BE: 1 = true, 0 = false. OctetBoundary 4."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 4

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-052")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-052")


class HLAASCIIchar(Codec):
    """Single ASCII char (1 byte). OctetBoundary 1."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 1

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-052")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-052")


class HLAunicodeChar(Codec):
    """Single Unicode char as UTF-16BE (2 bytes). OctetBoundary 2."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 2

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-052")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-052")
