"""Build a classpath override that replaces Portico UDP discovery with TCP."""

from __future__ import annotations

import argparse
import io
import zipfile
from pathlib import Path
from xml.etree import ElementTree


RESOURCE = "etc/jgroups-udp.xml"


def _local_name(tag: str) -> str:
    return tag.rsplit("}", 1)[-1]


def build_override(portico_jar: Path, output: Path) -> None:
    with zipfile.ZipFile(portico_jar) as archive:
        source = archive.read(RESOURCE)

    root = ElementTree.fromstring(source)  # noqa: S314 - Portico's local JAR is trusted input.
    namespace = root.tag.partition("}")[0].removeprefix("{")

    transport_index = next(
        index for index, child in enumerate(root) if _local_name(child.tag) == "UDP"
    )
    discovery_index = next(
        index for index, child in enumerate(root) if _local_name(child.tag) == "PING"
    )
    udp = root[transport_index]
    ping = root[discovery_index]

    tcp = ElementTree.Element(
        f"{{{namespace}}}TCP",
        {
            "bind_addr": "${portico.jgroups.tcp.bindAddress:NON_LOOPBACK}",
            "bind_port": "${portico.jgroups.tcp.bindPort:7800}",
            "loopback": "true",
            **{
                key: value
                for key, value in udp.attrib.items()
                if key.startswith(
                    (
                        "enable_bundling",
                        "max_bundle_",
                        "timer",
                        "thread_pool",
                        "oob_thread_pool",
                    )
                )
            },
        },
    )
    tcpping = ElementTree.Element(
        f"{{{namespace}}}TCPPING",
        {
            "initial_hosts": "${portico.jgroups.tcp.initialHosts:127.0.0.1[7800]}",
            "port_range": "0",
            "timeout": ping.attrib.get("timeout", "2000"),
            "num_initial_members": ping.attrib.get("num_initial_members", "1"),
            "break_on_coord_rsp": ping.attrib.get("break_on_coord_rsp", "true"),
        },
    )
    root[transport_index] = tcp
    root[discovery_index] = tcpping

    ElementTree.register_namespace("", namespace)
    payload = io.BytesIO()
    ElementTree.ElementTree(root).write(payload, encoding="utf-8", xml_declaration=True)

    output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        archive.writestr(RESOURCE, payload.getvalue())


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--portico-jar", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    build_override(args.portico_jar.resolve(), args.output.resolve())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
