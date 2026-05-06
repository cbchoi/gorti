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

Flips the Phase-1 deferral marker the agent-c report flagged: callback
ordering — not just terminal state — is now an observable contract.
"""

from __future__ import annotations

import asyncio
import shutil
import sys

import pytest

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
    if sys.platform == "win32":  # pragma: no cover - CI is linux/mac
        pytest.skip("rtid subprocess harness is POSIX-only at this cut")


async def _drain_until(
    fed: object, predicate, *, timeout: float = 5.0
) -> object:
    """Pull events off ``fed.events()`` until ``predicate(event)`` is true.

    Returns the matching event. Raises ``asyncio.TimeoutError`` if the
    predicate never fires within ``timeout``. Other event types are
    silently discarded — this helper is a typed-callback waiter, not a
    full stream consumer.
    """
    deadline = asyncio.get_event_loop().time() + timeout

    async def _wait() -> object:
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

        # 3. Both federates achieve. The manager closes out and emits
        #    FederationSynchronized to every required federate.
        await fed_a.sync.synchronization_point_achieved(label)
        await fed_b.sync.synchronization_point_achieved(label)

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


# Save-side callback delivery deferred: the production rtid wires
# savepoint.Manager without a MembersResolver (see
# rti/cmd/rtid/main.go around line 664), so the request-save fan-out
# fires a single broadcast envelope addressed to InvalidFederateHandle.
# multiOutbox.Send drops events with no matching subscription, so the
# typed InitiateFederateSave never reaches the federate's stream
# until rtid wires MembersResolver. The proto + manager + emission
# wiring are all in place; this is an rtid composition gap, not a
# proto / SDK gap. The sync-side test above is the definitive
# deferral-#1-closed assertion (it pins the required set explicitly,
# so the fan-out targets real federate handles).
