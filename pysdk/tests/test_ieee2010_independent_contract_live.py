"""Four internal black-box contracts, not a full IEEE conformance assessment.

Source: S1516.1 (SHA256 789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4),
one-based PDF pages 49, 52-54 and 132. Expectations were derived from rendered
redlines and the source review, not production implementation oracles.
GORTI_RTID must select a hash-pinned executable; missing prerequisites fail, not skip.
New candidates require an explicit GORTI_RTID_SHA256; the historical pin is the default.
"""

from __future__ import annotations

import hashlib
import json
import os
import socket
import subprocess
import tempfile
import threading
import time
import xml.etree.ElementTree as ET
from contextlib import ExitStack
from pathlib import Path

import pytest

from rti1516e import Rti1516eAmbassador
from rti1516e.errors import (
    DeletePrivilegeNotHeld,
    FederatesCurrentlyJoined,
    FederationExecutionAlreadyExists,
    FederationExecutionDoesNotExist,
    ObjectInstanceNotKnown,
)

ROOT = Path(__file__).resolve().parents[2]
PINNED_SHA256 = "647a999b02cfc81fcd9521c0a96c9e06a440c5a0719735c95ab4d889a9084fbb"
BASE_CLASS = "HLAobjectRoot.IndependentBeacon"
EXTRA_CLASS = "HLAobjectRoot.IndependentSensor"


def _expected_binary_sha256() -> str:
    expected = os.environ.get("GORTI_RTID_SHA256", PINNED_SHA256)
    assert len(expected) == 64 and all(char in "0123456789abcdef" for char in expected), (
        "invalid binary SHA-256 pin"
    )
    return expected


def _write_module(path: Path, class_name: str) -> None:
    # A new complete class, with no dependency on any checked-in FOM fixture.
    root = ET.Element("objectModel", xmlns="http://standards.ieee.org/IEEE1516-2010")
    identity = ET.SubElement(root, "modelIdentification")
    for name, value in (
        ("name", path.stem),
        ("type", "FOM"),
        ("version", "1.0"),
        ("description", "Independent bounded service contract model."),
    ):
        ET.SubElement(identity, name).text = value
    objects = ET.SubElement(root, "objects")
    parent = ET.SubElement(objects, "objectClass")
    ET.SubElement(parent, "name").text = "HLAobjectRoot"
    child = ET.SubElement(parent, "objectClass")
    ET.SubElement(child, "name").text = class_name
    ET.SubElement(child, "sharing").text = "PublishSubscribe"
    attribute = ET.SubElement(child, "attribute")
    for name, value in (
        ("name", "Sample"),
        ("dataType", "HLAopaqueData"),
        ("updateType", "Conditional"),
        ("updateCondition", "NA"),
        ("ownership", "DivestAcquire"),
        ("sharing", "PublishSubscribe"),
        ("transportation", "HLAreliable"),
        ("order", "Receive"),
        ("semantics", "Opaque bytes chosen by the test federate."),
    ):
        ET.SubElement(attribute, name).text = value
    ET.ElementTree(root).write(path, encoding="utf-8", xml_declaration=True)


