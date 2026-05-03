"""HLAfixedRecord: ordered named fields. Agent C implements per TASK-056.

Construction: HLAfixedRecord(fields=[(name, codec), ...]).
Wire format: each field encoded in order, each padded to its codec's
OctetBoundary BEFORE encoding. OctetBoundary of the record = the max
of all field boundaries.

Python value mapping: dict[str, Any] keyed by field name (preferred for
spec tests; Agent C may also accept dataclass instances).
"""

from __future__ import annotations

from typing import Any

from rti1516e.encoding._base import Codec, OctetBoundary


class HLAfixedRecord(Codec):
    """Record with a fixed list of named fields."""

    def __init__(self, fields: list[tuple[str, Codec]]) -> None:
        self.fields = fields

    @property
    def octet_boundary(self) -> OctetBoundary:
        if not self.fields:
            return 1
        return max(c.octet_boundary for _, c in self.fields)

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-056")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-056")
