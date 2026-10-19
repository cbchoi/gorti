"""Bounded S1516.1 service checks, not standardized Python-binding conformance.

Source SHA256 789780a38898bd513fe1dbdb877433e6518dac8d0eb577056fc0bbe9e5b5a9d4.
PDF pages 53, 56, 59, 65 and 73: connected creation, unique join names,
joined-member sync/save/restore calls and save-in-progress guards.
Uses the existing pinned independent server fixture; no server build or skip.
"""

from __future__ import annotations

import json
import threading
import time
from contextlib import ExitStack

import pytest

from rti1516e import Rti1516eAmbassador
from rti1516e.errors import (
    FederateNameAlreadyInUse,
    FederateNotExecutionMember,
    SaveInProgress,
)
from tests.test_ieee2010_independent_contract_live import (
    BASE_CLASS,
    independent_artifacts,  # noqa: F401 - imported pytest fixture
)
from tests.test_ieee2010_independent_contract_live import (
    independent_server as _independent_server,  # noqa: F401 - named pytest fixture
)


class Observer(Rti1516eAmbassador):
    def __init__(self, path):
        super().__init__()
        self.path = path
        self.condition = threading.Condition()
        self.events = []

    def record(self, kind, *values):
        with self.condition:
            event = [kind, *values]
            self.events.append(event)
            with self.path.open("a", encoding="utf-8") as output:
                output.write(json.dumps(event) + "\n")
            self.condition.notify_all()

    def wait(self, kind, label):
        deadline = time.monotonic() + 8
        while True:
            with self.condition:
                matches = [e for e in self.events if e[:2] == [kind, label]]
                if matches:
                    assert len(matches) == 1, matches
                    return matches[0]
            remaining = deadline - time.monotonic()
            assert remaining > 0, self.events
            self.evokeCallback(min(0.05, remaining))

    def announceSynchronizationPoint(self, label, tag):  # noqa: N802 - HLA callback
        self.record("announce", label, bytes(tag).hex())

    def federationSynchronized(self, label, failed_to_sync=()):  # noqa: N802 - HLA callback
        self.record("synchronized", label, list(failed_to_sync))

    def initiateFederateSave(self, label, save_time):  # noqa: N802 - HLA callback
        self.record("save", label, save_time)

    def federationNotSaved(self, label):  # noqa: N802 - HLA callback
        self.record("not-saved", label)

    def federationSaved(self, label):  # noqa: N802 - HLA callback
        self.record("saved", label)


def connected(stack, url, run, name):
    actor = Observer(run / (name + "-observations.jsonl"))
    # Return from callback delivery before a subsequent save can checkpoint it.
    actor.connect(actor, url, callback_model="HLA_EVOKED")

    def close(exc_type, primary_error, traceback):
        cleanup_errors = []
        try:
            if actor._federate is not None:  # cleanup only, not an oracle
                actor.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST")
        except Exception as exc:
            cleanup_errors.append(exc)
            actor.record("cleanup-error", type(exc).__name__, str(exc))
        try:
            actor.disconnect()
        except Exception as exc:
            cleanup_errors.append(exc)
            actor.record("cleanup-disconnect-error", type(exc).__name__, str(exc))
        if cleanup_errors and primary_error is None:
            raise cleanup_errors[0]
        return False

    stack.push(close)
    return actor


def rejection(actor, service, operation):
    # Observe the public exception, then run independent state controls even
    # when its type is wrong. The caller asserts the required type afterwards.
    try:
        operation()
    except Exception as exc:
        actor.record("rejection", service, type(exc).__name__, str(exc))
        return exc
    actor.record("accepted-negative", service)
    return None


def roster(actor):
    return tuple(actor.queryFederationAttributes().federate_handles)


def synchronize(owner, members, label):
    tag = b"\x00fm-poststate\xff"
    owner.registerFederationSynchronizationPoint(label, tag)
    for member in members:
        assert member.wait("announce", label) == ["announce", label, tag.hex()]
    for member in members:
        member.synchronizationPointAchieved(label)
    for member in members:
        assert member.wait("synchronized", label) == ["synchronized", label, []]


def test_disconnected_create_rejects_without_creating_execution(independent_server):
    """4.5.3(a), 4.5.5(h): no public NotConnected class is assumed."""
    url, base, _, run = independent_server
    offline = Observer(run / "offline-observations.jsonl")
    try:
        error = rejection(
            offline,
            "disconnected-create",
            lambda: offline.createFederationExecution("fm-offline", [str(base)]),
        )
        with ExitStack() as stack:
            admin = connected(stack, url, run, "admin")
            assert all(row.name != "fm-offline" for row in admin.listFederationExecutions())
            admin.createFederationExecution("fm-offline", [str(base)])
            admin.joinFederationExecution("control", "fm-offline")
            assert admin.getObjectClassName(admin.getObjectClassHandle(BASE_CLASS)) == BASE_CLASS
            synchronize(admin, [admin], "offline-control")
        assert error is not None, "Disconnected creation returned success instead of rejecting"
    finally:
        offline.disconnect()