@pytest.fixture(scope="module")
def independent_artifacts():
    binary = Path(os.environ["GORTI_RTID"]).resolve()
    assert hashlib.sha256(binary.read_bytes()).hexdigest() == _expected_binary_sha256()
    parent = Path(os.environ.get("GORTI_INDEPENDENT_EVIDENCE", tempfile.gettempdir())).resolve()
    assert parent != ROOT and ROOT not in parent.parents
    parent.mkdir(parents=True, exist_ok=True)
    directory = Path(tempfile.mkdtemp(prefix="independent-contract-", dir=parent))
    base, extra = directory / "base.xml", directory / "contribution.xml"
    _write_module(base, "IndependentBeacon")
    _write_module(extra, "IndependentSensor")
    (directory / "inputs.json").write_text(
        json.dumps(
            {
                "binary": {"path": str(binary), "sha256": _expected_binary_sha256()},
                "fom": [
                    {"path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
                    for path in (base, extra)
                ],
            },
            indent=2,
        ),
        encoding="utf-8",
    )
    return directory, binary, base, extra


@pytest.fixture(name="independent_server")
def independent_server(independent_artifacts, request):
    directory, binary, base, extra = independent_artifacts
    run = directory / request.node.name
    run.mkdir()
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    env = os.environ.copy()
    env["GORTI_RESEARCH_CONFIG"] = ""
    argv = [
        str(binary),
        f"--listen=127.0.0.1:{port}",
        "--admin-listen=127.0.0.1:0",
        "--metrics-listen=127.0.0.1:0",
        f"--save-dir={run / 'saves'}",
        "--audit-replay-plugin=none",
        "--log-dir=",
        "--log-level=info",
    ]
    with (run / "server.log").open("wb") as output:
        child = subprocess.Popen(  # noqa: S603 - hash-verified local server, no shell
            argv,
            cwd=run,
            env=env,
            stdout=output,
            stderr=subprocess.STDOUT,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        try:
            deadline = time.monotonic() + 15
            while True:
                assert child.poll() is None, f"server exited: {run / 'server.log'}"
                try:
                    with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                        break
                except OSError:
                    assert time.monotonic() < deadline, "server did not accept TCP connections"
                    time.sleep(0.05)
            yield f"grpc://127.0.0.1:{port}", base, extra, run
        finally:
            if child.poll() is None:
                child.terminate()
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)
            (run / "process.json").write_text(
                json.dumps(
                    {
                        "argv": argv,
                        "pid": child.pid,
                        "returncode": child.returncode,
                        "closed": child.poll() is not None,
                        "binary_sha256_after": hashlib.sha256(binary.read_bytes()).hexdigest(),
                    },
                    indent=2,
                ),
                encoding="utf-8",
            )
            assert child.poll() is not None
            assert hashlib.sha256(binary.read_bytes()).hexdigest() == _expected_binary_sha256()


class _Observer(Rti1516eAmbassador):
    def __init__(self, log: Path):
        super().__init__()
        self.log = log
        self.condition = threading.Condition()
        self.callbacks = []

    def _record(self, item):
        with self.condition:
            self.callbacks.append(item)
            with self.log.open("a", encoding="utf-8") as output:
                output.write(json.dumps(item) + "\n")
            self.condition.notify_all()

    def discoverObjectInstance(  # noqa: N802 - HLA callback name
        self, object_handle, class_name, instance_name, object_class=None
    ):
        self._record(["discover", int(object_handle), instance_name, int(object_class)])

    def reflectAttributeValues(  # noqa: N802 - HLA callback name
        self, object_handle, values, timestamp, attribute_values=None, tag=b"", metadata=None
    ):
        self._record(
            [
                "reflect",
                int(object_handle),
                {str(int(k)): bytes(v).hex() for k, v in attribute_values.items()},
                timestamp,
            ]
        )

    def removeObjectInstance(self, object_handle, tag, timestamp):  # noqa: N802 - HLA callback
        self._record(["remove", int(object_handle), bytes(tag).hex(), timestamp])

    def wait(self, kind, handle):
        deadline = time.monotonic() + 10
        with self.condition:
            while True:
                found = [item for item in self.callbacks if item[:2] == [kind, int(handle)]]
                if found:
                    assert len(found) == 1, found
                    return found[0]
                remaining = deadline - time.monotonic()
                assert remaining > 0, f"missing {kind}: {self.callbacks!r}"
                self.condition.wait(remaining)


def _connect(stack: ExitStack, url: str, run: Path, name: str):
    ambassador = _Observer(run / f"{name}-callbacks.jsonl")
    ambassador.connect(ambassador, url)
    stack.callback(ambassador.disconnect)
    return ambassador


def _exact_exception(kind, operation, *args):
    with pytest.raises(kind) as caught:
        operation(*args)
    assert type(caught.value) is kind
    print(f"expected exception: {kind.__name__}: {caught.value}")


def test_duplicate_create_preserves_original_model(independent_server):
    """IND-FM-DUPLICATE: 4.5.3(b), 4.5.4(a), 4.5.5(g), PDF 52-53."""
    url, base, extra, run = independent_server
    with ExitStack() as stack:
        creator = _connect(stack, url, run, "creator")
        competitor = _connect(stack, url, run, "competitor")
        creator.createFederationExecution("ind-duplicate", [str(base)])
        _exact_exception(
            FederationExecutionAlreadyExists,
            competitor.createFederationExecution,
            "ind-duplicate",
            [str(extra)],
        )
        competitor.joinFederationExecution("fresh-reader", "ind-duplicate")
        beacon = competitor.getObjectClassHandle(BASE_CLASS)
        assert competitor.getObjectClassName(beacon) == BASE_CLASS
        sample = competitor.getAttributeHandle(beacon, "Sample")
        competitor.publishObjectClassAttributes(beacon, [sample])
        instance = competitor.registerObjectInstance(beacon)
        assert competitor.getKnownObjectClassHandle(instance) == beacon
        competitor.deleteObjectInstance(instance, b"original-model-control")
        competitor.resignFederationExecution("NO_ACTION")
        creator.destroyFederationExecution("ind-duplicate")
        creator.createFederationExecution("ind-duplicate", [str(extra)])
        replacement = _connect(stack, url, run, "replacement")
        replacement.joinFederationExecution("replacement-reader", "ind-duplicate")
        sensor = replacement.getObjectClassHandle(EXTRA_CLASS)
        assert replacement.getObjectClassName(sensor) == EXTRA_CLASS
        sample = replacement.getAttributeHandle(sensor, "Sample")
        replacement.publishObjectClassAttributes(sensor, [sample])
        instance = replacement.registerObjectInstance(sensor)
        assert replacement.getKnownObjectClassHandle(instance) == sensor
        replacement.deleteObjectInstance(instance, b"replacement-model-control")
        replacement.resignFederationExecution("NO_ACTION")
        creator.destroyFederationExecution("ind-duplicate")


def test_destroy_joined_preserves_execution_until_resign(independent_server):
    """IND-FM-DESTROY-JOINED: 4.6.3(c), 4.6.4(a), 4.6.5(a-b), PDF 53-54."""
    url, base, _, run = independent_server
    with ExitStack() as stack:
        admin = _connect(stack, url, run, "admin")
        member = _connect(stack, url, run, "member")
        entrant = _connect(stack, url, run, "entrant")
        admin.createFederationExecution("ind-destroy", [str(base)])
        member.joinFederationExecution("member", "ind-destroy")
        original = member.getObjectClassHandle(BASE_CLASS)
        _exact_exception(FederatesCurrentlyJoined, admin.destroyFederationExecution, "ind-destroy")
        assert member.getObjectClassHandle(BASE_CLASS) == original
        entrant.joinFederationExecution("entrant", "ind-destroy")
        assert entrant.getObjectClassHandle(BASE_CLASS) == original
        member.resignFederationExecution("NO_ACTION")
        _exact_exception(FederatesCurrentlyJoined, admin.destroyFederationExecution, "ind-destroy")
        entrant.resignFederationExecution("NO_ACTION")
        admin.destroyFederationExecution("ind-destroy")
        _exact_exception(
            FederationExecutionDoesNotExist, admin.destroyFederationExecution, "ind-destroy"
        )
        admin.createFederationExecution("ind-destroy", [str(base)])
        admin.destroyFederationExecution("ind-destroy")


def test_delete_unknown_precedes_privilege_and_preserves_object(independent_server):
    """IND-OM-DELETE-UNKNOWN: 6.14.3(e-f), 6.14.4(b), 6.14.5(b-c), PDF 132.

    The historical method name describes only this tested knowledge state,
    not a universal ordering among multiple IEEE service exceptions.
    """
    url, base, _, run = independent_server
    with ExitStack() as stack:
        owner = _connect(stack, url, run, "owner")
        stranger = _connect(stack, url, run, "stranger")
        witness = _connect(stack, url, run, "witness")
        owner.createFederationExecution("ind-delete", [str(base)])
        for name, ambassador in (("owner", owner), ("stranger", stranger), ("witness", witness)):
            ambassador.joinFederationExecution(name, "ind-delete")
        beacon = owner.getObjectClassHandle(BASE_CLASS)
        sample = owner.getAttributeHandle(beacon, "Sample")
        owner.publishObjectClassAttributes(beacon, [sample])
        witness.subscribeObjectClassAttributes(beacon, [sample])
        instance = owner.registerObjectInstance(beacon)
        name = owner.getObjectInstanceName(instance)
        assert witness.wait("discover", instance) == ["discover", int(instance), name, int(beacon)]
        assert stranger.callbacks == []
        _exact_exception(
            ObjectInstanceNotKnown, stranger.deleteObjectInstance, instance, b"unknown"
        )
        assert owner.getObjectInstanceName(instance) == name
        assert witness.getObjectInstanceHandle(name) == instance
        _exact_exception(
            DeletePrivilegeNotHeld, witness.deleteObjectInstance, instance, b"not-owner"
        )
        payload = b"\x00\xffindependent\x01"
        owner.updateAttributeValues(instance, {sample: payload})
        assert witness.wait("reflect", instance) == [
            "reflect",
            int(instance),
            {str(int(sample)): payload.hex()},
            None,
        ]
        assert not any(item[0] == "remove" for item in witness.callbacks)
        tag = b"\x00owner-delete\xff"
        owner.deleteObjectInstance(instance, tag)
        assert witness.wait("remove", instance) == ["remove", int(instance), tag.hex(), None]
        _exact_exception(ObjectInstanceNotKnown, owner.deleteObjectInstance, instance, b"again")
        assert stranger.callbacks == []
        assert [item[0] for item in witness.callbacks] == ["discover", "reflect", "remove"]
        for ambassador in (witness, stranger, owner):
            ambassador.resignFederationExecution("NO_ACTION")
        owner.destroyFederationExecution("ind-delete")


def test_contributed_fom_survives_last_resigner(independent_server):
    """IND-FOM-CONTRIBUTOR-PERSISTENCE: 4.1.4.1 lifetime rule, PDF 49."""
    url, base, extra, run = independent_server
    with ExitStack() as stack:
        admin = _connect(stack, url, run, "admin")
        contributor = _connect(stack, url, run, "contributor")
        reader = _connect(stack, url, run, "reader")
        admin.createFederationExecution("ind-fom-life", [str(base)])
        reader.joinFederationExecution("before", "ind-fom-life")
        base_handle = reader.getObjectClassHandle(BASE_CLASS)
        contributor.joinFederationExecution("contributor", "ind-fom-life", [str(extra)])
        contributed = contributor.getObjectClassHandle(EXTRA_CLASS)
        sample = contributor.getAttributeHandle(contributed, "Sample")
        assert reader.getObjectClassHandle(EXTRA_CLASS) == contributed
        _exact_exception(FederatesCurrentlyJoined, admin.destroyFederationExecution, "ind-fom-life")
        reader.resignFederationExecution("NO_ACTION")
        contributor.resignFederationExecution("NO_ACTION")
        # No surviving connection's local FOM cache supplies this fresh member.
        fresh = _connect(stack, url, run, "fresh")
        fresh.joinFederationExecution("after-empty", "ind-fom-life")
        assert fresh.getObjectClassHandle(BASE_CLASS) == base_handle
        assert fresh.getObjectClassHandle(EXTRA_CLASS) == contributed
        assert fresh.getAttributeHandle(contributed, "Sample") == sample
        assert fresh.getObjectClassName(contributed) == EXTRA_CLASS
        assert fresh.getAttributeName(contributed, sample) == "Sample"
        fresh.publishObjectClassAttributes(contributed, [sample])
        instance = fresh.registerObjectInstance(contributed)
        assert fresh.getKnownObjectClassHandle(instance) == contributed
        fresh.deleteObjectInstance(instance, b"persistence-control")
        fresh.resignFederationExecution("NO_ACTION")
        admin.destroyFederationExecution("ind-fom-life")
        _exact_exception(
            FederationExecutionDoesNotExist, admin.destroyFederationExecution, "ind-fom-life"
        )
