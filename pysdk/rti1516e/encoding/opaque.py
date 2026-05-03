"""HLAopaqueData: length-prefixed byte blob. Agent C implements per TASK-058.

Wire format: 4-byte HLAinteger32BE length, then ``length`` raw bytes.
No element typing. OctetBoundary 4.
"""

from __future__ import annotations

from typing import Any

from rti1516e.encoding._base import Codec, OctetBoundary


class HLAopaqueData(Codec):
    """Variable-length byte blob with a 4-byte length prefix."""

    @property
    def octet_boundary(self) -> OctetBoundary:
        return 4

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-058")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-058")
