"""HLAvariantRecord: discriminated union. Agent C implements per TASK-057.

Construction: HLAvariantRecord(
    discriminant=("name", discriminant_codec),
    variants={discriminant_value: ("variant_name", variant_codec)},
).

Wire format: discriminant encoded first, padded to its boundary, then
the active variant. OctetBoundary = max(discriminant.octet_boundary,
max(v.octet_boundary for v in variants.values())).

Python value mapping: dict {"discriminant": <value>, "variant": <value>}
keyed by discriminant value (Agent C may also expose typed dataclasses).
"""

from __future__ import annotations

from typing import Any

from rti1516e.encoding._base import Codec, OctetBoundary


class HLAvariantRecord(Codec):
    """Discriminated union of variants keyed by discriminant value."""

    def __init__(
        self,
        discriminant: tuple[str, Codec],
        variants: dict[Any, tuple[str, Codec]],
    ) -> None:
        self.discriminant_name, self.discriminant_codec = discriminant
        self.variants = variants

    @property
    def octet_boundary(self) -> OctetBoundary:
        boundaries = [self.discriminant_codec.octet_boundary]
        boundaries.extend(c.octet_boundary for _, c in self.variants.values())
        return max(boundaries) if boundaries else 1

    def encode(self, value: Any) -> bytes:
        raise NotImplementedError("TASK-057")

    def decode(self, data: bytes, offset: int = 0) -> tuple[Any, int]:
        raise NotImplementedError("TASK-057")
