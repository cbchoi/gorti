"""Product report-claim guard; not an external certification oracle."""

import contextlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
SPEC = importlib.util.spec_from_file_location(
    "certification_claim_review", Path(__file__).resolve().parents[1] / "ieee2010_requirements_review.py"
)
review = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(review)


class TestCertificationClaimGuard(unittest.TestCase):
    def test_green_mapping_and_records_cannot_claim_hla_conforming(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            sources, slices = root / "sources", root / "slices"
            sources.mkdir()
            slices.mkdir()
            pdf = sources / "synthetic.pdf"
            pdf.write_bytes(b"Synthetic source identity only, not standard content")
            (sources / "sources.json").write_text(json.dumps([
                {"id": "1516.1", "filename": pdf.name, "sha256": review.digest(pdf), "pages": 17}
            ]), encoding="utf-8")
            (sources / "1516.1-pages.jsonl").write_text("".join(json.dumps(
                {"pdf_page": number, "active_candidate": "13. Synthetic conformance claim condition\nSynthetic body" if number == 17 else ""}
            ) + "\n" for number in range(1, 18)), encoding="utf-8")
            test_path = root / "tests" / "claim_test.go"
            test_path.parent.mkdir()
            test_path.write_text("package tests\nimport \"testing\"\nfunc TestClaim(t *testing.T) {}\n", encoding="utf-8")
            (root / "go.mod").write_text("module example.org/claim\n", encoding="utf-8")
            row = {
                "id": "SYNTHETIC-CLAIM", "source_id": "S1516.1", "clause": "13", "pdf_pages": [17],
                "subject": "rti", "applicability": "applicable", "review_status": "source-reviewed",
                "statement": "Conditional product-label obligation.",
                "conditions": "Recognized certification remains unestablished.", "implementation": [],
                "tests": [{"path": "tests/claim_test.go", "name": "TestClaim", "oracle": "Synthetic passing record."}],
                "open_items": ["No recognized external certificate or authority-certified public results."],
            }
            (slices / "claim.json").write_text(json.dumps({
                "schema_version": 1, "source_id": "S1516.1", "coverage_complete": False,
                "reviewed_units": ["13"], "requirements": [row],
            }), encoding="utf-8")
            go_results = root / "passing.jsonl"
            go_results.write_text(json.dumps({
                "Package": "example.org/claim/tests", "Test": "TestClaim", "Action": "pass"
            }) + "\n", encoding="utf-8")
            args = SimpleNamespace(
                sources=sources, pdf_dir=sources, slices=slices, go_results=go_results,
                python_results=None, java_results=None, supplemental_junit=[], supplemental_go_results=[],
                output=root / "report.json", require_applicable_oracles=True,
            )
            stdout = io.StringIO()
            with patch.object(review, "ROOT", root), contextlib.redirect_stdout(stdout):
                review.build(args)
            report = json.loads(args.output.read_text(encoding="utf-8"))
            summary = json.loads(stdout.getvalue())
            self.assertEqual(report["applicable_oracle_linkage"], {
                "scope": "reviewed-applicable-records-only", "applicable": 1,
                "linked": 1, "unlinked_ids": [], "complete": True,
            })
            self.assertTrue(report["review_units"][0]["reviewed_decomposition_declared"])
            evidence = report["requirements"][0]["test_evidence"][0]
            self.assertEqual(evidence["execution_status"], "passing-records-only-completeness-unassessed")
            self.assertEqual([record["status"] for record in evidence["invocations"]], ["pass"])
            self.assertIs(report["coverage_complete"], False)
            self.assertIs(report["full_conformance"], False)
            self.assertIs(summary["full_conformance"], False)
            self.assertEqual(report["requirements"][0]["semantic_verdict"], "open")
            self.assertEqual(report["requirements"][0]["open_items"], row["open_items"])
            self.assertNotIn("hla conforming", args.output.read_text(encoding="utf-8").lower())
            self.assertNotIn("hla conforming", stdout.getvalue().lower())


if __name__ == "__main__":
    unittest.main()
