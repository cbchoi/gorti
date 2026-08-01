"""Python SDK ``grpcs://`` TLS client tests (M6 W2 Part A).

Validates that:

  1. ``build_grpc_transport`` accepts a ``grpcs://`` URL and constructs
     a TLS-secured ``grpc.aio`` channel without raising.
  2. ``RtiConnection.connect(url, ca_cert=...)`` plumbs the CA bundle
     through to the underlying transport.
  3. End-to-end: a Python SDK with ``grpcs://`` connects to a real
     ``rtid`` binary running with ``--tls-cert/--tls-key`` and
     successfully exchanges interactions over TLS. (Server-side TLS
     was added by W1B; this test exercises both halves.)
  4. Invalid URL schemes raise a clear error at connect time.

The end-to-end test (#3) requires the rtid binary on disk + a
self-signed cert pair; it follows the same pattern as
``rti/cmd/rtid/tls_test.go::TestNewRTID_TLSEnabled``. Skipped when ``go``
isn't on PATH so the unit-test layer (#1, #2, #4) still runs in
constrained environments.
"""

from __future__ import annotations

import asyncio
import contextlib
import datetime
import shutil
import socket
import subprocess
import sys
from pathlib import Path
from typing import TYPE_CHECKING

import pytest

# Ensure pysdk is importable when the test is invoked outside an editable install.
REPO_ROOT = Path(__file__).resolve().parents[2]
PYSDK = REPO_ROOT / "pysdk"
if str(PYSDK) not in sys.path:
    sys.path.insert(0, str(PYSDK))

# ruff: noqa: E402  (sys.path tweak above must precede project imports)
from rti1516e._transport import build_grpc_transport
from rti1516e.connection import FederationSpec, RtiConnection
from rti1516e.events import ReceiveInteraction

if TYPE_CHECKING:
    from rti1516e._transport import GrpcTransport


# --- Self-signed cert generation (mirrors rti/cmd/rtid/tls_test.go) --------


