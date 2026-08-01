"""Unit tests for ``pyjevsim_bridge.select_preserve``.

These cover behavior not pinned by the specification test
under ``tests/spec/m4/test_spec_m4_select_preserve.py``:

* deterministic alphabetical ordering by port name,
* stability for ties on the port-name key (preserves payload order),
* empty-input no-op,
* the function does not consult the coupled-model parameter (cut-1),
  so any object satisfying ``CoupledModelProtocol`` is accepted.
"""

from __future__ import annotations

from pyjevsim_bridge.select_preserve import order_simultaneous_events
from tests.spec.m4._fakes import StubCoupledModel


def test_orders_alphabetically_by_port_name() -> None:
    cm = StubCoupledModel()
    events = [
        ("port_b", b"b1"),
        ("port_a", b"a1"),
        ("port_c", b"c1"),
    ]
    ordered = order_simultaneous_events(cm, events)
    assert [p for p, _ in ordered] == ["port_a", "port_b", "port_c"]


def test_stable_for_repeated_port_names() -> None:
    """Two events on the same port retain insertion order (sorted is
    stable)."""
    cm = StubCoupledModel()
    events = [
        ("port_a", b"first"),
        ("port_a", b"second"),
        ("port_a", b"third"),
    ]
    ordered = order_simultaneous_events(cm, events)
    assert [p for p, _ in ordered] == ["port_a"] * 3
    assert [pl for _, pl in ordered] == [b"first", b"second", b"third"]


def test_empty_input_returns_empty_list() -> None:
    cm = StubCoupledModel()
    assert order_simultaneous_events(cm, []) == []


def test_no_drops_or_duplicates() -> None:
    cm = StubCoupledModel()
    events = [(f"port_{i}", i) for i in range(10)]
    ordered = order_simultaneous_events(cm, events)
    assert sorted(ordered) == sorted(events)
    assert len(ordered) == len(events)


def test_returns_a_new_list() -> None:
    cm = StubCoupledModel()
    events = [("port_b", b"x"), ("port_a", b"y")]
    ordered = order_simultaneous_events(cm, events)
    # ``sorted`` always returns a new list; the input must be untouched
    # so callers can safely re-use it.
    assert ordered is not events
    assert events == [("port_b", b"x"), ("port_a", b"y")]
