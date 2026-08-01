from __future__ import annotations

import importlib.util
from pathlib import Path
import xml.etree.ElementTree as ET


MODULE_PATH = Path(__file__).with_name("run_scalability.py")
SPEC = importlib.util.spec_from_file_location("gorti_scalability_test_target", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def base_command() -> list[str]:
    return [
        "java",
        "-Dportico.jgroups.responseTimeout=5000",
        "-Dportico.jgroups.tcp.initialHosts=publisher[7800]",
        "-cp",
        "portico.jar",
        "Verifier",
        "--output",
        "/bench/run",
        "--teardown-ready-file",
        "/bench/run/.teardown",
    ]


def test_two_federates_keep_verified_teardown_handshake() -> None:
    command = MODULE.scale_command(base_command(), "publisher", 2, "/bench/publisher")

    assert command[command.index("--teardown-ready-file") + 1] == "/bench/run/.teardown"
    assert "-Dportico.jgroups.responseTimeout=5000" in command


def test_multiple_subscribers_use_scale_teardown_path() -> None:
    command = MODULE.scale_command(
        base_command(),
        "subscriber-1",
        4,
        "/bench/subscriber-1",
        portico_tcp_hosts=[
            "publisher",
            "subscriber-1",
            "subscriber-2",
            "subscriber-3",
        ],
    )

    assert command[command.index("--teardown-ready-file") + 1] == "/bench/run/.teardown"
    assert "-Dportico.jgroups.responseTimeout=5000" in command
    assert "-Dportico.jgroups.tcp.bindPort=7800" in command
    assert command[-4:] == ["--participant-count", "4", "--participant-index", "1"]


def test_portico_timeout_scales_with_federation_size() -> None:
    assert MODULE.portico_response_timeout_ms(2) == 5_000
    assert MODULE.portico_response_timeout_ms(3) == 5_000
    assert MODULE.portico_response_timeout_ms(4) == 5_000
    assert MODULE.portico_response_timeout_ms(5) == 5_000
    assert MODULE.portico_response_timeout_ms(6) == 5_000
    assert MODULE.portico_response_timeout_ms(8) == 40_000
    assert MODULE.portico_response_timeout_ms(16) == 60_000


def test_portico_declaration_settle_applies_only_above_two_federates() -> None:
    assert MODULE.portico_declaration_settle_seconds(2) == 0.0
    assert MODULE.portico_declaration_settle_seconds(4) == 5.0


def test_portico_uses_shared_namespace_above_two_federates() -> None:
    assert not MODULE.portico_uses_shared_namespace("udp", 2)
    assert MODULE.portico_uses_shared_namespace("udp", 3)
    assert MODULE.portico_uses_shared_namespace("udp", 4)
    assert MODULE.portico_uses_prestarted_runtimes("tcp-override", 3)
    assert not MODULE.portico_uses_shared_namespace("tcp-override", 4)
    assert MODULE.portico_uses_prestarted_runtimes("tcp-override", 4)


def test_three_federates_have_one_publisher_and_two_subscribers() -> None:
    assert MODULE.actor_names(3) == [
        "publisher",
        "subscriber-1",
        "subscriber-2",
    ]


def test_fom_uses_distinct_readiness_and_acknowledgement_interactions() -> None:
    fom = (
        MODULE_PATH.parents[2]
        / "verification"
        / "commercial-rti"
        / "fom"
        / "CommercialRtiVerifier.xml"
    )
    root = ET.parse(fom).getroot()
    namespace = {"hla": "http://www.sisostds.org/schemas/IEEE1516-2010"}
    names = {
        element.text
        for element in root.findall(
            ".//hla:interactions//hla:interactionClass/hla:name", namespace
        )
    }

    assert "VerifierSubscriberReady" in names
    assert "VerifierPublisherAck" in names


def test_portico_tcp_hosts_cover_every_shared_process() -> None:
    assert MODULE.portico_tcp_initial_hosts(
        ["publisher", "subscriber-1", "subscriber-2", "subscriber-3"]
    ) == (
        "publisher[7800],subscriber-1[7800],"
        "subscriber-2[7800],subscriber-3[7800]"
    )


def test_docker_exec_runs_independent_process_in_shared_namespace() -> None:
    assert MODULE.docker_exec_command(
        "docker", "portico-runtime", ["java", "-version"]
    ) == [
        "docker",
        "exec",
        "--workdir",
        "/bench",
        "portico-runtime",
        "java",
        "-version",
    ]
