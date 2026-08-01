# ruff: noqa: S101

from __future__ import annotations

import io

from verification.common.compare_logs import compare_records
from verification.common.schema import NdjsonLog, seeded_payload


def test_seeded_payload_is_stable() -> None:
    assert seeded_payload(1516, "interaction", 0) == "9cd4d361eb451c10"
    assert seeded_payload(1516, "interaction", 0) != seeded_payload(
        1516, "interaction", 1
    )


def test_semantics_ignore_runtime_fields_but_not_payload() -> None:
    def records(implementation: str, handle: int) -> list[dict[str, object]]:
        stream = io.StringIO()
        log = NdjsonLog(stream)
        for service in ("FM", "DM", "OM", "TM"):
            log.semantic(
                service,
                "USED",
                "publisher",
                {
                    "payload": "same",
                    "implementation": implementation,
                    "runtime_handle": handle,
                },
            )
        return [__import__("json").loads(line) for line in stream.getvalue().splitlines()]

    report = compare_records(records("reference_rti", 10), records("gorti", 99))
    assert report["semantic_match"] is True

    changed = records("gorti", 99)
    changed[2]["data"]["payload"] = "different"  # type: ignore[index]
    assert compare_records(records("reference_rti", 10), changed)["semantic_match"] is False