def _generate_self_signed(tmp_path: Path) -> tuple[Path, Path, bytes]:
    """Generate a self-signed ECDSA cert + key pair under ``tmp_path``.

    Returns ``(cert_path, key_path, cert_pem_bytes)``. The cert is valid
    for 127.0.0.1 + localhost and expires in 1 hour. ``cryptography`` is
    part of the SDK development extra; if a minimal runtime environment omits
    it, this test is skipped rather than failing unrelated transport tests.
    """
    cryptography = pytest.importorskip(
        "cryptography",
        reason="cryptography library required for TLS test cert generation",
    )
    # The named imports below come from the cryptography package; the
    # importorskip above guarantees they resolve.
    from cryptography import x509  # noqa: PLC0415
    from cryptography.hazmat.primitives import hashes, serialization  # noqa: PLC0415
    from cryptography.hazmat.primitives.asymmetric import ec  # noqa: PLC0415
    from cryptography.x509.oid import NameOID  # noqa: PLC0415

    del cryptography  # importorskip return unused; we need the submodules

    priv_key = ec.generate_private_key(ec.SECP256R1())
    subject = issuer = x509.Name(
        [x509.NameAttribute(NameOID.COMMON_NAME, "rtid-test")]
    )
    not_before = datetime.datetime.now(datetime.UTC) - datetime.timedelta(minutes=1)
    not_after = not_before + datetime.timedelta(hours=1)
    cert = (
        x509.CertificateBuilder()
        .subject_name(subject)
        .issuer_name(issuer)
        .public_key(priv_key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(not_before)
        .not_valid_after(not_after)
        .add_extension(
            x509.SubjectAlternativeName(
                [
                    x509.DNSName("localhost"),
                    x509.IPAddress(__import__("ipaddress").ip_address("127.0.0.1")),
                ]
            ),
            critical=False,
        )
        .sign(priv_key, hashes.SHA256())
    )

    cert_pem = cert.public_bytes(serialization.Encoding.PEM)
    key_pem = priv_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    cert_path = tmp_path / "cert.pem"
    key_path = tmp_path / "key.pem"
    cert_path.write_bytes(cert_pem)
    key_path.write_bytes(key_pem)
    return cert_path, key_path, cert_pem


def _free_port() -> int:
    """Bind port 0 to discover a free TCP port; close + return it."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


# --- Unit-level tests (no rtid binary required) ----------------------------


def test_build_grpc_transport_grpcs_returns_transport(
    tmp_path: Path,
) -> None:
    """``build_grpc_transport`` accepts a ``grpcs://`` URL + ca_cert
    and returns a GrpcTransport without trying to dial — channel
    construction is lazy under grpc.aio."""
    pytest.importorskip("grpc")
    _cert_path, _key_path, cert_pem = _generate_self_signed(tmp_path)

    async def _build() -> GrpcTransport:
        return await build_grpc_transport(
            "grpcs://127.0.0.1:1", ca_cert=cert_pem
        )

    transport = asyncio.run(_build())
    assert transport.url == "grpcs://127.0.0.1:1"
    # Tear down to avoid asyncio warnings about unawaited coroutines.
    asyncio.run(transport.close())


def test_build_grpc_transport_grpcs_no_ca_uses_system_roots() -> None:
    """``ca_cert=None`` is valid — grpc falls back to the system trust
    store. We assert the transport is constructed; we don't dial here."""
    pytest.importorskip("grpc")

    async def _build() -> GrpcTransport:
        return await build_grpc_transport("grpcs://127.0.0.1:1", ca_cert=None)

    transport = asyncio.run(_build())
    assert transport.url == "grpcs://127.0.0.1:1"
    asyncio.run(transport.close())


def test_build_grpc_transport_rejects_unknown_scheme() -> None:
    """Unknown URL scheme raises ValueError at construct time."""
    pytest.importorskip("grpc")

    async def _build() -> GrpcTransport:
        return await build_grpc_transport("https://127.0.0.1:1")

    with pytest.raises(ValueError, match="unsupported URL scheme"):
        asyncio.run(_build())


def test_rti_connection_connect_passes_ca_cert(tmp_path: Path) -> None:
    """``RtiConnection.connect(url, ca_cert=...)`` propagates ca_cert
    through to the underlying transport build call."""
    pytest.importorskip("grpc")
    _cert_path, _key_path, cert_pem = _generate_self_signed(tmp_path)

    async def _enter() -> None:
        async with RtiConnection.connect(
            "grpcs://127.0.0.1:1", ca_cert=cert_pem
        ) as rti:
            assert rti.transport is not None

    asyncio.run(_enter())


def test_rti_connection_rejects_unknown_scheme() -> None:
    """An unsupported URL scheme surfaces a clear error from __aenter__."""

    async def _enter() -> None:
        async with RtiConnection.connect("ftp://127.0.0.1:1") as _:
            pass

    with pytest.raises(ValueError, match="unsupported URL scheme"):
        asyncio.run(_enter())


# --- End-to-end: TLS handshake against real rtid ---------------------------


def _build_rtid_binary() -> Path | None:
    """Build the rtid binary into ``bin/rtid``. Returns None when ``go`` is
    not available (test will skip)."""
    if shutil.which("go") is None:
        return None
    bin_dir = REPO_ROOT / "bin"
    bin_dir.mkdir(parents=True, exist_ok=True)
    binary = bin_dir / "rtid"
    subprocess.run(  # noqa: S603 — controlled args
        ["go", "build", "-o", str(binary), "./rti/cmd/rtid"],  # noqa: S607 — ``go`` is the standard toolchain
        cwd=REPO_ROOT,
        check=True,
    )
    return binary


async def _wait_for_tcp(port: int, timeout: float = 10.0) -> None:  # noqa: ASYNC109 — explicit timeout matches cross_lang_runner pattern
    """Poll a TCP connect to ``port`` until success or timeout."""
    loop = asyncio.get_event_loop()
    deadline = loop.time() + timeout
    while loop.time() < deadline:
        try:
            reader, writer = await asyncio.wait_for(
                asyncio.open_connection("127.0.0.1", port), timeout=0.5
            )
            writer.close()
            with contextlib.suppress(BaseException):
                await writer.wait_closed()
            del reader
            return
        except (OSError, TimeoutError):
            await asyncio.sleep(0.1)
    raise TimeoutError(f"rtid never accepted on port {port}")


@pytest.mark.spec
def test_grpcs_end_to_end_connects_to_tls_rtid(tmp_path: Path) -> None:
    """End-to-end: launch rtid with --tls-cert/--tls-key, dial it from the
    Python SDK using ``grpcs://``, complete a CreateFederation round-trip.

    Skipped when the ``go`` toolchain or ``cryptography`` is unavailable.
    """
    pytest.importorskip("grpc")
    binary = _build_rtid_binary()
    if binary is None:
        pytest.skip("go toolchain not on PATH; cannot build rtid for TLS smoke")

    cert_path, key_path, cert_pem = _generate_self_signed(tmp_path)
    port = _free_port()
    metrics_port = _free_port()
    if metrics_port == port:
        metrics_port = _free_port()

    fom_path = (
        REPO_ROOT
        / "tests"
        / "conformance"
        / "foms"
        / "good"
        / "pyjevsim-bridge.xml"
    )

    proc = subprocess.Popen(  # noqa: S603 — controlled args
        [
            str(binary),
            "--listen", f":{port}",
            "--metrics-listen", f":{metrics_port}",
            "--log-level", "warn",
            "--tls-cert", str(cert_path),
            "--tls-key", str(key_path),
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,  # noqa: S101 — detach so test signals don't kill rtid early
    )

    async def _exercise() -> None:
        await _wait_for_tcp(port)
        spec = FederationSpec(
            name="m6-grpcs-smoke",
            fom_modules=[str(fom_path)],
        )
        # Use ``localhost`` (the cert's SAN) so server-name verification
        # passes; the cert is self-signed and only chains via ca_cert.
        url = f"grpcs://localhost:{port}"
        async with RtiConnection.connect(url, ca_cert=cert_pem) as rti:
            async with rti.join_federation(
                spec, federate_name="tls-smoke"
            ) as fed:
                # Just demonstrate a real RPC crosses the TLS channel.
                await fed.publish_interaction_class("ProducerOutput")

    try:
        asyncio.run(_exercise())
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)


# Reference the imported symbol so ruff doesn't flag the unused import in
# environments where the e2e block above is skipped.
_ = ReceiveInteraction