@pytest.mark.parametrize("service", ["sync", "save", "restore"])
def test_connected_nonmember_rejection_preserves_roster_and_joinability(
    independent_server, service
):
    """4.11.5(c), 4.16.5(f), 4.24.5(c): Python public typed mapping."""
    url, base, _, run = independent_server
    with ExitStack() as stack:
        owner = connected(stack, url, run, "owner")
        caller = connected(stack, url, run, "caller")
        owner.createFederationExecution("fm-nonmember", [str(base)])
        owner.joinFederationExecution("owner", "fm-nonmember")
        if service == "restore":
            owner.requestFederationSave("nonmember-probe")
            owner.wait("save", "nonmember-probe")
            owner.federateSaveBegun()
            owner.federateSaveComplete()
            owner.wait("saved", "nonmember-probe")
        before = roster(owner)
        events_before = list(owner.events)
        operations = {
            "sync": lambda: caller.registerFederationSynchronizationPoint("nonmember-probe"),
            "save": lambda: caller.requestFederationSave("nonmember-probe"),
            "restore": lambda: caller.requestFederationRestore("nonmember-probe"),
        }
        error = rejection(caller, service, operations[service])
        assert roster(owner) == before
        assert owner.events == events_before
        caller.joinFederationExecution("later-member", "fm-nonmember")
        assert len(roster(owner)) == 2
        synchronize(owner, [owner, caller], "nonmember-poststate")
        assert type(error) is FederateNotExecutionMember, (
            f"{service}: expected public FederateNotExecutionMember, got {type(error).__name__}"
        )


def test_duplicate_join_name_preserves_owner_and_entrant_retry(independent_server):
    """4.9.3(d), 4.9.5(b): different connection collides with a joined name."""
    url, base, _, run = independent_server
    with ExitStack() as stack:
        owner = connected(stack, url, run, "owner")
        caller = connected(stack, url, run, "caller")
        owner.createFederationExecution("fm-join-name", [str(base)])
        owner.joinFederationExecution("taken", "fm-join-name")
        handle = owner.getFederateHandle("taken")
        before = roster(owner)
        error = rejection(
            caller,
            "duplicate-name-join",
            lambda: caller.joinFederationExecution("taken", "fm-join-name"),
        )
        assert roster(owner) == before
        assert owner.getFederateHandle("taken") == handle
        synchronize(owner, [owner], "original-member-still-active")
        caller.joinFederationExecution("unique-retry", "fm-join-name")
        assert owner.getFederateHandle("taken") == handle
        assert caller.getFederateHandle("unique-retry") != handle
        synchronize(owner, [owner, caller], "duplicate-join-poststate")
        assert type(error) is FederateNameAlreadyInUse, type(error).__name__


def test_save_guards_preserve_participants_and_release_rejected_sync_label(independent_server):
    """4.9.5(g), 4.11.5(a): initiated save cannot admit join or sync."""
    url, base, _, run = independent_server
    with ExitStack() as stack:
        owner = connected(stack, url, run, "owner")
        peer = connected(stack, url, run, "peer")
        entrant = connected(stack, url, run, "entrant")
        owner.createFederationExecution("fm-save-guards", [str(base)])
        owner.joinFederationExecution("owner", "fm-save-guards")
        peer.joinFederationExecution("peer", "fm-save-guards")
        before = roster(owner)
        owner.requestFederationSave("guard-save")
        owner.wait("save", "guard-save")
        peer.wait("save", "guard-save")
        status = owner.queryFederationSaveStatus()
        sync_error = rejection(
            owner,
            "sync-during-save",
            lambda: owner.registerFederationSynchronizationPoint("rejected-during-save"),
        )
        join_error = rejection(
            entrant,
            "join-during-save",
            lambda: entrant.joinFederationExecution("entrant", "fm-save-guards"),
        )
        assert owner.queryFederationSaveStatus() == status
        owner.abortFederationSave()
        owner.wait("not-saved", "guard-save")
        peer.wait("not-saved", "guard-save")
        assert roster(owner) == before
        synchronize(owner, [owner, peer], "rejected-during-save")
        entrant.joinFederationExecution("entrant", "fm-save-guards")
        assert len(roster(owner)) == 3
        synchronize(owner, [owner, peer, entrant], "save-guard-poststate")
        assert type(sync_error) is SaveInProgress, type(sync_error).__name__
        assert type(join_error) is SaveInProgress, type(join_error).__name__
