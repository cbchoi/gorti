"""Keep supported SDKs in generation, CI and release validation."""

import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts"))
import check_release_metadata as metadata  # noqa: E402
from verify_python_hla import inventory, read_configuration  # noqa: E402


def workflow(name):
    data = yaml.safe_load((ROOT / ".github/workflows" / name).read_text(encoding="utf-8"))
    # PyYAML's YAML 1.1 resolver reads the Actions key `on` as boolean True.
    if True in data:
        data["on"] = data.pop(True)
    return data


class ConfigurationTests(unittest.TestCase):
    def test_generation_has_only_go_python_and_maven_java(self):
        config = yaml.safe_load((ROOT / "buf.gen.yaml").read_text(encoding="utf-8"))
        self.assertEqual(
            {plugin["out"] for plugin in config["plugins"]},
            {
                "rti/internal/adapters/protobuf",
                "pysdk/rti1516e/_generated",
            },
        )
        self.assertFalse(any("/cpp" in plugin["remote"] for plugin in config["plugins"]))
        expected_imports = {
            f"M{path.relative_to(ROOT / 'proto').as_posix()}="
            "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1;rtiv1"
            for path in (ROOT / "proto/rti/v1").glob("*.proto")
        }
        for plugin in config["plugins"]:
            if plugin["out"] == "rti/internal/adapters/protobuf":
                self.assertEqual(expected_imports, set(plugin["opt"]) - {"paths=source_relative"})
        pom = ET.parse(ROOT / "javasdk/wire/pom.xml").getroot()  # noqa: S314 - repository POM
        self.assertIn("${project.basedir}/../../proto", [node.text for node in pom.iter()])
        self.assertIn("compile-custom", [node.text for node in pom.iter()])

    def test_validation_stays_manual(self):
        for name in ("ci.yml", "conformance.yml"):
            trigger = workflow(name)["on"]
            self.assertTrue(trigger == "workflow_dispatch" or set(trigger) == {"workflow_dispatch"})

    def test_no_active_workflow_depends_on_cpp_toolchain(self):
        for path in (ROOT / ".github/workflows").glob("*.yml"):
            text = path.read_text(encoding="utf-8").lower()
            for legacy in ("cppsdk", "conan", "compiler.cppstd"):
                self.assertNotIn(legacy, text, str(path))

    def test_manual_ci_validates_each_supported_sdk(self):
        jobs = workflow("ci.yml")["jobs"]
        self.assertTrue({"go", "python", "java", "proto", "configuration"} <= set(jobs))
        for language in ("python", "java"):
            commands = "\n".join(step.get("run", "") for step in jobs[language]["steps"])
            self.assertIn(f"scripts/validate_sdks.py {language}", commands)
        go_steps = jobs["go"]["steps"]
        self.assertTrue(
            any(step.get("uses", "").startswith("actions/setup-python@") for step in go_steps)
        )
        self.assertTrue(
            any("pip install -e './pysdk[dev]'" in step.get("run", "") for step in go_steps)
        )

    def test_release_runs_java_and_python_before_publish(self):
        jobs = workflow("release.yml")["jobs"]
        self.assertEqual(jobs["goreleaser"]["needs"], "preflight")
        steps = jobs["preflight"]["steps"]
        self.assertTrue(
            any(step.get("uses", "").startswith("actions/setup-java@") for step in steps)
        )
        self.assertTrue(
            any("validate_sdks.py python java ivct" in step.get("run", "") for step in steps)
        )
        install = next(
            i for i, step in enumerate(steps)
            if "pip install -e './pysdk[dev]'" in step.get("run", "")
        )
        go_tests = next(
            i for i, step in enumerate(steps)
            if "go test" in step.get("run", "")
        )
        self.assertLess(install, go_tests)

    def test_gate_defaults_and_rejects_legacy_stages(self):
        text = (ROOT / "scripts/ci-gates.sh").read_text(encoding="utf-8")
        self.assertIn("set -euo pipefail", text)
        self.assertIn("STAGES=(stubs go python java static ivct)", text)
        self.assertNotIn("cppsdk", text)
        self.assertNotIn("check-spec-traceability.sh", text)

    def test_python_inventory_covers_current_files_without_cpp(self):
        selected = inventory(ROOT, read_configuration())
        self.assertIn("pysdk/tests/test_integer64_production_live.py", selected)
        self.assertNotIn("pysdk/tests/spec/m17/test_cpp_python_interop.py", selected)
        self.assertFalse(any(Path(path).name.startswith("test_rl_") for path in selected))


class MetadataTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        for name in (
            "pysdk/pyproject.toml",
            "CITATION.cff",
            "codemeta.json",
            "javasdk/pom.xml",
            "javasdk/client/pom.xml",
            "javasdk/ieee1516-api/pom.xml",
            "javasdk/wire/pom.xml",
        ):
            target = self.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes((ROOT / name).read_bytes())

    def test_versions_pass_without_a_cpp_tree(self):
        self.assertEqual([], metadata.check_versions(self.root))

    def test_tag_mismatch_fails(self):
        self.assertTrue(metadata.check_versions(self.root, "v99.99.99"))

    def test_java_child_version_mismatch_fails(self):
        path = self.root / "javasdk/client/pom.xml"
        tree = ET.parse(path)  # noqa: S314 - owned fixture
        tree.find("m:parent/m:version", metadata.MAVEN).text = "99.0.0"
        tree.write(path)
        self.assertTrue(any("client" in error for error in metadata.check_versions(self.root)))

    def test_java_artifact_version_can_differ_from_product_version(self):
        self.assertEqual([], metadata.check_versions(self.root))


if __name__ == "__main__":
    unittest.main()
