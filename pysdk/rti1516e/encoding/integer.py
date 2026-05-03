"""HLAinteger16/32/64 BE+LE codecs. Agent C implements per TASK-050.

Six concrete classes total. All MUST round-trip every integer vector in
tests/conformance/encoding_vectors.json byte-identical to the Go side.

OctetBoundary: 2 for 16-bit, 4 for 32-bit, 8 for 64-bit.
Encoding: two's complement, big- or little-endian per the type name.
"""

from __future__ import annotations

from typing import Any

from rti1516e.encoding._base import Codec, OctetBoundary


class HLAinteger16BE(Codec):
    """16-bit signed big-endian integer. OctetBoundary 2."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 2

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-050")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-050")


class HLAinteger16LE(Codec):
    """16-bit signed little-endian integer. OctetBoundary 2."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 2

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-050")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-050")


class HLAinteger32BE(Codec):
    """32-bit signed big-endian integer. OctetBoundary 4."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 4

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-050")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-050")


class HLAinteger32LE(Codec):
    """32-bit signed little-endian integer. OctetBoundary 4."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 4

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-050")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-050")


class HLAinteger64BE(Codec):
    """64-bit signed big-endian integer. OctetBoundary 8."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 8

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-050")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-050")


class HLAinteger64LE(Codec):
    """64-bit signed little-endian integer. OctetBoundary 8."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 8

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-050")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-050")
