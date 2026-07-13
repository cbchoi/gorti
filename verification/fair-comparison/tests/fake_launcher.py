from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path

PACKAGE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PACKAGE_ROOT))

from fair_comparison.contract import RESULT_SCHEMA, canonical_json, sha256_json  # noqa: E402


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser()
    result.add_argument("--implementation", required=True, choices=("pitch", "go"))
    result.add_argument("--fom", required=True, type=Path)
    result.add_argument("--seed", required=True, type=int)
    result.add_argument("--count", required=True, type=int)
    result.add_argument("--server-event-log", required=True)
    result.add_argument("--output", required=True, type=Path)
    result.add_argument("--run-id", required=True)
    result.add_argument("--workload", required=True, type=Path)
    return result


def file_sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> int:
    args = parser().parse_args()
    workload = json.loads(args.workload.read_text(encoding="utf-8"))
    assert args.seed == workload["seed"] == 1516
    assert args.count == workload["count"]
    assert args.server_event_log == workload["server_event_log"]
    assert file_sha256(args.fom) == workload["fom_sha256"]
    projection = [
        {"service": service, "record": {"event": f"{service.lower()}-pass"}}
        for service in ("FM", "DM", "OM", "TM")
    ]
    expected = 2 * args.count
    value = {
        "schema": RESULT_SCHEMA,
        "run_id": args.run_id,
        "implementation": args.implementation,
        "workload": workload,
        "semantics": {
            "normalization": "gorti.fm-dm-om-tm-projection/v1",
            "canonical_projection": projection,
            "projection_sha256": sha256_json(projection),
            "status": "pass",
        },
        "provenance": {
            "commit": "fake",
            "binary_sha256": file_sha256(Path(sys.executable)),
            "runtime_versions": {"python": sys.version.split()[0]},
            "build_flags": [],
            "exact_argv": sys.argv,
            "environment": {},
        },
        "metrics": [
            {
                "name": "completed_delivery_batch_latency",
                "unit": "ns",
                "direction": "lower",
                "sample_scope": "subscriber_pre_tar_to_both_callbacks",
                "dimensions": {},
                "samples": [10, 20, 30] if args.implementation == "pitch" else [20, 40, 60],
            }
        ],
        "accounting": {
            "expected_fanout": expected,
            "delivered": expected,
            "explicitly_rejected": 0,
            "dropped": 0,
            "duplicates": 0,
            "invalid": 0,
        },
    }
    args.output.mkdir(parents=True, exist_ok=True)
    (args.output / "result.json").write_text(
        canonical_json(value) + "\n", encoding="utf-8", newline="\n"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
