from __future__ import annotations

import hashlib
import json
import threading
from pathlib import Path
from typing import Any, TextIO

SERVICES = frozenset({"FM", "DM", "OM", "TM"})
KINDS = frozenset({"meta", "semantic", "metric"})


def seeded_payload(seed: int, channel: str, index: int) -> str:
    """Language-neutral deterministic payload used by Java and Python."""
    source = f"{seed}:{channel}:{index}".encode()
    return hashlib.sha256(source).hexdigest()[:16]


class NdjsonLog:
    def __init__(self, target: str | Path | TextIO) -> None:
        self._owns_stream = not hasattr(target, "write")
        self._stream = (
            Path(target).open("w", encoding="utf-8", newline="\n")  # noqa: SIM115
            if self._owns_stream
            else target
        )
        self._lock = threading.Lock()
        self._seq = 0

    def close(self) -> None:
        if self._owns_stream:
            self._stream.close()

    def __enter__(self) -> NdjsonLog:
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    def write(self, kind: str, **fields: Any) -> dict[str, Any]:
        if kind not in KINDS:
            raise ValueError(f"unknown log kind: {kind}")
        with self._lock:
            record = {"kind": kind, "seq": self._seq, **fields}
            self._seq += 1
            self._stream.write(
                json.dumps(record, sort_keys=True, separators=(",", ":")) + "\n"
            )
            self._stream.flush()
            return record

    def semantic(
        self,
        service: str,
        event: str,
        actor: str,
        data: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        if service not in SERVICES:
            raise ValueError(f"unknown HLA service group: {service}")
        return self.write(
            "semantic", service=service, event=event, actor=actor, data=data or {}
        )

    def metric(
        self, service: str, metric: str, value: float, unit: str
    ) -> dict[str, Any]:
        if service not in SERVICES:
            raise ValueError(f"unknown HLA service group: {service}")
        return self.write(
            "metric", service=service, metric=metric, value=value, unit=unit
        )
