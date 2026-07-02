"""M39 HA-2 — Layer-2 API completeness against a live rtid.

Contract asserted here (IEEE 1516.1-2010 § references):

  - §4.5  ``createFederationExecution`` issues the create RPC eagerly
          and raises typed ``FederationExecutionAlreadyExists`` on a
          duplicate name (previously: stash-only, ALREADY_EXISTS
          swallowed by the rolled create-on-join). The rolled path
          stays idempotent.
  - §4.6  ``destroyFederationExecution`` exists at Layers 1+2; typed
          ``FederatesCurrentlyJoined`` while members remain, typed
          ``FederationExecutionDoesNotExist`` for an unknown name.
  - §4.14 ``synchronizationPointAchieved(label, successfully=False)``
          lands the federate in the §4.15 failed_to_sync set.
  - §7.3+§7.6 ``negotiatedAttributeOwnershipDivestiture(two_phase=True)``
          parks on requestDivestitureConfirmation until
          ``confirmDivestiture`` completes the transfer.
  - §7.9+§7.10 ``attributeOwnershipAcquisitionIfAvailable`` transfers
          nothing that is owned and reports the losing subset via
          attributeOwnershipUnavailable.
  - §7.11 a plain acquisition against owned attributes fires
          requestAttributeOwnershipRelease at the owner.
  - Unified error translation: object-path RPCs raise the same typed
          exceptions the time path does (register on an unpublished
          class -> ObjectClassNotPublished).
"""

from __future__ import annotations

import asyncio
import contextlib
import shutil
import socket
import subprocess
import sys
import threading
import time
import uuid
from collections.abc import Iterator
from pathlib import Path
from typing import Any

import pytest

REPO_ROOT = Path(__file__).resolve().parents[4]
_PYSDK = REPO_ROOT / "pysdk"
if str(_PYSDK) not in sys.path:
    sys.path.insert(0, str(_PYSDK))

# ruff: noqa: E402
from rti1516e import Rti1516eAmbassador
from rti1516e.errors import (
    FederatesCurrentlyJoined,
    FederationExecutionAlreadyExists,
    FederationExecutionDoesNotExist,
    ObjectClassNotPublished,
    RtiError,
)
from rti1516e.handles import AttributeHandle

BIN_DIR = REPO_ROOT / "bin"
RTID_BINARY = BIN_DIR / "rtid"
FOM = str(REPO_ROOT / "examples" / "pitch-typed-smoke" / "federation.fom.xml")
VEHICLE_ATTRS: list[int | str | AttributeHandle] = ["Position", "Velocity"]
WAIT = 5.0


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def _build_rtid() -> Path:
    BIN_DIR.mkdir(parents=True, exist_ok=True)
    argv = ["go", "build", "-o", str(RTID_BINARY), "./rti/cmd/rtid"]  # noqa: S607
    subprocess.run(argv, cwd=REPO_ROOT, check=True)  # noqa: S603
    return RTID_BINARY


async def _wait_for_grpc(port: int, *, timeout: float = 10.0) -> None:  # noqa: ASYNC109
    loop = asyncio.get_event_loop()
    deadline = loop.time() + timeout
    while loop.time() < deadline:
        try:
            _, w = await asyncio.wait_for(
                asyncio.open_connection("127.0.0.1", port), timeout=0.5
            )
            w.close()
            with contextlib.suppress(BaseException):
                await w.wait_closed()
            return
        except (OSError, TimeoutError):
            await asyncio.sleep(0.1)
    raise TimeoutError(f"rtid did not accept connections on :{port} within {timeout}s")


