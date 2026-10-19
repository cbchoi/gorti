"""Live regression contracts for two independently reproduced SDK gaps.

Lookup by name: IEEE 1516.1-2010 10.6.3(d)/10.6.5(a), PDF page 252.
Annex B (PDF 439-440) incorporates the supplied Java API; its 10.6 method
declares NameNotFound, as does the supplied C++ API. ObjectClassNotFound is
not the standard method's exception. Python is a product binding, so the
exact-name assertion is a standard-shaped SDK mapping expectation, not
proof of a mandatory standardized Python binding.

Rejoining after another connection recreates the execution is an engineering
SDK lifecycle regression. Generic join/resign clauses provide context, but
this test does not claim a standard prescribes SDK generation-cache behavior.
No xfail/skip: failures remain ordinary failing results. Historical failed
runs are retained separately from post-fix executions.
"""

from __future__ import annotations

import json
import os
from contextlib import ExitStack
from pathlib import Path

import pytest

from rti1516e import Rti1516eAmbassador
from rti1516e import errors as public_errors
from rti1516e.errors import RtiError
from tests.test_ieee2010_independent_contract_live import (
    BASE_CLASS,
    EXTRA_CLASS,
    independent_artifacts,  # noqa: F401 - imported pytest fixture
)
from tests.test_ieee2010_independent_contract_live import (
    independent_server as _independent_server,  # noqa: F401 - named pytest fixture
)


def _record(run: Path, **observation):
    with (run / "gap-observations.jsonl").open("a", encoding="utf-8") as output:
        output.write(json.dumps(observation) + "\n")
    print(json.dumps(observation))


def _connected(stack: ExitStack, url: str):
    ambassador = Rti1516eAmbassador()
    ambassador.connect(ambassador, url)
    stack.callback(ambassador.disconnect)
    return ambassador


def test_missing_object_class_lookup_raises_name_not_found(independent_server):
    """Service 10.6 by-name mapping, not ObjectClassNotDefined by-handle logic."""
    url, base, _, run = independent_server
    with ExitStack() as stack:
        ambassador = _connected(stack, url)
        ambassador.createFederationExecution("ind-gap-lookup", [str(base)])
        ambassador.joinFederationExecution("lookup-reader", "ind-gap-lookup")
        known = ambassador.getObjectClassHandle(BASE_CLASS)
        assert ambassador.getObjectClassName(known) == BASE_CLASS
        try:
            # Catch the public base only to inspect the exact returned class;
            # an unrelated error is not accepted as the expected outcome.
            with pytest.raises(RtiError) as caught:
                ambassador.getObjectClassHandle("HLAobjectRoot.IndependentDefinitelyAbsent")
            observed = type(caught.value)
            cause = caught.value.__cause__
            trailers = (
                dict(cause.trailing_metadata() or ()) if hasattr(cause, "trailing_metadata") else {}
            )
            _record(
                run,
                service="getObjectClassHandle",
                expected_exception="NameNotFound",
                observed_exception=observed.__name__,
                observed_module=observed.__module__,
                detail=str(caught.value),
                trailing_spec_exception=trailers.get("rti-spec-exception"),
                public_expected_class_available=hasattr(public_errors, "NameNotFound"),
            )
            if os.environ.get("GORTI_REQUIRE_SUPPORT_NAME_TRAILER") == "1":
                assert trailers.get("rti-spec-exception") == "NameNotFound"
            assert ambassador.getObjectClassHandle(BASE_CLASS) == known
            assert ambassador.getObjectClassName(known) == BASE_CLASS
        finally:
            ambassador.resignFederationExecution("NO_ACTION")
            ambassador.destroyFederationExecution("ind-gap-lookup")
        assert observed.__name__ == "NameNotFound", (
            f"10.6 by-name lookup requires NameNotFound mapping, got {observed.__name__}"
        )
        assert getattr(public_errors, "NameNotFound", None) is observed


def test_connected_member_rejoins_recreated_execution(independent_server):
    """Engineering gap: a still-connected resigned member retains stale scope."""
    url, base, extra, run = independent_server
    with ExitStack() as stack:
        admin = _connected(stack, url)
        reused = _connected(stack, url)
        admin.createFederationExecution("ind-gap-rejoin", [str(base)])
        reused.joinFederationExecution("first-member", "ind-gap-rejoin")
        beacon = reused.getObjectClassHandle(BASE_CLASS)
        assert reused.getObjectClassName(beacon) == BASE_CLASS
        reused.resignFederationExecution("NO_ACTION")
        # Positive control: this same connection can rejoin the unchanged execution.
        reused.joinFederationExecution("unchanged-member", "ind-gap-rejoin")
        assert reused.getObjectClassHandle(BASE_CLASS) == beacon
        reused.resignFederationExecution("NO_ACTION")
        _record(run, control="same_connection_unchanged_execution_rejoin", succeeded=True)
        admin.destroyFederationExecution("ind-gap-rejoin")
        admin.createFederationExecution("ind-gap-rejoin", [str(extra)])
        fresh = _connected(stack, url)
        fresh.joinFederationExecution("fresh-member", "ind-gap-rejoin")
        sensor = fresh.getObjectClassHandle(EXTRA_CLASS)
        assert fresh.getObjectClassName(sensor) == EXTRA_CLASS
        fresh.resignFederationExecution("NO_ACTION")
        _record(run, control="fresh_connection_recreated_execution_join", succeeded=True)
        _record(
            run,
            expected="same_connected_resigned_member_rejoin_succeeds",
            classification="engineering-sdk-lifecycle-gap",
        )
        try:
            reused.joinFederationExecution("recreated-member", "ind-gap-rejoin")
        except Exception as exc:
            _record(
                run,
                observed_exception=type(exc).__name__,
                detail=str(exc),
                cause_exception=type(exc.__cause__).__name__ if exc.__cause__ else None,
                cause_detail=str(exc.__cause__) if exc.__cause__ else None,
            )
            raise
        assert reused.getObjectClassHandle(EXTRA_CLASS) == sensor
        reused.resignFederationExecution("NO_ACTION")
        admin.destroyFederationExecution("ind-gap-rejoin")
