"""HLAfloat32/64 BE+LE codecs (IEEE 754). Agent C implements per TASK-051.

Module is named float_codec to avoid shadowing the float built-in.
OctetBoundary: 4 for 32-bit, 8 for 64-bit.
"""

from __future__ import annotations

from typing import Any

from rti1516e.encoding._base import Codec, OctetBoundary


class HLAfloat32BE(Codec):
    """IEEE-754 single-precision big-endian. OctetBoundary 4."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 4

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-051")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-051")


class HLAfloat32LE(Codec):
    """IEEE-754 single-precision little-endian. OctetBoundary 4."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 4

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-051")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-051")


class HLAfloat64BE(Codec):
    """IEEE-754 double-precision big-endian. OctetBoundary 8."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 8

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-051")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-051")


class HLAfloat64LE(Codec):
    """IEEE-754 double-precision little-endian. OctetBoundary 8."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 8

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-051")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-051")
