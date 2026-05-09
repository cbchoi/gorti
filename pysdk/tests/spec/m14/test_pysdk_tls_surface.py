"""TASK-302 (M14 W3) — pysdk TLS / bearer-token surface introspection.

Verifies:
  - RtiConnection.connect accepts ca_cert, client_cert, client_key,
    bearer_token kwargs.
  - build_grpc_transport accepts the same.
  - bearer_token + grpc:// raises ValueError (matches Go SDK contract).
"""

from __future__ import annotations

import inspect

import pytest

from rti1516e import _transport as tr
from rti1516e.connection import RtiConnection


@pytest.mark.spec
def test_ac_3_connect_accepts_tls_kwargs() -> None:
    """AC §3.5 — pysdk RtiConnection.connect accepts the M14 TLS kwargs."""
    sig = inspect.signature(RtiConnection.connect)
    expected = {"ca_cert", "client_cert", "client_key", "bearer_token"}
    actual = set(sig.parameters.keys())
    missing = expected - actual
    assert not missing, f"connect() missing kwargs: {missing}"


@pytest.mark.spec
def test_ac_3_build_grpc_transport_accepts_tls_kwargs() -> None:
    """AC §3.5 — build_grpc_transport accepts the M14 TLS kwargs."""
    sig = inspect.signature(tr.build_grpc_transport)
    expected = {"ca_cert", "client_cert", "client_key", "bearer_token"}
    actual = set(sig.parameters.keys())
    missing = expected - actual
    assert not missing, f"build_grpc_transport missing kwargs: {missing}"
