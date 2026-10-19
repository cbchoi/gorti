"""The full-HLA test gate must fail closed on inventory or execution gaps."""

from __future__ import annotations

import importlib.util
from pathlib import Path

import pytest

_spec = importlib.util.spec_from_file_location(
    "verify_python_hla", Path(__file__).resolve().parents[2] / "scripts/verify_python_hla.py"
)
assert _spec is not None and _spec.loader is not None
gate = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(gate)


def reports(nodeid="test_a", **overrides):
    return [
        {"nodeid": nodeid, "when": phase, "outcome": "passed", **overrides}
        for phase in ("setup", "call", "teardown")
    ]


def test_complete_execution_passes():
    assert gate.evaluate(["test_a"], ["test_a"], reports())["passed"]


@pytest.mark.parametrize("outcome", ["failed", "skipped"])
@pytest.mark.parametrize("when", ["setup", "call", "teardown"])
def test_every_failed_or_skipped_phase_rejects(outcome, when):
    data = reports()
    next(p for p in data if p["when"] == when)["outcome"] = outcome
    assert not gate.evaluate(["test_a"], ["test_a"], data)["passed"]


@pytest.mark.parametrize("collected", [[], ["other"], ["test_a", "test_a"]])
def test_inventory_disagreement_rejects(collected):
    assert not gate.evaluate(["test_a"], collected, reports())["passed"]


@pytest.mark.parametrize(
    "data", [reports()[:2], reports(wasxfail="known issue"), reports(wasxfail=""), reports() * 2]
)
def test_incomplete_xfail_or_duplicate_execution_rejects(data):
    assert not gate.evaluate(["test_a"], ["test_a"], data)["passed"]


def test_unclassified_files_reject(tmp_path):
    tests = tmp_path / "pysdk/tests"
    tests.mkdir(parents=True)
    (tests / "test_new.py").write_text("", encoding="utf-8")
    with pytest.raises(ValueError, match="unclassified=.*test_new"):
        gate.inventory(tmp_path, {"files": {}, "categories": {}})


def test_source_identity_includes_cpp_inputs_not_build_products(tmp_path, monkeypatch):
    monkeypatch.setattr(gate, "ROOT", tmp_path)
    monkeypatch.setattr(gate, "CONFIG", tmp_path / "pysdk/tests/hla_campaign.json")
    monkeypatch.setattr(gate, "EXPECTED", tmp_path / "pysdk/tests/hla_expected_nodeids.txt")
    monkeypatch.setattr(gate, "__file__", str(tmp_path / "scripts/verify_python_hla.py"))
    sources = [
        "cppsdk/src/ambassador.cpp", "cppsdk/include/RTI/Ambassador.h",
        "cppsdk/include/detail.hpp", "cppsdk/_generated/wire.cc",
        "cppsdk/CMakeLists.txt", "cppsdk/cmake/Settings.cmake",
        "examples/cpp-ieee1516e-smoke/publisher.cpp",
    ]
    for name in [*sources, "cppsdk/build/ignored.cpp"]:
        path = tmp_path / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("before", encoding="utf-8")
    before = gate.source_hashes()
    assert set(before) == set(sources)
    (tmp_path / sources[0]).write_text("after", encoding="utf-8")
    after = gate.source_hashes()
    assert [name for name in before if before[name] != after[name]] == [sources[0]]


def test_publisher_identity_tracks_override_creation_and_rebuild(tmp_path, monkeypatch):
    publisher = tmp_path / "publisher"
    monkeypatch.setenv("GORTI_CPP_PUBLISHER", str(publisher))
    missing = gate.cpp_publisher_identity()
    assert missing == {"path": str(publisher.resolve()), "sha256": None}
    publisher.write_bytes(b"first build")
    first = gate.cpp_publisher_identity()
    publisher.write_bytes(b"second build")
    second = gate.cpp_publisher_identity()
    assert first["path"] == second["path"] == missing["path"]
    assert first["sha256"] is not None and second["sha256"] is not None
    assert first["sha256"] != second["sha256"]


def test_worker_avoids_anonymous_temporary_file_capture(tmp_path, monkeypatch):
    calls = []
    monkeypatch.setattr(gate, "inventory", lambda *args: ["pysdk/tests/test_example.py"])
    monkeypatch.setattr(pytest, "main", lambda args, **kwargs: calls.append(args) or 0)
    assert gate.worker(tmp_path / "pytest.json", collect_only=False) == 0
    assert "--capture=sys" in calls[0]
    assert "pysdk/tests/test_example.py" in calls[0]
    assert f"--junitxml={tmp_path / 'junit.xml'}" in calls[0]


@pytest.mark.parametrize(
    "configuration",
    [
        '{"files":{"test_a.py":"hla_sdk","test_a.py":"hla_sdk"}}',
        '{"files":{"test_a.py":"hla_sdk","test_a.py":"rl_application"}}',
        '{"categories":{"hla_sdk":{"selected":true,"selected":false}}}',
    ],
    ids=["duplicate-file", "conflicting-category", "duplicate-selection"],
)
def test_worker_rejects_duplicate_configuration_keys(tmp_path, monkeypatch, configuration):
    config = tmp_path / "campaign.json"
    config.write_text(configuration, encoding="utf-8")
    monkeypatch.setattr(gate, "CONFIG", config)
    monkeypatch.setattr(gate, "inventory", lambda *args: ["pysdk/tests/test_example.py"])
    calls = []
    monkeypatch.setattr(pytest, "main", lambda *args, **kwargs: calls.append(args) or 0)
    with pytest.raises(ValueError, match="duplicate configuration key"):
        gate.worker(tmp_path / "pytest.json", collect_only=False)
    assert calls == []
