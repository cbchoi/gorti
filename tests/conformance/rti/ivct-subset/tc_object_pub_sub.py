"""IVCT-inspired TC-005 analogue — object publish/subscribe round-trip.

Spec-anchored assertions (IEEE 1516.1-2010):

- §5.2  ``publishObjectClassAttributes`` — publisher can then register
        instances of that class; a re-publish is idempotent (spec §5.2
        note: "May be called more than once").
- §5.6  ``subscribeObjectClassAttributes`` — subscriber receives
        ``discoverObjectInstance`` for instances registered by any
        publisher of the class *AFTER* the subscription is in effect.
        Instances registered *before* subscribe are also discoverable
        (RTI publishes discovered set on catchup).
- §6.7  ``registerObjectInstance`` — auto-generated name is unique per
        federation; explicit name uses the §6.1-6.5 reservation flow.
- §6.10 ``updateAttributeValues`` — the exact byte payload of each
        attribute round-trips to every subscriber; UNORDERED delivery
        is at-least-once (Annex E note).
- §6.11 ``reflectAttributeValues`` — the subscriber's callback fires with
        the same attribute handles + the exact bytes from the update.
- §6.15 ``removeObjectInstance`` — fires on every discoverer when the
        registrant deletes or resigns.

Scaffold status (M35 Agent BF-3): body is ``pytest.skip("stub …")``.
"""

from __future__ import annotations

import pytest


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_publish_then_register_instance() -> None:
    """§5.2 + §6.7 — publishing enables subsequent registerObjectInstance."""
    pytest.skip("stub — impl in follow-on")


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_subscribe_discovers_existing_instance() -> None:
    """§5.6 — subscribing after registration still yields discoverObjectInstance."""
    pytest.skip("stub — impl in follow-on")


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_update_reflect_round_trip_bytes() -> None:
    """§6.10 + §6.11 — updated attribute bytes arrive unchanged at subscriber."""
    pytest.skip("stub — impl in follow-on")


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_remove_object_instance_on_resign() -> None:
    """§6.15 — resign fires removeObjectInstance on every discoverer."""
    pytest.skip("stub — impl in follow-on")


@pytest.mark.conformance
@pytest.mark.ivct_subset
def test_tc005_subscribe_without_publish_no_discovery() -> None:
    """§5.6 — no discoverObjectInstance for a class with zero publishers."""
    pytest.skip("stub — impl in follow-on")
