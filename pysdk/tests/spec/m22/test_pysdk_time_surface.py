"""TASK-225 (M22 W1) — pysdk Federate + Ambassador surface parity.

Per AC §3.1-3.2: pysdk Federate exposes all 15 time methods and
Rti1516eAmbassador exposes all 15 corresponding camelCase methods.

This test pins the surface; if a method is removed it fails loudly.
"""

from __future__ import annotations

import pytest

from rti1516e.connection import Federate
from rti1516e.standard import Rti1516eAmbassador

# Snake_case methods on Federate. Order: 4 enable/disable, modify,
# 5 advance primitives, 3 queries, 2 async toggles. Total = 15.
FEDERATE_METHODS = (
    "enable_time_regulation",
    "disable_time_regulation",
    "enable_time_constrained",
    "disable_time_constrained",
    "modify_lookahead",
    "next_message_request",
    "next_message_request_available",
    "time_advance_request",
    "time_advance_request_available",
    "flush_queue_request",
    "query_logical_time",
    "query_lookahead",
    "query_lbts",
    "enable_asynchronous_delivery",
    "disable_asynchronous_delivery",
)

# CamelCase methods on Rti1516eAmbassador. Same 15.
AMBASSADOR_METHODS = (
    "enableTimeRegulation",
    "disableTimeRegulation",
    "enableTimeConstrained",
    "disableTimeConstrained",
    "modifyLookahead",
    "nextMessageRequest",
    "nextMessageRequestAvailable",
    "timeAdvanceRequest",
    "timeAdvanceRequestAvailable",
    "flushQueueRequest",
    "queryLogicalTime",
    "queryLookahead",
    "queryLBTS",
    "enableAsynchronousDelivery",
    "disableAsynchronousDelivery",
)


@pytest.mark.spec
@pytest.mark.parametrize("name", FEDERATE_METHODS)
def test_federate_method_exists(name: str) -> None:
    """AC §3.1 — Federate.<name> is callable."""
    method = getattr(Federate, name, None)
    assert method is not None, f"Federate is missing {name!r}"
    assert callable(method), f"Federate.{name} is not callable"


@pytest.mark.spec
@pytest.mark.parametrize("name", AMBASSADOR_METHODS)
def test_ambassador_method_exists(name: str) -> None:
    """AC §3.2 — Rti1516eAmbassador.<name> is callable."""
    method = getattr(Rti1516eAmbassador, name, None)
    assert method is not None, f"Rti1516eAmbassador is missing {name!r}"
    assert callable(method), f"Rti1516eAmbassador.{name} is not callable"


@pytest.mark.spec
def test_method_count_matches() -> None:
    """Surface arity sanity: exactly 15 each per plan §2.1."""
    assert len(FEDERATE_METHODS) == 15
    assert len(AMBASSADOR_METHODS) == 15
