"""TASK-290 (M24 W4) — Python-side acceptance gate for AC §3.

Surface introspection: pysdk's _resign_federation accepts an action
parameter; ResignAction enum values are reachable; the proto stubs
expose the 5 new resign actions + new Federation/Savepoint RPCs.
"""

from __future__ import annotations

import inspect

import pytest


@pytest.mark.spec
def test_ac_3_8_resign_dispatch_accepts_action() -> None:
    """AC §3.8 — pysdk's _resign_federation dispatch accepts action kwarg."""
    from rti1516e import _transport as tr

    src = inspect.getsource(tr.GrpcTransport._resign_federation)
    assert "action" in src, "_resign_federation must take action parameter"
    # The dispatch reads kwargs.get("action") — quick check that the
    # router forwards the value.
    router_src = inspect.getsource(tr.GrpcTransport.record)
    assert 'kwargs.get("action")' in router_src or "kwargs['action']" in router_src, (
        "record() router does not forward 'action' to _resign_federation"
    )


# The proto-stub presence tests below are wrapped in a path-aware
# import so the test module loads even when rti.v1 isn't on sys.path
# at collection time. The pysdk + tests tree has different sys.path
# behavior across environments; the source-level checks above catch
# the most important drift.


@pytest.mark.spec
def test_ac_3_8_resign_action_proto_enum_complete() -> None:
    """AC §3.10 — proto ResignAction enum has all 7 values."""
    pytest.importorskip("rti.v1.common_pb2")
    from rti.v1 import common_pb2

    expected = {
        "RESIGN_ACTION_UNSPECIFIED",
        "RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES",
        "RESIGN_ACTION_DELETE_THEN_DIVEST",
        "RESIGN_ACTION_CANCEL_THEN_DELETE",
        "RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP",
        "RESIGN_ACTION_NO_ACTION",
        "RESIGN_ACTION_DELETE_OBJECTS",
    }
    actual = set(common_pb2.ResignAction.keys())
    missing = expected - actual
    assert not missing, f"ResignAction enum missing: {missing}"


@pytest.mark.spec
def test_ac_3_5_list_federation_members_proto_present() -> None:
    """AC §3.5 — ListFederationMembers RPC declared in proto stubs."""
    pytest.importorskip("rti.v1.federation_pb2")
    from rti.v1 import federation_pb2

    assert hasattr(federation_pb2, "ListFederationMembersRequest")
    assert hasattr(federation_pb2, "ListFederationMembersResponse")
    assert hasattr(federation_pb2, "FederationMember")


@pytest.mark.spec
def test_ac_3_6_3_7_abort_save_restore_proto_present() -> None:
    """AC §3.6 + §3.7 — Abort RPCs declared in savepoint proto."""
    pytest.importorskip("rti.v1.savepoint_pb2")
    from rti.v1 import savepoint_pb2

    assert hasattr(savepoint_pb2, "AbortFederationSaveRequest")
    assert hasattr(savepoint_pb2, "AbortFederationRestoreRequest")
