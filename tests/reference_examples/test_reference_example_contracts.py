from __future__ import annotations

import asyncio
import contextlib
import importlib.util
import json
import os
import shutil
import socket
import subprocess
import sys
import xml.etree.ElementTree as ET
from collections.abc import Iterator
from pathlib import Path
from typing import Any

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
MANIFEST = HERE / "reference_examples.json"
CONTRACT = HERE / "contracts" / "chat-1516e.json"
EXAMPLE = REPO_ROOT / "examples" / "ieee1516e-chat-parity" / "chat_scenario.py"
RTID_BINARY = REPO_ROOT / "bin" / ("rtid.exe" if os.name == "nt" else "rtid")


def _load_json(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as stream:
        return json.load(stream)


def _load_scenario() -> Any:
    spec = importlib.util.spec_from_file_location("_ieee1516e_chat_parity", EXAMPLE)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot import scenario from {EXAMPLE}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


async def _wait_for_port(port: int, timeout: float = 10.0) -> None:
    loop = asyncio.get_event_loop()
    deadline = loop.time() + timeout
    while loop.time() < deadline:
        try:
            _, writer = await asyncio.wait_for(
                asyncio.open_connection("127.0.0.1", port), timeout=0.5
            )
            writer.close()
            with contextlib.suppress(BaseException):
                await writer.wait_closed()
            return
        except (OSError, TimeoutError):
            await asyncio.sleep(0.1)
    raise TimeoutError(f"rtid did not listen on port {port}")


def _ensure_rtid() -> Path:
    if RTID_BINARY.exists():
        return RTID_BINARY
    go = shutil.which("go")
    if go is None:
        pytest.skip("no prebuilt rtid and no Go toolchain on PATH")
    RTID_BINARY.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(  # noqa: S603
        [go, "build", "-o", str(RTID_BINARY), "./rti/cmd/rtid"],
        cwd=REPO_ROOT,
        check=True,
    )
    return RTID_BINARY


@contextlib.contextmanager
def _rtid_url() -> Iterator[str]:
    binary = _ensure_rtid()
    listen_port = _free_port()
    metrics_port = _free_port()
    admin_port = _free_port()
    while len({listen_port, metrics_port, admin_port}) != 3:
        metrics_port = _free_port()
        admin_port = _free_port()
    process = subprocess.Popen(  # noqa: S603
        [
            str(binary),
            "--listen",
            f":{listen_port}",
            "--metrics-listen",
            f":{metrics_port}",
            "--admin-listen",
            f":{admin_port}",
            "--log-level",
            "warn",
        ],
        cwd=REPO_ROOT,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )
    try:
        asyncio.run(_wait_for_port(listen_port))
        yield f"grpc://127.0.0.1:{listen_port}"
    finally:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)


def _validate_contract(
    trace: list[dict[str, Any]], contract: dict[str, Any]
) -> None:
    by_name: dict[str, list[dict[str, Any]]] = {}
    for item in trace:
        by_name.setdefault(item["event"], []).append(item)

    missing = [name for name in contract["required_events"] if name not in by_name]
    assert not missing, f"missing semantic events: {missing}; trace={trace!r}"

    for before, after in contract["happens_before"]:
        assert by_name[before][0]["sequence"] < by_name[after][0]["sequence"], (
            f"required order violated: {before} -> {after}; trace={trace!r}"
        )

    for event_name, expected in contract["payloads"].items():
        actual = by_name[event_name][0]
        for key, value in expected.items():
            assert actual.get(key) == value, (
                f"{event_name}.{key}: expected {value!r}, got {actual.get(key)!r}"
            )

    registered = by_name["PARTICIPANT_REGISTERED"][0]["object_handle"]
    discovered = by_name["PARTICIPANT_DISCOVERED"][0]["object_handle"]
    reflected = by_name["PARTICIPANT_NAME_REFLECTED"][0]["object_handle"]
    delete_on_resign = by_name["PUB_RESIGN_DELETE_REQUESTED"][0]["object_handle"]
    removed = by_name["PARTICIPANT_REMOVED"][0]["object_handle"]
    assert len({registered, discovered, reflected, delete_on_resign, removed}) == 1


def test_ieee1516_sample_inventory_is_fully_classified() -> None:
    manifest = _load_json(MANIFEST)
    examples = manifest["examples"]
    names = [item["name"] for item in examples]
    assert len(names) == len(set(names))
    assert {item["status"] for item in examples} <= {
        "contract-covered",
        "unsupported-standard",
    }

    covered = [item for item in examples if item["status"] == "contract-covered"]
    assert {item["name"] for item in covered} == {"chat-cpp", "chat-java"}
    assert {item["contract"] for item in covered} == {"chat-1516e"}

def test_cpp_and_java_examples_share_one_contract() -> None:
    contract = _load_json(CONTRACT)
    assert contract["id"] == "chat-1516e"
    assert set(contract["language_examples"]) == {"chat-cpp", "chat-java"}
    assert len(contract["required_events"]) == len(
        set(contract["required_events"])
    )


def test_project_owned_fom_matches_chat_contract() -> None:
    contract = _load_json(CONTRACT)
    source = EXAMPLE.read_text(encoding="utf-8")
    for token in contract["required_api_tokens"]:
        assert token in source, f"chat scenario no longer exercises {token}"

    fom = EXAMPLE.with_name("federation.fom.xml")
    root = ET.parse(fom).getroot()  # noqa: S314
    declarations = []
    for element in root.iter():
        local_name = element.tag.rsplit("}", 1)[-1]
        if local_name not in {"attribute", "parameter"}:
            continue
        children = {
            child.tag.rsplit("}", 1)[-1]: (child.text or "").strip()
            for child in element
        }
        declarations.append((children.get("name", ""), children.get("dataType", "")))
    assert ("Name", "HLAunicodeString") in declarations
    assert ("Message", "HLAunicodeString") in declarations
    assert ("Sender", "HLAunicodeString") in declarations


@pytest.mark.integration
def test_gorti_chat_matches_ieee1516_language_neutral_contract() -> None:
    contract = _load_json(CONTRACT)
    scenario = _load_scenario()
    with _rtid_url() as url:
        trace = scenario.run_chat_scenario(url)
    _validate_contract(trace, contract)
