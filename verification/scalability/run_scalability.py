"""Compare Portico and gorti in one federation with 2, 4, 8, or 16 federates."""

from __future__ import annotations

import argparse
import importlib.util
import json
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
import uuid
from pathlib import Path, PurePosixPath
from types import ModuleType, SimpleNamespace
from typing import Any


def load_fair(repo: Path) -> ModuleType:
    path = repo / "verification" / "portico" / "compare_receive_order.py"
    sys.path.insert(0, str(path.parent))
    spec = importlib.util.spec_from_file_location("gorti_scale_fair", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def actor_names(participant_count: int) -> list[str]:
    return ["publisher", *[f"subscriber-{index}" for index in range(1, participant_count)]]


def replace_output(command: list[str], output: str) -> list[str]:
    result = list(command)
    for index, value in enumerate(result):
        if value == "--output":
            result[index + 1] = output
            return result
        if value.startswith("--output="):
            result[index] = f"--output={output}"
            return result
    raise ValueError("participant command has no output argument")


def portico_response_timeout_ms(participant_count: int) -> int:
    if participant_count <= 6:
        return 5_000
    return min(60_000, participant_count * 5_000)


def portico_declaration_settle_seconds(participant_count: int) -> float:
    return 0.0 if participant_count == 2 else 5.0


def portico_tcp_initial_hosts(hosts: list[str]) -> str:
    return ",".join(f"{host}[7800]" for host in hosts)


def scale_command(
    command: list[str],
    actor: str,
    participant_count: int,
    output: str,
    *,
    portico_tcp_hosts: list[str] | None = None,
) -> list[str]:
    result = replace_output(command, output)
    response_timeout_ms = portico_response_timeout_ms(participant_count)
    result = [
        (
            f"-Dportico.jgroups.responseTimeout={response_timeout_ms}"
            if value.startswith("-Dportico.jgroups.responseTimeout=")
            else value
        )
        for value in result
    ]
    participant_index = 0 if actor == "publisher" else int(actor.split("-", 1)[1])
    if portico_tcp_hosts is not None:
        result = [
            (
                "-Dportico.jgroups.tcp.initialHosts="
                + portico_tcp_initial_hosts(portico_tcp_hosts)
                if value.startswith("-Dportico.jgroups.tcp.initialHosts=")
                else value
            )
            for value in result
        ]
        classpath_index = result.index("-cp")
        result[classpath_index:classpath_index] = [
            "-Dportico.jgroups.tcp.bindPort=7800"
        ]
    result.extend(
        [
            "--participant-count",
            str(participant_count),
            "--participant-index",
            str(participant_index),
        ]
    )
    return result


def copy_scale_summaries(
    fair: ModuleType,
    docker: str,
    repo: Path,
    volume: str,
    run_output: str,
    output: Path,
    participant_count: int,
) -> None:
    expected: dict[str, str] = {}
    for actor in actor_names(participant_count):
        role = "publisher" if actor == "publisher" else "subscriber"
        expected[actor] = f"{actor}/{role}-summary.json"
    files = fair._volume_files(docker, repo, volume, run_output)
    if files != set(expected.values()):
        raise ValueError(f"scale summaries differ: {sorted(files)}")
    output.mkdir(parents=True, exist_ok=False)
    for actor, relative in expected.items():
        source = str(PurePosixPath(run_output) / relative)
        command = [
            *fair.docker_run_prefix(docker),
            "--rm",
            "--volume",
            f"{volume}:{fair.BENCH_ROOT}:ro",
            "--volume",
            f"{output}:/host-output",
            fair.IMAGE,
            "cp",
            "--archive",
            source,
            f"/host-output/{actor}.json",
        ]
        fair.run_checked(command, cwd=repo)


def create_volume_marker(
    fair: ModuleType,
    docker: str,
    repo: Path,
    volume: str,
    marker: str,
) -> None:
    fair._validate_volume_child(marker)
    fair.run_checked(
        [
            *fair.docker_run_prefix(docker),
            "--rm",
            "--volume",
            f"{volume}:{fair.BENCH_ROOT}",
            fair.IMAGE,
            "touch",
            marker,
        ],
        cwd=repo,
    )


def portico_uses_shared_namespace(transport: str, participant_count: int) -> bool:
    return transport == "udp" and participant_count > 2


def portico_uses_prestarted_runtimes(
    transport: str, participant_count: int
) -> bool:
    return transport == "tcp-override" and participant_count > 2


def docker_exec_command(
    docker: str,
    namespace_name: str,
    command: list[str],
) -> list[str]:
    return [
        docker,
        "exec",
        "--workdir",
        "/bench",
        namespace_name,
        *command,
    ]


def start_portico_namespace(
    fair: ModuleType,
    docker: str,
    repo: Path,
    network: str,
    volume: str,
    name: str,
    timeout_seconds: float = 10.0,
) -> Any:
    process = subprocess.Popen(
        [
            *fair.docker_run_prefix(docker),
            "--rm",
            "--name",
            name,
            "--network",
            network,
            "--volume",
            f"{volume}:{fair.BENCH_ROOT}",
            "--workdir",
            str(fair.BENCH_ROOT),
            fair.IMAGE,
            "sleep",
            "infinity",
        ],
        cwd=repo,
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    namespace = fair.ContainerProcess(name=name, role="portico-runtime", process=process)
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if process.poll() is not None:
            capture = fair.capture_process(namespace)
            raise RuntimeError(
                "Portico shared runtime exited during startup: "
                + fair.process_output_tail(capture)
            )
        if fair._resource_exists(docker, repo, "container", name):
            return namespace
        time.sleep(0.05)
    fair.remove_container(docker, repo, name)
    fair.capture_process(namespace)
    raise RuntimeError("Portico shared runtime did not become ready")


def start_portico_exec(
    fair: ModuleType,
    docker: str,
    repo: Path,
    namespace_name: str,
    role: str,
    command: list[str],
) -> Any:
    process = subprocess.Popen(
        docker_exec_command(docker, namespace_name, command),
        cwd=repo,
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return fair.ContainerProcess(
        name=f"{namespace_name}:{role}",
        role=role,
        process=process,
    )


def wait_portico_execs(
    fair: ModuleType,
    docker: str,
    repo: Path,
    runtime_names: list[str],
    participants: list[Any],
    timeout_seconds: float,
) -> dict[str, Any]:
    deadline = time.monotonic() + timeout_seconds
    failure: str | None = None
    while any(item.process.poll() is None for item in participants):
        failed = next(
            (item for item in participants if item.process.poll() not in (None, 0)),
            None,
        )
        if failed is not None:
            failure = f"{failed.role} exited with code {failed.process.returncode}"
            break
        if time.monotonic() >= deadline:
            failure = f"federates exceeded {timeout_seconds:.1f} seconds"
            break
        time.sleep(0.05)

    if failure is not None:
        for runtime_name in runtime_names:
            fair.remove_container(docker, repo, runtime_name)

    captures = {item.role: fair.capture_process(item) for item in participants}
    if failure is not None or any(capture.returncode != 0 for capture in captures.values()):
        details = " | ".join(
            f"{role}={capture.returncode}: "
            f"{(capture.stdout + capture.stderr)[-20000:].strip()}"
            for role, capture in captures.items()
        )
        raise fair.RetryableParticipantError(
            f"{failure or 'federate failure'}; {details}"
        )
    return captures


def stop_portico_namespace(
    fair: ModuleType,
    docker: str,
    repo: Path,
    namespace: Any | None,
) -> None:
    if namespace is None:
        return
    fair.remove_container(docker, repo, namespace.name)
    fair.capture_process(namespace)


def validate_scale(
    fair: ModuleType,
    output: Path,
    implementation: str,
    plan: Any,
    participant_count: int,
) -> dict[str, Any]:
    participants: dict[str, dict[str, Any]] = {}
    for actor in actor_names(participant_count):
        role = "publisher" if actor == "publisher" else "subscriber"
        raw = json.loads((output / f"{actor}.json").read_text(encoding="utf-8"))
        summary = fair.normalize_participant_summary(raw, role)
        if summary["seed"] != plan.seed or summary["count"] != plan.count:
            raise ValueError(f"{implementation} {actor}: seed/count mismatch")
        if summary["plan_sha256"] != plan.sha256:
            raise ValueError(f"{implementation} {actor}: plan mismatch")
        if not all(summary["synchronization"].values()):
            raise ValueError(f"{implementation} {actor}: synchronization incomplete")
        participants[actor] = summary

    publisher = participants["publisher"]
    if any(publisher["callback_accounting"].values()):
        raise ValueError(f"{implementation}: publisher received callbacks")
    subscribers = [participants[name] for name in actor_names(participant_count)[1:]]
    expected_per_subscriber = 2 * plan.count
    for index, summary in enumerate(subscribers, start=1):
        accounting = summary["callback_accounting"]
        if accounting["expected"] != expected_per_subscriber or accounting["delivered"] != expected_per_subscriber:
            raise ValueError(f"{implementation} subscriber-{index}: incomplete fanout")
        if any(accounting[name] for name in ("rejected", "dropped", "unexpected", "duplicates", "invalid")):
            raise ValueError(f"{implementation} subscriber-{index}: callback defect")

    durations = [
        int(summary["measurements"]["completedReceiveOrderBatch"]) for summary in subscribers
    ]
    completion_ns = max(durations)
    total_callbacks = expected_per_subscriber * len(subscribers)
    traces = [summary["callback_trace_sha256"] for summary in subscribers]
    attributes = [summary["attribute_callback_sha256"] for summary in subscribers]
    interactions = [summary["interaction_callback_sha256"] for summary in subscribers]
    if len(set(traces)) != 1 or len(set(attributes)) != 1 or len(set(interactions)) != 1:
        raise ValueError(f"{implementation}: subscribers observed different callback traces")
    evidence = {
        "participant_count": participant_count,
        "subscriber_count": participant_count - 1,
        "plan_sha256": plan.sha256,
        "topology_sha256": plan.topology_sha256,
        "callbacks_per_subscriber": expected_per_subscriber,
        "total_callbacks": total_callbacks,
        "callback_trace_sha256": traces[0],
        "attribute_callback_sha256": attributes[0],
        "interaction_callback_sha256": interactions[0],
        "defects": 0,
    }
    return {
        "implementation": implementation,
        "completion_ns": completion_ns,
        "subscriber_completion_ns": durations,
        "total_callbacks": total_callbacks,
        "throughput_callbacks_per_second": total_callbacks * 1_000_000_000.0 / completion_ns,
        "publisher_operation_medians_ns": publisher["measurements"],
        "evidence": evidence,
    }


def run_portico_scale(
    fair: ModuleType,
    args: argparse.Namespace,
    repo: Path,
    network: str,
    volume: str,
    override: Path,
    output: Path,
    run_id: str,
    plan: Any,
    participant_count: int,
) -> dict[str, Any]:
    suffix = uuid.uuid4().hex[:8]
    publisher_name = f"scale-portico-pub-{suffix}"
    federation = f"PS-{participant_count}-{suffix}"
    run_output = fair.volume_run_path(run_id)
    ready_file = fair.portico_publisher_ready_path(run_output)
    release_file = str(PurePosixPath(run_output) / ".portico-publisher-startup.release")
    scale_teardown_file = str(PurePosixPath(run_output) / ".scale-teardown")
    containers = []
    runtimes: list[Any] = []
    runtime_by_actor: dict[str, Any] = {}
    shared_namespace = portico_uses_shared_namespace(
        args.portico_transport, participant_count
    )
    prestarted_runtimes = portico_uses_prestarted_runtimes(
        args.portico_transport, participant_count
    )
    try:
        portico_transport_jar = (
            override if args.portico_transport == "tcp-override" else args.verifier_jar
        )
        if shared_namespace:
            runtime = start_portico_namespace(
                fair,
                args.docker,
                repo,
                network,
                volume,
                f"scale-portico-runtime-{suffix}",
            )
            runtimes.append(runtime)
            for actor in actor_names(participant_count):
                runtime_by_actor[actor] = runtime
        elif prestarted_runtimes:
            runtime_names = {
                "publisher": publisher_name,
                **{
                    f"subscriber-{index}": f"scale-portico-sub-{index}-{suffix}"
                    for index in range(1, participant_count)
                },
            }
            for actor in actor_names(participant_count):
                runtime = start_portico_namespace(
                    fair,
                    args.docker,
                    repo,
                    network,
                    volume,
                    runtime_names[actor],
                )
                runtimes.append(runtime)
                runtime_by_actor[actor] = runtime

        portico_tcp_hosts = (
            [runtime_by_actor[actor].name for actor in actor_names(participant_count)]
            if prestarted_runtimes
            else None
        )

        def start_participant(name: str, actor: str, command: list[str]) -> Any:
            if shared_namespace or prestarted_runtimes:
                return start_portico_exec(
                    fair,
                    args.docker,
                    repo,
                    runtime_by_actor[actor].name,
                    actor,
                    command,
                )
            return fair.start_container(
                args.docker, repo, network, volume, name, actor, command
            )

        publisher_command = fair.portico_command(
            "publisher", publisher_name, args.portico_home, args.verifier_jar,
            portico_transport_jar, args.fom, run_output, repo, federation, plan,
            args.operation_warmup, args.timeout_ms, startup_ready_file=ready_file,
            startup_release_file=release_file,
            teardown_ready_file=scale_teardown_file,
        )
        publisher = start_participant(
            publisher_name, "publisher",
            scale_command(
                publisher_command, "publisher", participant_count,
                str(PurePosixPath(run_output) / "publisher"),
                portico_tcp_hosts=portico_tcp_hosts,
            ),
        )
        containers.append(publisher)
        fair.wait_for_portico_publisher_ready(
            args.docker, repo, volume, publisher, ready_file, args.timeout_ms / 1000
        )
        fair.remove_volume_path(args.docker, repo, volume, ready_file)
        for index in range(1, participant_count):
            actor = f"subscriber-{index}"
            subscriber_join_file = str(
                PurePosixPath(run_output) / f".portico-subscriber-{index}.join.ready"
            )
            command = fair.portico_command(
                "subscriber", publisher_name, args.portico_home, args.verifier_jar,
                portico_transport_jar, args.fom, run_output, repo, federation, plan,
                args.operation_warmup, args.timeout_ms,
                join_ready_file=subscriber_join_file,
                teardown_ready_file=scale_teardown_file,
            )
            subscriber = start_participant(
                f"scale-portico-sub-{index}-{suffix}", actor,
                scale_command(
                    command, actor, participant_count,
                    str(PurePosixPath(run_output) / actor),
                    portico_tcp_hosts=portico_tcp_hosts,
                ),
            )
            containers.append(subscriber)
            fair.wait_for_portico_publisher_ready(
                args.docker, repo, volume, subscriber, subscriber_join_file,
                args.timeout_ms / 1000,
            )
            fair.remove_volume_path(
                args.docker, repo, volume, subscriber_join_file
            )
            if index < participant_count - 1:
                time.sleep(args.launch_interval_seconds)
        time.sleep(portico_declaration_settle_seconds(participant_count))
        create_volume_marker(fair, args.docker, repo, volume, release_file)
        if shared_namespace or prestarted_runtimes:
            captures = wait_portico_execs(
                fair,
                args.docker,
                repo,
                [runtime.name for runtime in runtimes],
                containers,
                args.timeout_ms / 1000 * 4,
            )
        else:
            captures = fair.wait_federates(
                args.docker, repo, containers, args.timeout_ms / 1000 * 4
            )
        copy_scale_summaries(
            fair, args.docker, repo, volume, run_output, output, participant_count
        )
        result = validate_scale(fair, output, "portico", plan, participant_count)
        result["participant_exit_codes"] = {
            actor: captures[actor].returncode for actor in actor_names(participant_count)
        }
        return result
    finally:
        try:
            if shared_namespace or prestarted_runtimes:
                for runtime in runtimes:
                    stop_portico_namespace(fair, args.docker, repo, runtime)
            else:
                fair.cleanup_container_processes(args.docker, repo, containers)
        finally:
            fair.remove_volume_path(args.docker, repo, volume, run_output)
            if output.exists():
                shutil.rmtree(output)


def run_gorti_scale(
    fair: ModuleType,
    args: argparse.Namespace,
    repo: Path,
    network: str,
    volume: str,
    rtid: Path,
    client: Path,
    output: Path,
    run_id: str,
    plan: Any,
    participant_count: int,
) -> dict[str, Any]:
    suffix = uuid.uuid4().hex[:8]
    server_name = f"scale-gorti-rtid-{suffix}"
    federation = f"GS-{participant_count}-{suffix}"
    run_output = fair.volume_run_path(run_id)
    server_output = fair.volume_server_path(run_id)
    server = None
    clients = []
    try:
        server = fair.start_container(
            args.docker, repo, network, volume, server_name, "rtid",
            [
                fair.staged_path(rtid, repo),
                "--listen=0.0.0.0:8442",
                "--metrics-listen=127.0.0.1:0",
                "--admin-listen=",
                f"--save-dir={server_output}",
                "--log-dir=",
                "--log-format=text",
                "--log-level=error",
                "--outbox-event-capacity=65536",
                "--outbox-batch-size=32",
                "--outbox-flush-interval=1ms",
                "--audit-replay-plugin=none",
            ],
            publish_ports=(8442,),
        )
        fair.wait_for_rtid_port(args.docker, repo, server, 20)
        for index in range(1, participant_count):
            actor = f"subscriber-{index}"
            command = fair.gorti_client_command(
                "subscriber", server_name, client, args.fom, run_output, repo,
                federation, plan, args.operation_warmup, args.timeout_ms,
                "local-lrc", 1024, 32, 32, "handles",
            )
            clients.append(
                fair.start_container(
                    args.docker, repo, network, volume, f"scale-gorti-sub-{index}-{suffix}",
                    actor,
                    scale_command(
                        command, actor, participant_count,
                        str(PurePosixPath(run_output) / actor),
                    ),
                )
            )
            if index < participant_count - 1:
                time.sleep(args.launch_interval_seconds)
        command = fair.gorti_client_command(
            "publisher", server_name, client, args.fom, run_output, repo,
            federation, plan, args.operation_warmup, args.timeout_ms,
            "local-lrc", 1024, 32, 32, "handles",
        )
        clients.append(
            fair.start_container(
                args.docker, repo, network, volume, f"scale-gorti-pub-{suffix}",
                "publisher",
                scale_command(
                    command, "publisher", participant_count,
                    str(PurePosixPath(run_output) / "publisher"),
                ),
            )
        )
        captures = fair.wait_federates(
            args.docker, repo, clients, args.timeout_ms / 1000 * 4
        )
        copy_scale_summaries(
            fair, args.docker, repo, volume, run_output, output, participant_count
        )
        result = validate_scale(fair, output, "gorti", plan, participant_count)
        result["participant_exit_codes"] = {
            actor: captures[actor].returncode for actor in actor_names(participant_count)
        }
        return result
    finally:
        try:
            fair.cleanup_container_processes(args.docker, repo, clients)
        finally:
            if server is not None:
                fair.stop_server(args.docker, repo, server)
            fair.remove_volume_path(args.docker, repo, volume, run_output)
            fair.remove_volume_path(args.docker, repo, volume, server_output)
            if output.exists():
                shutil.rmtree(output)


def summarize(values: list[float]) -> dict[str, float]:
    ordered = sorted(values)
    p95 = ordered[max(0, (95 * len(ordered) + 99) // 100 - 1)]
    return {
        "count": len(values),
        "median": statistics.median(values),
        "mean": statistics.fmean(values),
        "p95": p95,
        "min": ordered[0],
        "max": ordered[-1],
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--portico-home", type=Path, required=True)
    parser.add_argument("--verifier-jar", type=Path, required=True)
    parser.add_argument("--fom", type=Path, required=True)
    parser.add_argument("--workload", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--scales", type=int, nargs="+", default=[2, 3, 4, 8, 16])
    parser.add_argument("--warmup", type=int, default=1)
    parser.add_argument("--measured", type=int, default=3)
    parser.add_argument("--seed", type=int, default=1516)
    parser.add_argument("--operation-warmup", type=int, default=128)
    parser.add_argument("--launch-interval-seconds", type=float, default=0.5)
    parser.add_argument(
        "--portico-transport",
        choices=("udp", "tcp-override"),
        default="tcp-override",
    )
    parser.add_argument(
        "--implementations",
        choices=("portico", "gorti"),
        nargs="+",
        default=["portico", "gorti"],
    )
    parser.add_argument("--timeout-ms", type=int, default=300000)
    parser.add_argument("--docker", default="docker")
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    args.implementations = list(dict.fromkeys(args.implementations))
    if (
        any(scale < 2 for scale in args.scales)
        or args.warmup < 0
        or args.measured < 1
        or args.launch_interval_seconds < 0
    ):
        parser.error("scales must be >=2 and run counts must be valid")
    return args


def main() -> int:
    args = parse_args()
    repo = args.repo.resolve()
    args.portico_home = args.portico_home.resolve()
    args.verifier_jar = args.verifier_jar.resolve()
    args.fom = args.fom.resolve()
    args.workload = args.workload.resolve()
    output = args.output.resolve()
    if output.exists():
        raise FileExistsError(output)
    fair = load_fair(repo)
    plan_module = fair.load_devstone_plan_module(repo)
    workload = plan_module.load_workload(args.workload)
    workload_metadata = fair._workload_metadata(plan_module, args.workload, workload)
    records: list[dict[str, Any]] = []
    with tempfile.TemporaryDirectory(prefix="gorti-scale-", dir=repo / ".tmp") as raw:
        scratch = Path(raw)
        build_args = SimpleNamespace(go=args.go, portico_home=args.portico_home)
        rtid, client, override = fair.prepare_binaries(build_args, repo, scratch)
        volume = fair.create_benchmark_volume(args.docker, repo)
        try:
            archive = fair.create_staging_archive(
                repo,
                (
                    args.portico_home / "jre",
                    args.portico_home / "lib" / "portico.jar",
                    args.verifier_jar,
                    override,
                    args.fom,
                    rtid,
                    client,
                ),
                scratch / "scale-static.tar.gz",
            )
            fair.stage_archive(args.docker, repo, volume, archive)
            total_runs = len(args.scales) * (args.warmup + args.measured)
            position = 0
            for scale in args.scales:
                for run_index in range(args.warmup + args.measured):
                    position += 1
                    phase = "warmup" if run_index < args.warmup else "measured"
                    measured_index = run_index - args.warmup
                    seed = args.seed + scale * 100 + run_index
                    identity = f"scale-{scale}-{phase}-{run_index + 1}-{seed}"
                    plan = fair.materialize_pair_plan(
                        plan_module, workload, seed, scratch / "plans" / f"{identity}.dvshla"
                    )
                    fair.stage_artifacts(args.docker, repo, volume, (plan.path,))
                    base_order = (
                        ("portico", "gorti")
                        if run_index % 2 == 0
                        else ("gorti", "portico")
                    )
                    order = tuple(
                        implementation
                        for implementation in base_order
                        if implementation in args.implementations
                    )
                    print(
                        f"[{position}/{total_runs}] federates={scale} {phase} "
                        f"{run_index + 1}: {' -> '.join(order)}",
                        flush=True,
                    )
                    pair: dict[str, dict[str, Any]] = {}
                    for implementation in order:
                        network = fair.create_pair_network(
                            args.docker, repo, f"{identity}-{implementation}"
                        )
                        started = time.time()
                        try:
                            transient = scratch / "summaries" / f"{identity}-{implementation}"
                            if implementation == "portico":
                                result = run_portico_scale(
                                    fair, args, repo, network, volume, override, transient,
                                    f"{identity}-{implementation}", plan, scale,
                                )
                            else:
                                result = run_gorti_scale(
                                    fair, args, repo, network, volume, rtid, client, transient,
                                    f"{identity}-{implementation}", plan, scale,
                                )
                            result.update(
                                {
                                    "scale": scale,
                                    "phase": phase,
                                    "run": run_index + 1,
                                    "seed": seed,
                                    "position": order.index(implementation) + 1,
                                    "wall_seconds": time.time() - started,
                                }
                            )
                            pair[implementation] = result
                            records.append(result)
                        finally:
                            fair.remove_pair_network(args.docker, repo, network)
                    if (
                        len(pair) == 2
                        and pair["portico"]["evidence"] != pair["gorti"]["evidence"]
                    ):
                        raise ValueError(f"{identity}: Portico/gorti semantics differ")
        finally:
            fair.remove_benchmark_volume(args.docker, repo, volume)

    aggregate: dict[str, Any] = {}
    for scale in args.scales:
        measured = [r for r in records if r["scale"] == scale and r["phase"] == "measured"]
        by_impl = {
            impl: [r for r in measured if r["implementation"] == impl]
            for impl in args.implementations
        }
        aggregate[str(scale)] = {
            impl: {
                "completion_ns": summarize([float(r["completion_ns"]) for r in runs]),
                "throughput_callbacks_per_second": summarize(
                    [float(r["throughput_callbacks_per_second"]) for r in runs]
                ),
            }
            for impl, runs in by_impl.items()
        }
        if set(args.implementations) == {"portico", "gorti"}:
            aggregate[str(scale)]["paired_completion_ratio_gorti_over_portico"] = summarize(
                [
                    next(r["completion_ns"] for r in by_impl["gorti"] if r["seed"] == seed)
                    / next(r["completion_ns"] for r in by_impl["portico"] if r["seed"] == seed)
                    for seed in sorted({r["seed"] for r in measured})
                ]
            )
    result = {
        "schema": "gorti.portico-scalability/v1",
        "metadata": {
            "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "scales": args.scales,
            "implementations": args.implementations,
            "warmup_per_scale": args.warmup,
            "measured_per_scale": args.measured,
            "gorti_transport": "local-lrc",
            "portico_transport": args.portico_transport,
            "portico_process_topology": (
                "prestarted-container-per-federate"
                if any(
                    portico_uses_prestarted_runtimes(
                        args.portico_transport, scale
                    )
                    for scale in args.scales
                )
                else "shared-container-independent-processes"
                if any(
                    portico_uses_shared_namespace(
                        args.portico_transport, scale
                    )
                    for scale in args.scales
                )
                else "one-container-per-federate"
            ),
            "portico_jgroups_response_timeout_ms_by_scale": {
                str(scale): portico_response_timeout_ms(scale) for scale in args.scales
            },
            "portico_declaration_settle_seconds_by_scale": {
                str(scale): portico_declaration_settle_seconds(scale)
                for scale in args.scales
            },
            "participant_launch_interval_seconds": args.launch_interval_seconds,
            "federation_shape": "one publisher plus N-1 subscribers in one federation",
            "completion_boundary": "latest subscriber final callback arrival",
            "workload": workload_metadata,
        },
        "aggregate": aggregate,
        "runs": records,
    }
    output.mkdir(parents=True)
    fair.write_json(output / "scalability.json", result)
    print(f"PASS: {output / 'scalability.json'}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
