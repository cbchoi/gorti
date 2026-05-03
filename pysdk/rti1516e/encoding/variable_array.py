"""HLAvariableArray: dynamically-sized array. Agent C implements per TASK-055.

Wire format: 4-byte HLAinteger32BE length prefix, then N elements
concatenated (with per-element padding to element's OctetBoundary).

OctetBoundary: max(4, element.octet_boundary) — the length prefix is
4-byte aligned but the first element pads to its own boundary after.
"""

from __future__ import annotations

from typing import Any

from rti1516e.encoding._base import Codec, OctetBoundary


class HLAvariableArray(Codec):
    """Variable-length array of elements encoded by ``element``."""

    def __init__(self, element: Codec) -> None:
        self.element = element

    @property
    def octet_boundary(self) -> OctetBoundary:
        return max(4, self.element.octet_boundary)

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-055")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-055")
