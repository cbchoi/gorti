from __future__ import annotations

import io
import json
from pathlib import Path

import pytest

from verification.common import (
    NDJSONError,
    PerformanceSample,
    SemanticEvent,
    dump_ndjson,
    dumps_ndjson,
    load_ndjson,
    loads_ndjson,
)


def test_records_round_trip_as_canonical_ndjson() -> None:
    records = (
        SemanticEvent(0, "FM", "create", {"federation": "alpha"}),
        PerformanceSample(1, "throughput", 42.5, "events/s", "higher", {"size": 5}),
    )

    encoded = dumps_ndjson(records)
    decoded = loads_ndjson(encoded)

    assert decoded == records
    assert encoded.endswith("\n")
    assert encoded.splitlines()[0] == json.dumps(
        records[0].to_dict(),
        ensure_ascii=False,
        allow_nan=False,
        sort_keys=True,
        separators=(",", ":"),
    )


def test_dump_ndjson_accepts_text_stream() -> None:
    destination = io.StringIO()
    dump_ndjson([SemanticEvent(0, "DM", "publish", {})], destination)
    assert loads_ndjson(destination.getvalue()) == (SemanticEvent(0, "DM", "publish", {}),)


def test_dump_and_load_ndjson_accept_path_strings(tmp_path: Path) -> None:
    destination = tmp_path / "events.ndjson"
    expected = (SemanticEvent(0, "TM", "grant", {"logical_time": 2.0}),)

    dump_ndjson(expected, str(destination))

    assert load_ndjson(str(destination)) == expected


@pytest.mark.parametrize(
    ("line", "message"),
    [
        ("\n", "blank lines"),
        ("[]\n", "JSON object"),
        ('{"record_type":"event"}\n', "missing required fields"),
        (
            '{"schema":"gorti.verification/v1","record_type":"event","sequence":0,'
            '"service":"OWN","event":"x","payload":{}}\n',
            "service must be one of",
        ),
    ],
)
def test_invalid_ndjson_reports_line_and_reason(line: str, message: str) -> None:
    with pytest.raises(NDJSONError, match=message) as error:
        loads_ndjson(line)
    assert "line 1" in str(error.value)


def test_non_finite_performance_value_is_rejected() -> None:
    with pytest.raises(NDJSONError, match="finite"):
        PerformanceSample(0, "latency", float("nan"), "ms", "lower").to_dict()
