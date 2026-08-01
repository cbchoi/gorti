"""In-memory v1 result documents used by benchmark/common tests."""

from __future__ import annotations

from typing import Any


def make_result_document() -> dict[str, Any]:
    implementations = [
        {
            "id": "portico",
            "label": "Portico RTI",
            "version": "test-1",
            "runtime": "Java test runtime",
            "artifact_sha256": "1" * 64,
            "source_revision": "portico-test-revision",
        },
        {
            "id": "gorti",
            "label": "gorti",
            "version": "test-1",
            "runtime": "Go test runtime",
            "artifact_sha256": "2" * 64,
            "source_revision": "gorti-test-revision",
        },
    ]
    runs: list[dict[str, Any]] = []
    for pair_index in range(1, 31):
        first = "portico" if pair_index % 2 else "gorti"
        second = "gorti" if first == "portico" else "portico"
        measurements = {
            "portico": {
                "latency_us": 100.0 + pair_index,
                "throughput_eps": 1000.0 + pair_index * 10.0,
            },
            "gorti": {
                "latency_us": 80.0 + pair_index,
                "throughput_eps": 1200.0 + pair_index * 10.0,
            },
        }
        for position, implementation in enumerate((first, second), start=1):
            runs.append(
                {
                    "run_id": f"run-{pair_index:02d}-{implementation}",
                    "pair_id": f"pair-{pair_index:02d}",
                    "pair_index": pair_index,
                    "implementation": implementation,
                    "position": position,
                    "seed": 151600 + pair_index,
                    "status": "ok",
                    "measurements": measurements[implementation],
                    "evidence": {
                        "fom_sha256": "4" * 64,
                        "workload_sha256": "3" * 64,
                        "workload_instance_sha256": f"{pair_index:064x}",
                        "expected_callbacks": 200,
                        "delivered_callbacks": 200,
                        "rejected_callbacks": 0,
                        "dropped_callbacks": 0,
                        "unexpected_callbacks": 0,
                        "duplicate_callbacks": 0,
                        "invalid_callbacks": 0,
                        "ready_synchronized": True,
                        "start_synchronized": True,
                        "measure_synchronized": True,
                        "done_synchronized": True,
                        "attribute_callback_sha256": f"{pair_index + 100:064x}",
                        "interaction_callback_sha256": f"{pair_index + 200:064x}",
                        "callback_trace_sha256": f"{pair_index + 300:064x}",
                        "terminal_state_sha256": "5" * 64,
                    },
                }
            )

    return {
        "schema_version": "gorti.benchmark.results/v1",
        "benchmark": {
            "name": "DEVStone-HLA",
            "version": "1.0",
            "profile": "two-federate-ping-pong",
            "configuration_sha256": "3" * 64,
        },
        "experiment": {
            "id": "devstone-hla-test",
            "created_at": "2026-07-22T12:00:00+09:00",
            "design": "paired-balanced-ab-ba",
            "runs_per_implementation": 30,
            "warmup_runs": 5,
            "base_seed": 1516,
            "fom_sha256": "4" * 64,
            "completion_boundary": "final-receive-callback",
            "process_model": "fresh-process-per-run",
        },
        "environment": {
            "host_id": "benchmark-host",
            "os": "test-os",
            "cpu": "test-cpu",
            "logical_cores": 8,
            "memory_bytes": 16 * 1024 * 1024 * 1024,
            "python_version": "3.test",
        },
        "implementations": implementations,
        "metrics": [
            {
                "id": "latency_us",
                "label": "Completed delivery latency",
                "unit": "us",
                "direction": "lower-is-better",
            },
            {
                "id": "throughput_eps",
                "label": "Completed event throughput",
                "unit": "events/s",
                "direction": "higher-is-better",
            },
        ],
        "runs": runs,
    }
