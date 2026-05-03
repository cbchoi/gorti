"""FakeRtiServer — back-compat re-export of the production in-process driver.

The historical implementation lived here; for M6 close it was extracted
to ``pysdk/rti1516e/_inprocess.InProcessTransport`` so production
examples (``examples/pyjevsim/runner.py``) can consume the driver
without importing test-only packages.

This module remains as a thin re-export so existing M4/M5 spec tests
that ``from spec.m4._fakes import FakeRtiServer`` (or
``from .fake_rti_server import FakeRtiServer, RecordedCall``) keep
working unchanged.

Both names are aliased to the production types:

    FakeRtiServer = InProcessTransport
    RecordedCall  = RecordedCall (the production one — same shape)

The runtime behaviour is identical:

  - Auto-registers under ``memory://fake-rti`` on construction.
  - ``record(method, **kwargs)`` captures calls + returns canned responses.
  - ``push_event``, ``events_for``, ``allocate_handle``, ``reset``,
    ``calls_for`` all preserve their existing signatures.
"""

from __future__ import annotations

from rti1516e._inprocess import InProcessTransport, RecordedCall

# Public back-compat alias. Tests import ``FakeRtiServer`` from this
# module (directly or via the package re-export); the underlying class
# is the production InProcessTransport.
FakeRtiServer = InProcessTransport

__all__ = ["FakeRtiServer", "RecordedCall"]
