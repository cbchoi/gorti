"""Evidence-index safeguards, not HLA conformance tests."""

import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest


sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
SPEC = importlib.util.spec_from_file_location("requirements_review", Path(__file__).resolve().parents[1] / "ieee2010_requirements_review.py")
review = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(review)


class RequirementsReviewTests(unittest.TestCase):
    def test_applicable_linkage_reports_only_unlinked_applicable_rows(self):
        rows = [
            {"id": "missing", "applicability": "applicable", "tests": []},
            {"id": "linked", "applicability": "applicable", "tests": [{"name": "test_contract"}]},
            {"id": "excluded", "applicability": "not-applicable", "tests": []},
            {"id": "undecided", "applicability": "unresolved", "tests": []},
        ]
        self.assertEqual(review.applicable_linkage(rows), {
            "scope": "reviewed-applicable-records-only", "applicable": 2,
            "linked": 1, "unlinked_ids": ["missing"], "complete": False,
        })

    def test_complete_linkage_does_not_promote_semantic_verdict(self):
        rows = [{"id": "linked", "applicability": "applicable", "tests": [{"name": "test_contract"}], "semantic_verdict": "open"}]
        self.assertTrue(review.applicable_linkage(rows)["complete"])
        self.assertEqual(rows[0]["semantic_verdict"], "open")

    def test_empty_inventory_cannot_claim_complete_linkage(self):
        self.assertFalse(review.applicable_linkage([])["complete"])

    def test_supplemental_go_failure_is_not_hidden_by_historical_pass(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            old, fresh = root / "historical.jsonl", root / "fresh.jsonl"
            package, name = "example.org/rti/hla", "TestContract"
            for path, status in ((old, "pass"), (fresh, "fail")):
                path.write_text(json.dumps({"Package": package, "Test": name, "Action": status}) + "\n")
            index, artifacts = review.execution_index(go_path=old, supplemental_go=[fresh])
            self.assertEqual(index[(package, name)], ["pass", "fail"])
            self.assertEqual(index.invocations[(package, name)][1]["artifact"], "fresh.jsonl")
            self.assertEqual(artifacts[1]["kind"], "supplemental-go-json")

    def test_toc_and_service_usage_rows_are_not_headings(self):
        for text in ("4.2 Connect ....... 35", "4.2 Connect  35", "4.2 Connect Yes", "11 of IEEE Std 1516.1-2010"):
            self.assertIsNone(review.heading(text))
        self.assertEqual(review.heading("4.2.3 Preconditions"), ("4.2.3", "Preconditions"))
        self.assertEqual(review.heading("6.23 .Request Attribute Transportation Type Change service"),
                         ("6.23", "Request Attribute Transportation Type Change service"))

    def test_redline_units_never_become_authoritative(self):
        pages = [{"pdf_page": 33, "active_candidate": "5.1 Rule 1\n5.2 Rule 2"}]
        units = review.census_source({"id": "1516"}, pages)
        self.assertTrue(all(u["candidate_only"] for u in units))
        self.assertEqual(units[0]["start_page"], 33)

    def test_duplicate_heading_is_explicitly_ambiguous(self):
        pages = [{"pdf_page": 33, "active_candidate": "5.1 Rule 1\n5.1 Rule 1"}]
        units = review.census_source({"id": "1516"}, pages)
        self.assertTrue(units[0]["ambiguous"])

    def test_same_page_sibling_stops_at_its_heading_line(self):
        pages = [{"pdf_page": 184, "active_candidate": "8.1.3 Logical time\nSome body\n8.1.4 Time regulation\nOther body"}]
        units = review.census_source({"id": "1516.1"}, pages)
        first = next(u for u in units if u["clause"] == "8.1.3")
        self.assertEqual(first["end_page_inclusive_candidate"], 184)
        self.assertEqual(first["end_before_line_on_end_page_candidate"], 3)

    def test_java_parameterized_annotation_and_comment_guard(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "ContractTest.java"
            path.write_text('@ParameterizedTest(name="case {0}")\n@org.junit.jupiter.params.provider.ValueSource(strings={"one"})\nvoid contract(String value) {}\n// @Test void invented() {}\n', encoding="utf-8")
            self.assertTrue(review.declared(path, "contract"))
            self.assertFalse(review.declared(path, "invented"))

    def test_python_mixed_variants_cannot_be_credited(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / "pysdk/tests/test_contract.py"
            path.parent.mkdir(parents=True)
            path.write_text("def test_contract(): pass\n", encoding="utf-8")
            test = {"path": "pysdk/tests/test_contract.py", "name": "test_contract"}
            self.assertEqual(review.execution_status(test, {("tests.test_contract", "test_contract"): ["pass", "skip"]}, root), "nonpassing-variant")
            self.assertEqual(review.execution_status(test, {("tests.test_other", "test_contract"): ["pass"]}, root), "not-found")
            self.assertEqual(review.execution_status(test, {("tests.test_contract", "test_contract"): ["pass"]}, root), "passing-records-only-completeness-unassessed")

    def test_python_repository_test_retains_failed_variants(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / "tests/conformance/test_contract.py"
            path.parent.mkdir(parents=True)
            path.write_text("def test_contract(): pass\n", encoding="utf-8")
            test = {"path": "tests/conformance/test_contract.py", "name": "test_contract"}
            index = {("tests.conformance.test_contract", "test_contract"): ["pass", "fail"]}
            self.assertEqual(review.execution_status(test, index, root), "nonpassing-variant")
            self.assertEqual(review.execution_status(test, {("test_contract", "test_contract"): ["pass"]}, root), "not-found")

    def test_python_nested_pytest_root_is_not_an_arbitrary_basename_match(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            folder = root / "tests/conformance/ivct-subset"
            folder.mkdir(parents=True)
            (folder / "pytest.ini").write_text("[pytest]\n", encoding="utf-8")
            (folder / "tc_contract.py").write_text("def test_contract(): pass\n", encoding="utf-8")
            test = {"path": "tests/conformance/ivct-subset/tc_contract.py", "name": "test_contract"}
            index = {("tc_contract", "test_contract"): ["fail"]}
            self.assertEqual(review.execution_status(test, index, root), "nonpassing-variant")
            self.assertEqual(review.execution_status(test, {("other.tc_contract", "test_contract"): ["pass"]}, root), "not-found")

    def test_missing_oracle_and_unreviewed_exclusion_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            row = {"id": "R1", "source_id": "S1516", "clause": "5.1", "pdf_pages": [33],
                   "subject": "federation", "applicability": "not-applicable", "review_status": "candidate",
                   "statement": "Synthetic", "conditions": "Synthetic", "implementation": [], "tests": [], "open_items": []}
            data = {"schema_version": 1, "source_id": "S1516", "coverage_complete": False, "reviewed_units": [], "requirements": [row]}
            with self.assertRaises(review.TraceError):
                review.validate_slice(data, root, {"S1516": 41})
            row.update(applicability="applicable", tests=[{"path": "test.py", "name": "test_contract"}])
            with self.assertRaisesRegex(review.TraceError, "oracle"):
                review.validate_slice(data, root, {"S1516": 41})

    def test_completion_claim_is_rejected(self):
        with self.assertRaises(review.TraceError):
            review.validate_slice({"schema_version": 1, "coverage_complete": True}, Path.cwd(), {})

    def test_bullets_and_composite_locators_keep_parent_clauses(self):
        self.assertEqual(review.locator_ids("S1516.1", "4.1.2; 4.24.3(e); 4.25"),
                         ["S1516.1:4.1.2", "S1516.1:4.24.3", "S1516.1:4.25"])
        self.assertEqual(review.locator_ids("S1516.1", "B; Table 6"),
                         ["S1516.1:AnnexB", "S1516.1:Table6"])
        self.assertEqual(review.locator_ids("S1516.2", "7/Table 50"),
                         ["S1516.2:7", "S1516.2:Table50"])

    def test_empty_decomposition_is_not_a_reviewed_unit(self):
        units = [{"id": "S1516.1:4.2.3"}]
        with self.assertRaisesRegex(review.TraceError, "without source-reviewed"):
            review.validate_decompositions({"S1516.1:4.2.3"}, units, [])
        for source_id, review_status in (("S1516.2", "source-reviewed"), ("S1516.1", "candidate")):
            with self.assertRaises(review.TraceError):
                review.validate_decompositions({"S1516.1:4.2.3"}, units,
                    [{"source_id": source_id, "review_status": review_status, "clause": "4.2.3"}])

    def test_ranges_include_every_interior_and_flag_non_sibling_ranges(self):
        self.assertEqual(review.locator_ids("S1516.1", "5.4.1-5.4.5"),
                         ["S1516.1:5.4." + str(i) for i in range(1, 6)])
        self.assertEqual(review.parse_locators("S1516.1", "4.1.4-4.1.4.1")[1], ["4.1.4-4.1.4.1"])

    def test_variant_identity_is_retained_without_whole_method_credit(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / "pysdk/tests/test_contract.py"
            path.parent.mkdir(parents=True)
            path.write_text("def test_contract(): pass\n", encoding="utf-8")
            xml = root / "results.xml"
            xml.write_text('<testsuite><testcase classname="tests.test_contract" name="test_contract[one]"/></testsuite>', encoding="utf-8")
            index, _ = review.execution_index(python_path=xml)
            key = ("tests.test_contract", "test_contract")
            self.assertEqual(index.invocations[key][0]["identity"], "tests.test_contract::test_contract[one]")
            self.assertEqual(review.execution_status({"path": "pysdk/tests/test_contract.py", "name": "test_contract"}, index, root),
                             "passing-records-only-completeness-unassessed")


if __name__ == "__main__":
    unittest.main()
