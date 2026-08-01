from __future__ import annotations

# ruff: noqa: E402
import copy
import sys
import unittest
from pathlib import Path

COMMON = Path(__file__).resolve().parents[1]
if str(COMMON) not in sys.path:
    sys.path.insert(0, str(COMMON))

from benchmark_core import ResultValidationError, validate_result_document
from fixture_factory import make_result_document


class ResultValidationTests(unittest.TestCase):
    def assert_invalid(self, document: dict, message: str) -> None:
        with self.assertRaises(ResultValidationError) as context:
            validate_result_document(document)
        self.assertIn(message, str(context.exception))

    def test_accepts_complete_30_by_30_balanced_result(self) -> None:
        document = make_result_document()
        self.assertIs(validate_result_document(document), document)

    def test_rejects_fewer_than_30_runs_per_implementation(self) -> None:
        document = make_result_document()
        document["runs"].pop()
        self.assert_invalid(document, "expected exactly 60 measured runs")
        self.assert_invalid(document, "has 29 runs; expected 30")

    def test_rejects_unbalanced_execution_order(self) -> None:
        document = make_result_document()
        for run in document["runs"]:
            run["position"] = 1 if run["implementation"] == "portico" else 2
        self.assert_invalid(document, "execution order must be balanced")

    def test_rejects_balanced_but_grouped_ab_ba_order(self) -> None:
        document = make_result_document()
        for run in document["runs"]:
            first = "portico" if run["pair_index"] <= 15 else "gorti"
            run["position"] = 1 if run["implementation"] == first else 2

        self.assert_invalid(
            document,
            "pair[2]: execution order must follow 'paired-balanced-ab-ba'; "
            "expected ('gorti', 'portico'), got ('portico', 'gorti')",
        )

    def test_rejects_balanced_reversed_alternation(self) -> None:
        document = make_result_document()
        for run in document["runs"]:
            first = "gorti" if run["pair_index"] % 2 else "portico"
            run["position"] = 1 if run["implementation"] == first else 2

        self.assert_invalid(
            document,
            "pair[1]: execution order must follow 'paired-balanced-ab-ba'; "
            "expected ('portico', 'gorti'), got ('gorti', 'portico')",
        )

    def test_ab_ba_pattern_uses_configured_implementation_order(self) -> None:
        document = make_result_document()
        document["implementations"].reverse()
        for run in document["runs"]:
            first = "gorti" if run["pair_index"] % 2 else "portico"
            run["position"] = 1 if run["implementation"] == first else 2

        self.assertIs(validate_result_document(document), document)

    def test_rejects_reused_pair_seed(self) -> None:
        document = make_result_document()
        for run in document["runs"]:
            if run["pair_index"] == 2:
                run["seed"] = 151601
        self.assert_invalid(document, "workload seeds must be unique")

    def test_rejects_mismatched_seed_inside_pair(self) -> None:
        document = make_result_document()
        document["runs"][0]["seed"] += 1
        self.assert_invalid(document, "paired runs must use the same workload seed")

    def test_rejects_log_or_process_output_fields(self) -> None:
        document = make_result_document()
        document["runs"][0]["stdout"] = "not an analysis input"
        self.assert_invalid(document, "unexpected field 'stdout'")

    def test_rejects_non_finite_measurement(self) -> None:
        document = make_result_document()
        document["runs"][0]["measurements"]["latency_us"] = float("nan")
        self.assert_invalid(document, "expected a finite, non-negative number")

    def test_rejects_non_fresh_process_design(self) -> None:
        document = copy.deepcopy(make_result_document())
        document["experiment"]["process_model"] = "reused-process"
        self.assert_invalid(document, "expected 'fresh-process-per-run'")

    def test_rejects_incomplete_callback_accounting(self) -> None:
        document = make_result_document()
        document["runs"][0]["evidence"]["delivered_callbacks"] = 199
        self.assert_invalid(
            document, "delivered_callbacks must equal expected_callbacks"
        )

    def test_rejects_semantic_digest_mismatch_inside_pair(self) -> None:
        document = make_result_document()
        document["runs"][0]["evidence"]["interaction_callback_sha256"] = "a" * 64
        self.assert_invalid(
            document, "paired evidence differs for interaction_callback_sha256"
        )

    def test_rejects_callback_error_evidence(self) -> None:
        document = make_result_document()
        document["runs"][0]["evidence"]["duplicate_callbacks"] = 1
        self.assert_invalid(document, "duplicate_callbacks: expected zero")

    def test_rejects_missing_workload_instance_digest(self) -> None:
        document = make_result_document()
        del document["runs"][0]["evidence"]["workload_instance_sha256"]
        self.assert_invalid(
            document, "missing required field 'workload_instance_sha256'"
        )

    def test_rejects_reused_workload_instance_across_pairs(self) -> None:
        document = make_result_document()
        first_instance = document["runs"][0]["evidence"][
            "workload_instance_sha256"
        ]
        for run in document["runs"]:
            if run["pair_index"] == 2:
                run["evidence"]["workload_instance_sha256"] = first_instance
        self.assert_invalid(
            document, "workload_instance_sha256 must be unique across the 30 pairs"
        )

    def test_rejects_non_static_workload_digest(self) -> None:
        document = make_result_document()
        for run in document["runs"]:
            if run["pair_index"] == 2:
                run["evidence"]["workload_sha256"] = "a" * 64
        self.assert_invalid(
            document,
            "workload_sha256 must identify one static workload across all runs",
        )

    def test_rejects_static_workload_digest_that_differs_from_declaration(self) -> None:
        document = make_result_document()
        for run in document["runs"]:
            run["evidence"]["workload_sha256"] = "a" * 64
        self.assert_invalid(
            document,
            "workload_sha256 must match benchmark.configuration_sha256",
        )

    def test_rejects_fom_digest_that_differs_from_declaration(self) -> None:
        document = make_result_document()
        for run in document["runs"]:
            run["evidence"]["fom_sha256"] = "a" * 64
        self.assert_invalid(
            document,
            "fom_sha256 must match experiment.fom_sha256",
        )

    def test_rejects_any_failed_synchronization(self) -> None:
        for field in (
            "ready_synchronized",
            "start_synchronized",
            "measure_synchronized",
            "done_synchronized",
        ):
            with self.subTest(field=field):
                document = make_result_document()
                document["runs"][0]["evidence"][field] = False
                self.assert_invalid(document, f"{field}: expected true")

    def test_rejects_any_nonzero_callback_error_counter(self) -> None:
        for field in (
            "rejected_callbacks",
            "dropped_callbacks",
            "unexpected_callbacks",
            "duplicate_callbacks",
            "invalid_callbacks",
        ):
            with self.subTest(field=field):
                document = make_result_document()
                document["runs"][0]["evidence"][field] = 1
                self.assert_invalid(document, f"{field}: expected zero")

    def test_paired_runs_must_match_all_semantic_evidence(self) -> None:
        fields = (
            "fom_sha256",
            "workload_sha256",
            "workload_instance_sha256",
            "expected_callbacks",
            "delivered_callbacks",
            "attribute_callback_sha256",
            "interaction_callback_sha256",
            "callback_trace_sha256",
            "terminal_state_sha256",
        )
        for field in fields:
            with self.subTest(field=field):
                document = make_result_document()
                evidence = document["runs"][0]["evidence"]
                evidence[field] = (
                    evidence[field] + 1
                    if isinstance(evidence[field], int)
                    else "a" * 64
                )
                self.assert_invalid(document, f"paired evidence differs for {field}")


if __name__ == "__main__":
    unittest.main()
