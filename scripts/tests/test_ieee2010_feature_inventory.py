"""Synthetic inventory mutation tests; no fixture is IEEE completion evidence."""

import copy
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).resolve().with_name("ieee2010_feature_inventory.py")
SPEC = importlib.util.spec_from_file_location("inventory_gate", SCRIPT)
gate = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gate)


def fixture():
    rows = []
    for name in sorted(gate.AUDITS | gate.PROFILES | {"surface:sample"}):
        status = "normativeunknown" if name.startswith("Q") or name == "java-wsdl" else "unverified"
        rows.append({"id": name, "kind": "audit" if name in gate.AUDITS else "profile" if name in gate.PROFILES else "surface",
                     "status": status, "applicability": "unresolved" if status == "normativeunknown" else "product-required",
                     "rationale": "Synthetic pending verification", "anchors": [{"source": "s0", "locator": name}],
                     "trace": "sample", "profiles": ["go-windows"], "facets": {k: status for k in gate.FACETS},
                     "blocking_dependencies": ["independent-review"], "required_tests": ["sample-case"], "evidence": [], "reviewer": None})
    ids = sorted(row["id"] for row in rows)
    return {"schema_version": 1, "claim": "inventory-only", "source_snapshot": "test-snapshot",
            "claims": {"engineering_complete": False, "conformance_established": False, "certification": False},
            "sources": [{"id": "s0", "path": "sample.txt", "root": "repository", "sha256": "a" * 64}],
            "traces": {"sample": {"implementation_paths": ["sample.txt"], "oracle": "Independent stateful case; pending", "mapping_status": "unverified"}},
            "expected_ids": ids, "surface_digest": gate.digest(ids), "rows": rows, "evidence": []}


