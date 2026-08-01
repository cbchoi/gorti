from __future__ import annotations

import json
import unittest
from pathlib import Path

COMMON = Path(__file__).resolve().parents[1]


class SchemaTests(unittest.TestCase):
    def test_result_schema_is_versioned_and_requires_60_runs(self) -> None:
        schema = json.loads((COMMON / "result-schema-v1.json").read_text(encoding="utf-8"))
        self.assertEqual(
            schema["properties"]["schema_version"]["const"],
            "gorti.benchmark.results/v1",
        )
        self.assertEqual(schema["properties"]["runs"]["minItems"], 60)
        self.assertEqual(schema["properties"]["runs"]["maxItems"], 60)
        self.assertFalse(schema["additionalProperties"])
        evidence = schema["properties"]["runs"]["items"]["properties"]["evidence"]
        required = set(evidence["required"])
        self.assertIn("workload_sha256", required)
        self.assertIn("workload_instance_sha256", required)
        self.assertIn("attribute_callback_sha256", required)
        self.assertIn("interaction_callback_sha256", required)
        self.assertIn("callback_trace_sha256", required)
        self.assertIn("start_synchronized", required)
        self.assertNotIn("ordered_callback_sha256", required)
        self.assertEqual(evidence["properties"]["start_synchronized"]["const"], True)

    def test_analysis_schema_records_bootstrap_method(self) -> None:
        schema = json.loads((COMMON / "analysis-schema-v1.json").read_text(encoding="utf-8"))
        self.assertEqual(
            schema["properties"]["analysis_schema_version"]["const"],
            "gorti.benchmark.analysis/v1",
        )
        bootstrap = schema["properties"]["bootstrap"]["properties"]
        self.assertEqual(bootstrap["prng"]["const"], "splitmix64")
        self.assertEqual(
            bootstrap["paired_resampling"]["const"],
            "stratified-by-execution-order",
        )


if __name__ == "__main__":
    unittest.main()
