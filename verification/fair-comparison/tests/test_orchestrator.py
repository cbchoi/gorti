from __future__ import annotations

import json
import shutil
import subprocess
import sys
from pathlib import Path

import pytest
from fair_comparison.contract import CONFIG_SCHEMA, load_json, validate_manifest


def test_powershell_orchestrator_records_balanced_exact_invocations(tmp_path: Path) -> None:
    powershell = shutil.which("pwsh") or shutil.which("powershell")
    if not powershell:
        pytest.skip("PowerShell is not available")
    root = Path(__file__).resolve().parents[1]
    repo = root.parents[1]
    fake = root / "tests" / "fake_launcher.py"
    required = [
        "--fom",
        "{fom}",
        "--seed",
        "{seed}",
        "--count",
        "{count}",
        "--server-event-log",
        "{server_event_log}",
        "--output",
        "{output}",
        "--run-id",
        "{run_id}",
        "--workload",
        "{workload_file}",
    ]
    config = {
        "schema": CONFIG_SCHEMA,
        "launchers": {
            implementation: {
                "executable": sys.executable,
                "arguments": [
                    str(fake),
                    "--implementation",
                    implementation,
                    *required,
                ],
                "result_file": "result.json",
                "working_directory": "{repo}",
                "environment": {},
            }
            for implementation in ("reference_rti", "go")
        },
    }
    config_path = tmp_path / "launchers.json"
    config_path.write_text(json.dumps(config), encoding="utf-8")
    output = tmp_path / "session"

    completed = subprocess.run(  # noqa: S603 - fixed local PowerShell executable and script
        [
            powershell,
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-File",
            str(root / "run-comparison.ps1"),
            "-ConfigPath",
            str(config_path),
            "-Count",
            "2",
            "-ServerEventLog",
            "off",
            "-WarmupPairs",
            "1",
            "-MeasuredPairs",
            "2",
            "-BootstrapResamples",
            "100",
            "-OutputDirectory",
            str(output),
            "-Python",
            sys.executable,
        ],
        cwd=repo,
        capture_output=True,
        text=True,
        timeout=120,
        check=False,
    )
    assert completed.returncode == 0, completed.stdout + completed.stderr

    manifest = validate_manifest(load_json(output / "manifest.json"))
    assert manifest["schedule"]["warmup_pairs"] == 1
    assert manifest["schedule"]["measured_pairs"] == 2
    measured = [
        pair["order"] for pair in manifest["schedule"]["pairs"] if pair["phase"] == "measured"
    ]
    assert sorted(measured) == ["AB", "BA"]
    assert len(manifest["runs"]) == 6
    canonical_fom = (
        repo / "verification" / "commercial-rti" / "fom" / "CommercialRtiVerifier.xml"
    ).resolve()
    fom_arguments = []
    for run in manifest["runs"]:
        argv = run["command"]["argv"]
        fom_arguments.append(Path(argv[argv.index("--fom") + 1]).resolve())
        assert argv[argv.index("--seed") + 1] == "1516"
        assert argv[argv.index("--count") + 1] == "2"
        assert argv[argv.index("--server-event-log") + 1] == "off"
    assert set(fom_arguments) == {canonical_fom}
    assert manifest["orchestrator_provenance"]["canonical_fom_copied"] is False
    assert (output / "analysis.json").is_file()