@pytest.fixture(scope="module")
def rtid_url() -> Iterator[str]:
    if shutil.which("go") is None and not RTID_BINARY.exists():
        pytest.skip("no prebuilt rtid and no go toolchain to build one")
    binary = RTID_BINARY if RTID_BINARY.exists() else _build_rtid()
    listen_port = _free_port()
    metrics_port = _free_port()
    if metrics_port == listen_port:
        metrics_port = _free_port()
    proc = subprocess.Popen(  # noqa: S603
        [
            str(binary),
            "--listen", f":{listen_port}",
            "--metrics-listen", f":{metrics_port}",
            "--log-level", "warn",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )
    try:
        asyncio.run(_wait_for_grpc(listen_port))
        yield f"grpc://127.0.0.1:{listen_port}"
    finally:
        proc.terminate()
        with contextlib.suppress(Exception):
            proc.wait(timeout=5.0)


@pytest.fixture
def federation_name() -> str:
    return f"m39-{uuid.uuid4().hex[:12]}"  # ≤32 bytes (eventlog limit)


class _Recorder(Rti1516eAmbassador):
    """Thread-safe callback recorder with deadline waits (ivct pattern)."""

    def __init__(self) -> None:
        super().__init__()
        self._cv = threading.Condition()
        self.calls: list[tuple[str, dict[str, Any]]] = []

    def _record(self, name: str, **payload: Any) -> None:
        with self._cv:
            self.calls.append((name, payload))
            self._cv.notify_all()

    def wait_for(
        self, name: str, *, timeout: float = WAIT, **match: Any
    ) -> dict[str, Any]:
        def _find() -> dict[str, Any] | None:
            for cb_name, payload in self.calls:
                if cb_name == name and all(
                    payload.get(k) == v for k, v in match.items()
                ):
                    return payload
            return None

        deadline = time.monotonic() + timeout
        with self._cv:
            while True:
                if (found := _find()) is not None:
                    return found
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError(
                        f"no {name!r} matching {match!r}; recorded: {self.calls!r}"
                    )
                self._cv.wait(remaining)

    def assert_quiet(self, name: str, *, window: float = 0.5) -> None:
        time.sleep(window)
        with self._cv:
            got = [c for c in self.calls if c[0] == name]
        assert not got, f"expected no {name!r}, got {got!r}"

    # -- recorded overrides -------------------------------------------------

    def discoverObjectInstance(  # noqa: N802
        self,
        object_handle: int,
        class_name: str,
        instance_name: str,
        object_class: int | None = None,
    ) -> None:
        self._record("discover", object_handle=object_handle)

    def announceSynchronizationPoint(self, label: str, tag: bytes) -> None:  # noqa: N802
        self._record("announce", label=label)

    def federationSynchronized(  # noqa: N802
        self, label: str, failed_to_sync: tuple[int, ...] = ()
    ) -> None:
        self._record("synchronized", label=label, failed_to_sync=failed_to_sync)

    def requestDivestitureConfirmation(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        self._record(
            "divestConfirmation",
            object_handle=object_handle,
            attribute_handles=attribute_handles,
        )

    def attributeOwnershipAcquisitionNotification(  # noqa: N802
        self,
        object_handle: int,
        attribute_handles: tuple[int, ...],
        owning_federate: int,
    ) -> None:
        self._record("acquired", object_handle=object_handle)

    def requestAttributeOwnershipAssumption(  # noqa: N802
        self,
        object_handle: int,
        attribute_handles: tuple[int, ...],
        divesting_federate: int,
        tag: bytes,
    ) -> None:
        self._record("assumption", object_handle=object_handle)

    def requestAttributeOwnershipRelease(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...], tag: bytes
    ) -> None:
        self._record(
            "releaseRequested",
            object_handle=object_handle,
            attribute_handles=attribute_handles,
            tag=tag,
        )

    def attributeOwnershipUnavailable(  # noqa: N802
        self, object_handle: int, attribute_handles: tuple[int, ...]
    ) -> None:
        self._record(
            "unavailable",
            object_handle=object_handle,
            attribute_handles=attribute_handles,
        )


def _join(url: str, federation: str, federate: str) -> _Recorder:
    amb = _Recorder()
    amb.connect(amb, url)
    try:
        # §4.5 — a peer may have created it first; the standard pattern.
        with contextlib.suppress(FederationExecutionAlreadyExists):
            amb.createFederationExecution(federation, [FOM])
        amb.joinFederationExecution(federate, federation)
    except BaseException:
        amb.disconnect()
        raise
    return amb


def _leave(amb: _Recorder) -> None:
    try:
        if amb._federate is not None:  # noqa: SLF001 — teardown introspection
            amb.resignFederationExecution()
    finally:
        amb.disconnect()


def _handle(amb: _Recorder) -> int:
    fed = amb._federate  # noqa: SLF001
    assert fed is not None
    return int(fed.handle)


# --- wire boundary (FakeRtiServer): dispatch shapes ---------------------------


def test_create_and_destroy_dispatch_shapes() -> None:
    """The Layer-2 methods reach the transport record() boundary with
    the M39 shapes: eager create with exist_ok=False, rolled
    create-on-join with exist_ok=True, destroy with the name."""
    from tests.spec.m4._fakes import FakeRtiServer

    fake = FakeRtiServer()
    amb = Rti1516eAmbassador()
    try:
        amb.connect(amb, "memory://fake-rti")
        amb.createFederationExecution("demo", ["demo.fom.xml"])
        amb.joinFederationExecution("alice", "demo")
        amb.resignFederationExecution()
        amb.destroyFederationExecution("demo")
    finally:
        amb.disconnect()

    creates = [c for c in fake.calls if c.method == "create_federation"]
    assert [c.args.get("exist_ok") for c in creates] == [False, True], (
        "§4.5 eager create must be strict; the rolled create-on-join stays "
        "idempotent"
    )
    destroys = [c for c in fake.calls if c.method == "destroy_federation"]
    assert len(destroys) == 1
    assert destroys[0].args == {"federation_name": "demo"}


# --- §4.5 / §4.6 — split create/destroy surface ------------------------------


@pytest.mark.integration
def test_create_is_eager_and_duplicate_raises_typed(
    rtid_url: str, federation_name: str
) -> None:
    """§4.5 — the second create of the same name raises typed
    FederationExecutionAlreadyExists (no join needed to observe it)."""
    amb = _Recorder()
    amb.connect(amb, rtid_url)
    try:
        amb.createFederationExecution(federation_name, [FOM])
        with pytest.raises(FederationExecutionAlreadyExists):
            amb.createFederationExecution(federation_name, [FOM])
        # The rolled path stays convenient: joining after the failed
        # re-create works (create-on-join is exist_ok).
        amb.joinFederationExecution("alice", federation_name)
        amb.resignFederationExecution()
    finally:
        amb.disconnect()


@pytest.mark.integration
def test_destroy_lifecycle_typed_exceptions(
    rtid_url: str, federation_name: str
) -> None:
    """§4.6 — destroy is exposed at Layer 2; FederatesCurrentlyJoined
    while joined; succeeds after resign; unknown name raises
    FederationExecutionDoesNotExist."""
    amb = _join(rtid_url, federation_name, "alice")
    try:
        with pytest.raises(FederatesCurrentlyJoined):
            amb.destroyFederationExecution(federation_name)
    finally:
        _leave(amb)
    # Post-resign destroy succeeds; Layer 2 needs a live connection.
    amb2 = _Recorder()
    amb2.connect(amb2, rtid_url)
    try:
        amb2.destroyFederationExecution(federation_name)
        with pytest.raises(FederationExecutionDoesNotExist):
            amb2.destroyFederationExecution(federation_name)
    finally:
        amb2.disconnect()


# --- unified object-path error translation ------------------------------------


@pytest.mark.integration
def test_object_path_raises_typed_exceptions(
    rtid_url: str, federation_name: str
) -> None:
    """§6.8 precondition — register on an unpublished class raises the
    typed ObjectClassNotPublished (an RtiError, same translator as the
    time path), not a raw AioRpcError."""
    amb = _join(rtid_url, federation_name, "nopub")
    try:
        with pytest.raises(ObjectClassNotPublished) as exc_info:
            amb.registerObjectInstance("Vehicle")
        assert isinstance(exc_info.value, RtiError)
    finally:
        _leave(amb)


# --- §4.14 / §4.15 — achieve with successfully=False --------------------------


@pytest.mark.integration
def test_achieve_unsuccessfully_lands_in_failed_to_sync(
    rtid_url: str, federation_name: str
) -> None:
    alice = _join(rtid_url, federation_name, "alice")
    bob = _join(rtid_url, federation_name, "bob")
    try:
        alice.registerFederationSynchronizationPoint("gate", b"")
        alice.wait_for("announce", label="gate")
        bob.wait_for("announce", label="gate")

        alice.synchronizationPointAchieved("gate")  # successfully=True default
        bob.synchronizationPointAchieved("gate", successfully=False)

        got = alice.wait_for("synchronized", label="gate")
        # §4.15 — bob achieved with successfully=False.
        assert got["failed_to_sync"] == (_handle(bob),)
    finally:
        _leave(bob)
        _leave(alice)


# --- §7 — two-phase divest, if-available, release request ---------------------


def _setup_owned_instance(pub: _Recorder, sub: _Recorder) -> tuple[int, int]:
    vehicle = pub.getObjectClassHandle("Vehicle")
    pos = pub.getAttributeHandle(vehicle, "Position")
    sub.subscribeObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
    sub.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)  # §7 candidate
    pub.publishObjectClassAttributes("Vehicle", VEHICLE_ATTRS)
    obj = pub.registerObjectInstance("Vehicle")
    sub.wait_for("discover", object_handle=int(obj))
    return int(obj), int(pos)


