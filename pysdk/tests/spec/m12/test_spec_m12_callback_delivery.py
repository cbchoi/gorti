"""M12 W2 follow-up — assert on cut-2 callback delivery (not just Query).

Closes M12 W2 deferral #1: with the FederateEvent oneof now carrying
sync / ownership-transfer / save callback variants (proto tags
20/21, 30/31/32, 40/41/42), federates can dispatch on the actual
callback delivered through StreamService.Events instead of polling
Query RPCs.

This module re-runs the same sync round-trip that
``test_spec_m12_sync_register_and_achieve`` exercises, then ALSO drains
the federate's event stream and asserts that each side received a
:class:`FederationSynchronized` callback with the registered label.

This closes the Phase-1 callback-ordering deferral: callback ordering, not
just terminal state, is now an observable contract.
"""

from __future__ import annotations

import asyncio
import shutil
from collections.abc import Callable
from typing import Any

import pytest

from rti1516e.errors import SynchronizationPointLabelNotAnnounced
from rti1516e.events import (
    FederationSynchronized,
    SynchronizationPointAnnounced,
)
from tests.spec.m12._helpers import (
    RtidProcess,
    two_federates,
    write_minimal_fom,
)


def _go_or_skip() -> None:
    """Skip the test when the go toolchain is not on PATH."""
    if shutil.which("go") is None:
        pytest.skip("go toolchain not on PATH; cannot build rtid for the smoke test")


# noqa: ASYNC109 — ``timeout`` is the deadline for the entire poll
# loop, not a per-call I/O timeout. Same exemption as M5's _helpers.
async def _drain_until(
    fed: Any,
    predicate: Callable[[Any], bool],
    *,
    timeout: float = 5.0,  # noqa: ASYNC109
) -> Any:
    """Pull events off ``fed.events()`` until ``predicate(event)`` is true.

    Returns the matching event. Raises ``asyncio.TimeoutError`` if the
    predicate never fires within ``timeout``. Other event types are
    silently discarded — this helper is a typed-callback waiter, not a
    full stream consumer.
    """
    deadline = asyncio.get_event_loop().time() + timeout

    async def _wait() -> Any:
        async for ev in fed.events():
            if predicate(ev):
                return ev
        raise RuntimeError("federate event stream closed before match")

    remaining = deadline - asyncio.get_event_loop().time()
    return await asyncio.wait_for(_wait(), timeout=max(0.1, remaining))


@pytest.mark.spec
@pytest.mark.integration
def test_spec_m12_sync_synchronized_callback_delivered() -> None:
    """Each federate receives a FederationSynchronized callback.

    Drives the same protocol as
    ``test_spec_m12_sync_register_and_achieve``, then drains both
    federates' event streams and asserts each saw a
    :class:`FederationSynchronized` with ``label == "phase1"``.

    Both federates also assert on the prior
    :class:`SynchronizationPointAnnounced` callback that fires at
    Register-time — the typed event lets a federate dispatch on the
    announcement itself rather than discovering the sync point via
    Query.
    """
    _go_or_skip()
    asyncio.run(_run())


async def _run() -> None:
    fom_path = write_minimal_fom()
    label = "phase1"
    async with RtidProcess() as rtid, two_federates(
        rtid.url, federation_name="m12-sync-cb", fom_path=fom_path
    ) as (fed_a, fed_b):
        # 1. fed_a registers the sync point with both federates pinned.
        await fed_a.sync.register_synchronization_point(
            label,
            tag=b"hello",
            required_federates=[fed_a.handle, fed_b.handle],
        )

        # 2. Both federates should receive SynchronizationPointAnnounced.
        #    Drain in parallel so order independence is exercised.
        announced_a, announced_b = await asyncio.gather(
            _drain_until(
                fed_a,
                lambda ev: isinstance(ev, SynchronizationPointAnnounced),
            ),
            _drain_until(
                fed_b,
                lambda ev: isinstance(ev, SynchronizationPointAnnounced),
            ),
        )
        assert isinstance(announced_a, SynchronizationPointAnnounced)
        assert announced_a.label == label
        assert announced_a.tag == b"hello"
        assert set(announced_a.required_federates) == {fed_a.handle, fed_b.handle}
        assert isinstance(announced_b, SynchronizationPointAnnounced)
        assert announced_b.label == label
        assert announced_b.tag == b"hello"

        for fed, announcement in ((fed_a, announced_a), (fed_b, announced_b)):
            with pytest.raises(SynchronizationPointLabelNotAnnounced):
                await fed.sync.synchronization_point_achieved(label)

            async def achieve(event, participant=fed):
                assert event.label == label
                await participant.sync.synchronization_point_achieved(event.label)

            await fed.dispatch_event(announcement, achieve)

        # 4. Drain the FederationSynchronized callback on both sides.
        sync_a, sync_b = await asyncio.gather(
            _drain_until(
                fed_a,
                lambda ev: isinstance(ev, FederationSynchronized),
            ),
            _drain_until(
                fed_b,
                lambda ev: isinstance(ev, FederationSynchronized),
            ),
        )
        assert isinstance(sync_a, FederationSynchronized)
        assert sync_a.label == label
        assert isinstance(sync_b, FederationSynchronized)
        assert sync_b.label == label
        await fed_a.dispatch_event(sync_a, lambda event: None)
        await fed_b.dispatch_event(sync_b, lambda event: None)
