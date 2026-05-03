"""HLAfixedArray: fixed-cardinality array. Agent C implements per TASK-054.

Construction: HLAfixedArray(element_codec=<Codec>, cardinality=N).
Wire format: N elements concatenated. OctetBoundary = element's
OctetBoundary. Padding inserted between elements when the element's
encoded length isn't a multiple of its OctetBoundary.

The element_codec is resolved at construction time by the dispatcher
(rti1516e.encoding.dispatch.codec_for); HLAfixedArray itself is type-
erased over the element type.
"""

from __future__ import annotations

from typing import Any

from rti1516e.encoding._base import Codec, OctetBoundary


class HLAfixedArray(Codec):
    """Array of ``cardinality`` elements, each encoded by ``element``."""

    def __init__(self, element: Codec, cardinality: int) -> None:
        self.element = element
        self.cardinality = cardinality

    @property
    def octet_boundary(self) -> OctetBoundary:
        return self.element.octet_boundary

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-054")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-054")