@pytest.mark.integration
def test_two_phase_divest_parks_until_confirm(
    rtid_url: str, federation_name: str
) -> None:
    """§7.3 + §7.6 — Layer 2 drives the full two-phase flow (two_phase
    flag + confirmDivestiture, both new at M39)."""
    pub = _join(rtid_url, federation_name, "divester")
    sub = _join(rtid_url, federation_name, "assumer")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        pub.negotiatedAttributeOwnershipDivestiture(
            obj, [pos], b"offer", two_phase=True
        )
        sub.wait_for("assumption", object_handle=obj)  # §7.3
        sub.attributeOwnershipAcquisition(obj, [pos], b"take")
        pub.wait_for("divestConfirmation", object_handle=obj)  # §7.6 park
        owner, owned = pub.queryAttributeOwnership(obj, pos)
        assert owned and owner == _handle(pub), (
            "§7.6: two-phase transfer must park until confirmDivestiture"
        )
        sub.assert_quiet("acquired")

        pub.confirmDivestiture(obj, [pos])  # §7.6 complete
        sub.wait_for("acquired", object_handle=obj)
        owner, owned = sub.queryAttributeOwnership(obj, pos)
        assert owned and owner == _handle(sub)
    finally:
        _leave(sub)
        _leave(pub)


@pytest.mark.integration
def test_acquire_if_available_reports_unavailable(
    rtid_url: str, federation_name: str
) -> None:
    """§7.9 + §7.10 — acquisition-if-available against owned attributes
    transfers nothing and fires attributeOwnershipUnavailable at the
    requester (Layer-2 method + callback both new at M39)."""
    pub = _join(rtid_url, federation_name, "owner")
    sub = _join(rtid_url, federation_name, "loser")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        sub.attributeOwnershipAcquisitionIfAvailable(obj, [pos], b"try")
        got = sub.wait_for("unavailable", object_handle=obj)  # §7.10
        assert pos in got["attribute_handles"]
        owner, owned = pub.queryAttributeOwnership(obj, pos)
        assert owned and owner == _handle(pub), (
            "§7.9: ifAvailable against owned attrs must not transfer"
        )
    finally:
        _leave(sub)
        _leave(pub)


@pytest.mark.integration
def test_plain_acquire_fires_release_request_at_owner(
    rtid_url: str, federation_name: str
) -> None:
    """§7.11 — a plain acquisition against owned attributes asks the
    owner to release, with the acquirer's tag echoed."""
    pub = _join(rtid_url, federation_name, "owner")
    sub = _join(rtid_url, federation_name, "asker")
    try:
        obj, pos = _setup_owned_instance(pub, sub)
        sub.attributeOwnershipAcquisition(obj, [pos], b"please")
        got = pub.wait_for("releaseRequested", object_handle=obj)
        assert pos in got["attribute_handles"]
        assert got["tag"] == b"please"
    finally:
        _leave(sub)
        _leave(pub)