class InventoryTests(unittest.TestCase):
    def setUp(self):
        self.data = fixture()

    def check(self, expected="FAIL"):
        result = gate.evaluate(self.data)
        self.assertEqual(expected, result["verdict"], result)
        self.assertFalse(result["conformance_established"])
        self.assertFalse(result["certification"])
        return result

    def test_triaged_unknown_is_not_completion(self):
        result = self.check("PASS")
        self.assertFalse(result["engineering_complete"])
        self.assertEqual(0, result["commands_started"])

    def test_reject_untriaged_status(self):
        self.data["rows"][0]["status"] = "todo"
        self.check()

    def test_reject_missing_audit_even_if_expected_list_is_rewritten(self):
        self.data["rows"] = [r for r in self.data["rows"] if r["id"] != "C01"]
        self.data["expected_ids"].remove("C01")
        self.data["surface_digest"] = gate.digest(self.data["expected_ids"])
        self.check()

    def test_reject_missing_profile(self):
        self.data["rows"] = [r for r in self.data["rows"] if r["id"] != "cpp-tsan"]
        self.check()

    def test_reject_disappearing_surface(self):
        self.data["rows"] = [r for r in self.data["rows"] if r["id"] != "surface:sample"]
        self.check()

    def test_reject_duplicate_id(self):
        self.data["rows"].append(copy.deepcopy(self.data["rows"][0]))
        self.check()

    def test_reject_untriaged_facet(self):
        self.data["rows"][0]["facets"]["lifecycle"] = "todo"
        self.check()

    def test_reject_missing_facet(self):
        del self.data["rows"][0]["facets"]["exceptions"]
        self.check()

    def test_reject_fixed_without_executed_evidence(self):
        self.data["rows"][0]["status"] = "fixed-tested"
        self.check()

    def test_reject_stale_or_skipped_completion_evidence(self):
        for patch in [{"snapshot": "old"}, {"result": "skip"}, {"executed_cases": []}, {"kind": "historical-report"}]:
            with self.subTest(patch=patch):
                self.setUp()
                row = self.data["rows"][0]
                row.update(status="fixed-tested", reviewer="independent-reviewer", blocking_dependencies=[], evidence=["e0"])
                row["facets"] = {k: "fixed-tested" for k in gate.FACETS}
                self.data["evidence"] = [{"id": "e0", "kind": "executed-behavior", "snapshot": "test-snapshot", "result": "pass", "executed_cases": ["sample-case"], "profiles": ["go-windows"], "path": "not-present.json", "sha256": "b" * 64, **patch}]
                self.check()

    def test_reject_unreviewed_na(self):
        self.data["rows"][0]["status"] = "justifiedNA"
        self.check()

    def test_reject_reproduced_gap_without_reproducer(self):
        self.data["rows"][0]["status"] = "reproducedgap"
        self.check()

    def test_reject_normative_closure_without_text(self):
        row = next(r for r in self.data["rows"] if r["id"] == "Q01")
        row["status"] = "fixed-tested"
        self.check()

    def test_reject_claim_override(self):
        for claim in ["engineering_complete", "conformance_established", "certification"]:
            with self.subTest(claim=claim):
                self.setUp()
                self.data["claims"][claim] = True
                self.check()

    def test_completion_mode_rejects_unknowns(self):
        self.assertEqual("FAIL", gate.evaluate(self.data, require_completion=True)["verdict"])

    def test_reject_broken_trace_and_source(self):
        for field, value in [("trace", "missing"), ("anchors", [{"source": "missing", "locator": "x"}])]:
            with self.subTest(field=field):
                self.setUp()
                self.data["rows"][0][field] = value
                self.check()

    def test_reject_empty_unknown_reason(self):
        self.data["rows"][0]["rationale"] = ""
        self.check()

    def test_source_digest_drift_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "sample.txt"
            path.write_text("pinned source", encoding="utf-8")
            self.data["sources"][0]["sha256"] = gate.file_hash(path)
            self.assertEqual("PASS", gate.evaluate(self.data, root=root)["verdict"])
            path.write_text("changed source", encoding="utf-8")
            self.assertEqual("FAIL", gate.evaluate(self.data, root=root)["verdict"])

    def test_source_root_escape_fails(self):
        self.data["sources"][0]["path"] = "../outside.txt"
        with tempfile.TemporaryDirectory() as directory:
            self.assertEqual("FAIL", gate.evaluate(self.data, root=Path(directory))["verdict"])

    def test_duplicate_json_key_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "duplicate.json"
            path.write_text('{"claim": "inventory-only", "claim": "full-2010"}', encoding="utf-8")
            with self.assertRaises(ValueError):
                gate.load_json(path)

    def test_malformed_structures_fail_closed_for_imported_callers(self):
        for key, value in [("rows", {}), ("sources", None), ("traces", []), ("schema_version", True)]:
            with self.subTest(key=key):
                self.setUp()
                self.data[key] = value
                self.check()
        self.setUp()
        self.data["rows"][0]["status"] = []
        self.check()

    def test_verified_bounded_row_and_artifact_tampering(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "observed.json"
            artifact = {"snapshot": "test-snapshot", "cases": [{"id": "sample-case", "profile": "go-windows", "executed": True, "result": "pass"}]}
            path.write_text(json.dumps(artifact), encoding="utf-8")
            row = self.data["rows"][0]
            row.update(status="fixed-tested", reviewer="test-independent-reviewer", blocking_dependencies=[], evidence=["e0"])
            row["facets"] = {k: "fixed-tested" for k in gate.FACETS}
            self.data["evidence"] = [{"id": "e0", "kind": "executed-behavior", "snapshot": "test-snapshot", "result": "pass", "executed_cases": ["sample-case"], "profiles": ["go-windows"], "path": str(path), "sha256": gate.file_hash(path)}]
            self.check("PASS")
            artifact["cases"][0]["result"] = "skip"
            path.write_text(json.dumps(artifact), encoding="utf-8")
            self.check()
            self.data["evidence"][0]["sha256"] = gate.file_hash(path)
            self.check()

    def test_na_cannot_reuse_historical_test_report(self):
        row = self.data["rows"][0]
        row.update(status="justifiedNA", applicability="not-applicable", reviewer="reviewer", na_justification="outside profile", evidence=["e0"])
        self.data["evidence"] = [{"id": "e0", "kind": "historical-report"}]
        self.check()

    def test_gap_cannot_reuse_historical_pass_report(self):
        row = self.data["rows"][0]
        row.update(status="reproducedgap", reproducer="actual failing case", evidence=["e0"])
        self.data["evidence"] = [{"id": "e0", "kind": "historical-report"}]
        self.check()


class SourceExtractionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location("inventory_sources", Path(__file__).with_name("ieee2010_inventory_sources.py"))
        cls.sources = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.sources)

    def test_cpp_parameters_include_nested_default_constructors(self):
        declarations = '''// virtual void fake();
        virtual Handle join(std::vector<std::wstring> const & modules=std::vector<std::wstring>())
          throw(NotConnected, RTIinternalError) = 0;
        virtual void call(std::wstring const & value=L")") const throw(Error) = 0;
        virtual ~Example();'''
        methods = list(self.sources.cpp_methods(declarations))
        self.assertEqual(2, len(methods))
        self.assertTrue(methods[0]["parameters"].endswith("std::vector<std::wstring>()"))
        self.assertEqual(["NotConnected", "RTIinternalError"], methods[0]["exceptions"])
        self.assertEqual("const", methods[1]["qualifiers"])

    def test_cpp_overload_identity_differs(self):
        methods = list(self.sources.cpp_methods("virtual void call(int x)=0; virtual void call(int x, Tag t)=0;"))
        self.assertNotEqual(gate.digest(methods[0]), gate.digest(methods[1]))

    def test_cpp_unrecognized_virtual_is_not_silently_dropped(self):
        with self.assertRaises(ValueError):
            list(self.sources.cpp_methods("virtual nonsense;"))

    def test_repository_inventory_keeps_all_triaged_rows(self):
        path = SCRIPT.parents[2] / "engineering/verification/ieee1516-2010-feature-inventory.json"
        if not path.exists():
            self.fail("generated feature inventory is required")
        data = gate.load_json(path)
        self.assertEqual("PASS", gate.evaluate(data)["verdict"])
        official = [r for r in data["rows"] if r["id"].startswith("official-api:")]
        self.assertEqual(151, sum(r["kind"] == "cpp-service-overload" for r in official))
        self.assertEqual(60, sum(r["kind"] == "cpp-callback" for r in official))
        self.assertEqual(8, sum(r["kind"] == "implementation-gap" for r in data["rows"]))
        self.assertEqual(45, sum(r["kind"] == "mim-attribute" for r in data["rows"]))
        self.assertEqual(101, sum(r["kind"] == "mim-parameter" for r in data["rows"]))
        self.assertEqual(2, sum(r["kind"] == "mim-dimension" for r in data["rows"]))
        self.assertEqual(2, sum(r["kind"] == "mim-transportation" for r in data["rows"]))
        self.assertEqual(3, sum(r["kind"] == "python-export-manifest" for r in data["rows"]))


if __name__ == "__main__":
    unittest.main()
