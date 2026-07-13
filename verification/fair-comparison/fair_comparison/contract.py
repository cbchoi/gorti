"""Dependency-free validation for fair-comparison configuration and artifacts."""

from __future__ import annotations

import hashlib
import json
import math
import re
from collections.abc import Mapping, Sequence
from pathlib import Path
from typing import Any, Final, cast

CONFIG_SCHEMA: Final = "gorti.fair-comparison/launcher-config-v1"
RESULT_SCHEMA: Final = "gorti.fair-comparison/launcher-result-v1"
MANIFEST_SCHEMA: Final = "gorti.fair-comparison/session-manifest-v1"
ANALYSIS_SCHEMA: Final = "gorti.fair-comparison/analysis-v1"
WORKLOAD_SCHEMA: Final = "gorti.fair-comparison/workload-v1"

IMPLEMENTATIONS: Final = ("pitch", "go")
ORDERS: Final = ("AB", "BA")
STATISTICS: Final = ("median_ns", "p95_ns", "p99_ns")
REQUIRED_TOKENS: Final = (
    "{fom}",
    "{seed}",
    "{count}",
    "{server_event_log}",
    "{output}",
    "{run_id}",
    "{workload_file}",
)
_SHA256: Final = re.compile(r"[0-9a-f]{64}")
_METRIC_NAME: Final = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]*")
_KDRTI_MAGIC: Final = b"KDRTI\x00\x01\x00"
_KDRTI_V2_HEADER_BYTES: Final = 64


class ContractError(ValueError):
    """Raised when comparison input is unsafe to compare."""


def _object_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ContractError(f"duplicate JSON object key: {key!r}")
        result[key] = value
    return result


def loads_json_object(data: str, source: str = "JSON") -> dict[str, Any]:
    """Load one JSON object while rejecting duplicate keys and non-finite numbers."""
    try:
        value = json.loads(
            data,
            object_pairs_hook=_object_pairs,
            parse_constant=lambda value: (_ for _ in ()).throw(
                ContractError(f"non-finite JSON number: {value}")
            ),
        )
    except json.JSONDecodeError as error:
        raise ContractError(f"{source}: cannot load JSON: {error}") from error
    if not isinstance(value, dict):
        raise ContractError(f"{source}: root must be a JSON object")
    return value


def load_json(path: Path) -> dict[str, Any]:
    try:
        data = path.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as error:
        raise ContractError(f"{path}: cannot read JSON: {error}") from error
    return loads_json_object(data, str(path))


def canonical_json(value: object) -> str:
    """Return the stable JSON representation used for identity checks."""
    return json.dumps(
        value,
        ensure_ascii=True,
        allow_nan=False,
        sort_keys=True,
        separators=(",", ":"),
    )


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def sha256_json(value: object) -> str:
    return hashlib.sha256(canonical_json(value).encode("utf-8")).hexdigest()


def _mapping(value: object, path: str) -> Mapping[str, Any]:
    if not isinstance(value, dict):
        raise ContractError(f"{path} must be an object")
    return value


def _sequence(value: object, path: str) -> Sequence[Any]:
    if not isinstance(value, list):
        raise ContractError(f"{path} must be an array")
    return value


def _string(value: object, path: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ContractError(f"{path} must be a non-empty string")
    return value


def _integer(value: object, path: str, *, minimum: int = 0) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < minimum:
        raise ContractError(f"{path} must be an integer >= {minimum}")
    return value


def _number(value: object, path: str, *, minimum: float = 0.0) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ContractError(f"{path} must be a number")
    result = float(value)
    if not math.isfinite(result) or result < minimum:
        raise ContractError(f"{path} must be finite and >= {minimum}")
    return result


def _sha256(value: object, path: str) -> str:
    result = _string(value, path)
    if not _SHA256.fullmatch(result):
        raise ContractError(f"{path} must be a lowercase SHA-256 digest")
    return result


def _relative_path(value: object, path: str) -> str:
    result = _string(value, path)
    parsed = Path(result)
    if parsed.is_absolute() or ".." in parsed.parts:
        raise ContractError(f"{path} must stay relative to the session directory")
    return result


def _exact_keys(
    value: Mapping[str, Any], path: str, required: set[str], optional: set[str]
) -> None:
    missing = required - value.keys()
    extra = value.keys() - required - optional
    if missing:
        raise ContractError(f"{path} is missing keys: {', '.join(sorted(missing))}")
    if extra:
        raise ContractError(f"{path} has unsupported keys: {', '.join(sorted(extra))}")


def validate_workload(value: object, path: str = "workload") -> dict[str, Any]:
    workload = _mapping(value, path)
    required = {
        "schema",
        "fom_sha256",
        "seed",
        "count",
        "two_process",
        "choreography",
        "delivery_boundary",
        "callback",
        "server_event_log",
    }
    _exact_keys(workload, path, required, set())
    if workload["schema"] != WORKLOAD_SCHEMA:
        raise ContractError(f"{path}.schema must be {WORKLOAD_SCHEMA!r}")
    _sha256(workload["fom_sha256"], f"{path}.fom_sha256")
    if workload["seed"] != 1516:
        raise ContractError(f"{path}.seed must be 1516")
    _integer(workload["count"], f"{path}.count", minimum=1)
    if workload["two_process"] is not True:
        raise ContractError(f"{path}.two_process must be true")
    fixed = {
        "choreography": "sequential_update_send_then_tar",
        "delivery_boundary": "subscriber_pre_tar_to_both_callbacks",
        "callback": "immediate",
    }
    for name, expected in fixed.items():
        if workload[name] != expected:
            raise ContractError(f"{path}.{name} must be {expected!r}")
    if workload["server_event_log"] not in {"off", "file"}:
        raise ContractError(f"{path}.server_event_log must be 'off' or 'file'")
    return dict(workload)


def validate_config(value: object) -> dict[str, Any]:
    config = _mapping(value, "config")
    _exact_keys(config, "config", {"schema", "launchers"}, set())
    if config["schema"] != CONFIG_SCHEMA:
        raise ContractError(f"config.schema must be {CONFIG_SCHEMA!r}")

    launchers = _mapping(config["launchers"], "config.launchers")
    _exact_keys(launchers, "config.launchers", set(IMPLEMENTATIONS), set())
    for implementation in IMPLEMENTATIONS:
        path = f"config.launchers.{implementation}"
        launcher = _mapping(launchers[implementation], path)
        _exact_keys(
            launcher,
            path,
            {"executable", "arguments", "result_file"},
            {"working_directory", "environment"},
        )
        _string(launcher["executable"], f"{path}.executable")
        arguments = _sequence(launcher["arguments"], f"{path}.arguments")
        for index, argument in enumerate(arguments):
            _string(argument, f"{path}.arguments[{index}]")
        rendered = "\n".join(str(argument) for argument in arguments)
        missing_tokens = [token for token in REQUIRED_TOKENS if token not in rendered]
        if missing_tokens:
            raise ContractError(f"{path}.arguments is missing tokens: {', '.join(missing_tokens)}")
        result_file = _string(launcher["result_file"], f"{path}.result_file")
        if Path(result_file).is_absolute() or ".." in Path(result_file).parts:
            raise ContractError(f"{path}.result_file must stay inside the run output directory")
        if "working_directory" in launcher:
            _string(launcher["working_directory"], f"{path}.working_directory")
        environment = _mapping(launcher.get("environment", {}), f"{path}.environment")
        for name, item in environment.items():
            _string(name, f"{path}.environment key")
            if not isinstance(item, str):
                raise ContractError(f"{path}.environment.{name} must be a string")
    return dict(config)


def _validate_server_process(value: object, path: str) -> None:
    process = _mapping(value, path)
    _exact_keys(
        process,
        path,
        {
            "lifecycle",
            "pid",
            "started_at",
            "executable",
            "executable_sha256",
            "argv",
        },
        set(),
    )
    if process["lifecycle"] not in ("per_arm", "persistent_session"):
        raise ContractError(f"{path}.lifecycle must be 'per_arm' or 'persistent_session'")
    _integer(process["pid"], f"{path}.pid", minimum=1)
    _string(process["started_at"], f"{path}.started_at")
    _string(process["executable"], f"{path}.executable")
    _sha256(process["executable_sha256"], f"{path}.executable_sha256")
    argv = _sequence(process["argv"], f"{path}.argv")
    if not argv:
        raise ContractError(f"{path}.argv must not be empty")
    for index, argument in enumerate(argv):
        if not isinstance(argument, str):
            raise ContractError(f"{path}.argv[{index}] must be a string")


def _validate_server_logs(value: object, path: str) -> None:
    logs = _mapping(value, path)
    _exact_keys(logs, path, {"stdout", "stderr"}, set())
    for stream in ("stdout", "stderr"):
        log_path = Path(_string(logs[stream], f"{path}.{stream}"))
        if not log_path.is_absolute():
            raise ContractError(f"{path}.{stream} must be an absolute path")


def _read_kdrti_v2_header(path: Path) -> tuple[str, int]:
    try:
        with path.open("rb") as stream:
            header = stream.read(_KDRTI_V2_HEADER_BYTES)
    except OSError as error:
        raise ContractError(f"result.provenance.event_log.path cannot be read: {error}") from error
    if len(header) != _KDRTI_V2_HEADER_BYTES:
        raise ContractError("result.provenance.event_log has a truncated kdrti/v2 header")
    if header[:8] != _KDRTI_MAGIC or int.from_bytes(header[8:12], "little") != 2:
        raise ContractError("result.provenance.event_log header must be kdrti/v2")
    encoded_federation = header[12:44]
    first_padding = encoded_federation.find(b"\x00")
    if first_padding < 0:
        first_padding = len(encoded_federation)
    elif any(encoded_federation[first_padding:]):
        raise ContractError("result.provenance.event_log header federation padding is invalid")
    try:
        federation = encoded_federation[:first_padding].decode("utf-8")
    except UnicodeDecodeError as error:
        raise ContractError("result.provenance.event_log header federation is not UTF-8") from error
    generation = int.from_bytes(header[44:52], "little")
    return federation, generation


def _validate_event_log(value: object, path: str, *, expected_federation: str) -> None:
    descriptor = _mapping(value, path)
    _exact_keys(descriptor, path, {"path", "header", "bytes", "sha256"}, set())
    event_log_path = Path(_string(descriptor["path"], f"{path}.path"))
    if not event_log_path.is_absolute():
        raise ContractError(f"{path}.path must be an absolute path")
    header = _mapping(descriptor["header"], f"{path}.header")
    _exact_keys(
        header,
        f"{path}.header",
        {"format", "federation", "generation"},
        set(),
    )
    if header["format"] != "kdrti/v2":
        raise ContractError(f"{path}.header.format must be 'kdrti/v2'")
    federation = _string(header["federation"], f"{path}.header.federation")
    generation = _integer(header["generation"], f"{path}.header.generation", minimum=1)
    if federation != expected_federation:
        raise ContractError(f"{path}.header.federation does not match this run's Go federation")

    expected_parent = expected_federation.encode("utf-8").hex()
    expected_name = f"{generation:016x}.log"
    if event_log_path.parent.name != expected_parent or event_log_path.name != expected_name:
        raise ContractError(f"{path}.path is not the exact generation-qualified path")

    expected_bytes = _integer(
        descriptor["bytes"], f"{path}.bytes", minimum=_KDRTI_V2_HEADER_BYTES + 1
    )
    expected_hash = _sha256(descriptor["sha256"], f"{path}.sha256")
    try:
        actual_bytes = event_log_path.stat().st_size
    except OSError as error:
        raise ContractError(f"{path}.path cannot be inspected: {error}") from error
    if actual_bytes != expected_bytes:
        raise ContractError(f"{path}.bytes does not match the sealed event log")

    actual_federation, actual_generation = _read_kdrti_v2_header(event_log_path)
    if actual_federation != federation:
        raise ContractError(f"{path}.header.federation does not match the event-log header")
    if actual_generation != generation:
        raise ContractError(f"{path}.header.generation does not match the event-log header")
    try:
        actual_hash = sha256_file(event_log_path)
    except OSError as error:
        raise ContractError(f"{path}.path cannot be hashed: {error}") from error
    if actual_hash != expected_hash:
        raise ContractError(f"{path}.sha256 does not match the sealed event log")


def _validate_provenance(
    value: object,
    path: str,
    *,
    implementation: str,
    workload: Mapping[str, Any],
    run_id: str,
    claim_grade: bool,
) -> None:
    provenance = _mapping(value, path)
    required = {
        "commit",
        "binary_sha256",
        "runtime_versions",
        "build_flags",
        "exact_argv",
        "environment",
    }
    optional = {"notes", "event_log", "server_process", "server_logs"}
    _exact_keys(provenance, path, required, optional)
    _string(provenance["commit"], f"{path}.commit")
    _sha256(provenance["binary_sha256"], f"{path}.binary_sha256")
    versions = _mapping(provenance["runtime_versions"], f"{path}.runtime_versions")
    if not versions:
        raise ContractError(f"{path}.runtime_versions must not be empty")
    for name, version in versions.items():
        _string(name, f"{path}.runtime_versions key")
        _string(version, f"{path}.runtime_versions.{name}")
    for index, flag in enumerate(_sequence(provenance["build_flags"], f"{path}.build_flags")):
        _string(flag, f"{path}.build_flags[{index}]")
    for index, argument in enumerate(_sequence(provenance["exact_argv"], f"{path}.exact_argv")):
        if not isinstance(argument, str):
            raise ContractError(f"{path}.exact_argv[{index}] must be a string")
    _mapping(provenance["environment"], f"{path}.environment")
    if "server_process" in provenance:
        _validate_server_process(provenance["server_process"], f"{path}.server_process")
    if "server_logs" in provenance:
        _validate_server_logs(provenance["server_logs"], f"{path}.server_logs")
    if "event_log" in provenance:
        if implementation != "go" or workload["server_event_log"] != "file":
            raise ContractError(f"{path}.event_log is only valid for Go file mode")
        _validate_event_log(
            provenance["event_log"],
            f"{path}.event_log",
            expected_federation=f"GortiGoFair-{run_id}",
        )
    if claim_grade:
        missing = {"server_process", "server_logs"} - provenance.keys()
        if implementation == "go" and workload["server_event_log"] == "file":
            missing |= {"event_log"} - provenance.keys()
        if missing:
            raise ContractError(
                f"{path} is missing claim-grade evidence: {', '.join(sorted(missing))}"
            )


def validate_result(
    value: object,
    *,
    expected_workload: Mapping[str, Any] | None = None,
    expected_implementation: str | None = None,
    expected_run_id: str | None = None,
    claim_grade: bool = False,
) -> dict[str, Any]:
    result = _mapping(value, "result")
    required = {
        "schema",
        "run_id",
        "implementation",
        "workload",
        "semantics",
        "provenance",
        "metrics",
        "accounting",
    }
    _exact_keys(result, "result", required, set())
    if result["schema"] != RESULT_SCHEMA:
        raise ContractError(f"result.schema must be {RESULT_SCHEMA!r}")
    run_id = _string(result["run_id"], "result.run_id")
    implementation = _string(result["implementation"], "result.implementation")
    if implementation not in IMPLEMENTATIONS:
        raise ContractError(f"result.implementation must be one of {IMPLEMENTATIONS}")
    if expected_run_id is not None and run_id != expected_run_id:
        raise ContractError(f"result.run_id mismatch: {run_id!r} != {expected_run_id!r}")
    if expected_implementation is not None and implementation != expected_implementation:
        raise ContractError(
            f"result.implementation mismatch: {implementation!r} != {expected_implementation!r}"
        )

    workload = validate_workload(result["workload"], "result.workload")
    if expected_workload is not None and canonical_json(workload) != canonical_json(
        expected_workload
    ):
        raise ContractError("result workload does not match the orchestrator workload")

    semantics = _mapping(result["semantics"], "result.semantics")
    _exact_keys(
        semantics,
        "result.semantics",
        {"normalization", "canonical_projection", "projection_sha256", "status"},
        set(),
    )
    if semantics["normalization"] != "gorti.fm-dm-om-tm-projection/v1":
        raise ContractError("result.semantics.normalization is unsupported")
    projection = _sequence(
        semantics["canonical_projection"], "result.semantics.canonical_projection"
    )
    if len(projection) != 4:
        raise ContractError("result.semantics.canonical_projection must contain four records")
    services: list[str] = []
    for index, item in enumerate(projection):
        projection_path = f"result.semantics.canonical_projection[{index}]"
        record = _mapping(item, projection_path)
        _exact_keys(record, projection_path, {"service", "record"}, set())
        services.append(_string(record["service"], f"{projection_path}.service"))
        _mapping(record["record"], f"{projection_path}.record")
    if services != ["FM", "DM", "OM", "TM"]:
        raise ContractError("canonical projection services must be ordered FM, DM, OM, TM")
    projection_hash = _sha256(semantics["projection_sha256"], "result.semantics.projection_sha256")
    if projection_hash != sha256_json(projection):
        raise ContractError("result semantic projection SHA-256 does not match its records")
    if semantics["status"] != "pass":
        raise ContractError("result.semantics.status must be 'pass'")

    _validate_provenance(
        result["provenance"],
        "result.provenance",
        implementation=implementation,
        workload=workload,
        run_id=run_id,
        claim_grade=claim_grade,
    )

    metrics = _sequence(result["metrics"], "result.metrics")
    if not metrics:
        raise ContractError("result.metrics must not be empty")
    metric_keys: set[tuple[str, str]] = set()
    for index, item in enumerate(metrics):
        path = f"result.metrics[{index}]"
        metric = _mapping(item, path)
        _exact_keys(
            metric,
            path,
            {"name", "unit", "direction", "sample_scope", "dimensions", "samples"},
            set(),
        )
        name = _string(metric["name"], f"{path}.name")
        if not _METRIC_NAME.fullmatch(name):
            raise ContractError(f"{path}.name has unsupported characters")
        if metric["unit"] != "ns":
            raise ContractError(f"{path}.unit must be 'ns'")
        if metric["direction"] not in ("lower", "higher"):
            raise ContractError(f"{path}.direction must be 'lower' or 'higher'")
        _string(metric["sample_scope"], f"{path}.sample_scope")
        dimensions = _mapping(metric["dimensions"], f"{path}.dimensions")
        for dimension, dimension_value in dimensions.items():
            _string(dimension, f"{path}.dimensions key")
            if isinstance(dimension_value, (dict, list)):
                raise ContractError(f"{path}.dimensions.{dimension} must be a scalar")
        key = name, canonical_json(dimensions)
        if key in metric_keys:
            raise ContractError(f"{path} duplicates metric {name!r} with the same dimensions")
        metric_keys.add(key)
        samples = _sequence(metric["samples"], f"{path}.samples")
        if not samples:
            raise ContractError(f"{path}.samples must not be empty")
        for sample_index, sample in enumerate(samples):
            _integer(sample, f"{path}.samples[{sample_index}]")

    accounting = _mapping(result["accounting"], "result.accounting")
    fields = {
        "expected_fanout",
        "delivered",
        "explicitly_rejected",
        "dropped",
        "duplicates",
        "invalid",
    }
    _exact_keys(accounting, "result.accounting", fields, set())
    counts = {name: _integer(accounting[name], f"result.accounting.{name}") for name in fields}
    accounted = counts["delivered"] + counts["explicitly_rejected"] + counts["dropped"]
    if accounted != counts["expected_fanout"]:
        raise ContractError(
            "incomplete accounting: delivered + explicitly_rejected + dropped must equal "
            "expected_fanout"
        )
    required_fanout = 2 * int(workload["count"])
    if counts["expected_fanout"] != required_fanout or counts["delivered"] != required_fanout:
        raise ContractError("result accounting must have expected_fanout = delivered = 2 * count")
    if any(counts[name] for name in ("explicitly_rejected", "dropped", "duplicates", "invalid")):
        raise ContractError("fair result reported rejected, dropped, duplicate, or invalid events")
    return dict(result)


def semantics_identity(result: Mapping[str, Any]) -> str:
    semantics = cast(Mapping[str, Any], result["semantics"])
    return canonical_json(
        {
            "normalization": semantics["normalization"],
            "canonical_projection": semantics["canonical_projection"],
            "projection_sha256": semantics["projection_sha256"],
        }
    )


def metric_identity(metric: Mapping[str, Any]) -> str:
    return canonical_json(
        {
            "name": metric["name"],
            "unit": metric["unit"],
            "direction": metric["direction"],
            "sample_scope": metric["sample_scope"],
            "dimensions": metric["dimensions"],
        }
    )


def validate_manifest(value: object, *, require_complete: bool = True) -> dict[str, Any]:
    manifest = _mapping(value, "manifest")
    required = {
        "schema",
        "session_id",
        "state",
        "created_at",
        "finished_at",
        "workload",
        "schedule",
        "orchestrator_provenance",
        "runs",
    }
    _exact_keys(manifest, "manifest", required, {"analysis_path", "failure"})
    if manifest["schema"] != MANIFEST_SCHEMA:
        raise ContractError(f"manifest.schema must be {MANIFEST_SCHEMA!r}")
    _string(manifest["session_id"], "manifest.session_id")
    state = _string(manifest["state"], "manifest.state")
    if state not in ("running", "complete", "failed"):
        raise ContractError("manifest.state is unsupported")
    if require_complete and state != "complete":
        raise ContractError("manifest is not complete")
    _string(manifest["created_at"], "manifest.created_at")
    if state != "running":
        _string(manifest["finished_at"], "manifest.finished_at")
    validate_workload(manifest["workload"], "manifest.workload")

    schedule = _mapping(manifest["schedule"], "manifest.schedule")
    _exact_keys(
        schedule,
        "manifest.schedule",
        {"warmup_pairs", "measured_pairs", "order_seed", "pairs"},
        set(),
    )
    warmup_pairs = _integer(schedule["warmup_pairs"], "manifest.schedule.warmup_pairs")
    measured_pairs = _integer(
        schedule["measured_pairs"], "manifest.schedule.measured_pairs", minimum=1
    )
    _integer(schedule["order_seed"], "manifest.schedule.order_seed")
    pairs = _sequence(schedule["pairs"], "manifest.schedule.pairs")
    if len(pairs) != warmup_pairs + measured_pairs:
        raise ContractError("manifest.schedule.pairs count does not match warmup + measured pairs")
    pair_orders: dict[tuple[str, int], str] = {}
    expected_run_sequence: list[tuple[str, int, int, str]] = []
    measured_orders: list[str] = []
    for index, item in enumerate(pairs):
        path = f"manifest.schedule.pairs[{index}]"
        pair = _mapping(item, path)
        _exact_keys(pair, path, {"phase", "pair_index", "order"}, set())
        phase = pair["phase"]
        if phase not in ("warmup", "measured"):
            raise ContractError(f"{path}.phase must be 'warmup' or 'measured'")
        pair_index = _integer(pair["pair_index"], f"{path}.pair_index", minimum=1)
        order = pair["order"]
        if order not in ORDERS:
            raise ContractError(f"{path}.order must be AB or BA")
        key = str(phase), pair_index
        if key in pair_orders:
            raise ContractError(f"duplicate scheduled pair {key}")
        pair_orders[key] = str(order)
        implementations = ("pitch", "go") if order == "AB" else ("go", "pitch")
        expected_run_sequence.extend(
            (str(phase), pair_index, slot, implementation)
            for slot, implementation in enumerate(implementations, start=1)
        )
        if phase == "measured":
            measured_orders.append(str(order))
    if abs(measured_orders.count("AB") - measured_orders.count("BA")) > 1:
        raise ContractError("measured AB/BA schedule is not balanced")

    _mapping(manifest["orchestrator_provenance"], "manifest.orchestrator_provenance")
    runs = _sequence(manifest["runs"], "manifest.runs")
    if require_complete and len(runs) != 2 * (warmup_pairs + measured_pairs):
        raise ContractError("manifest does not contain exactly two runs per pair")
    seen: set[tuple[str, int, int]] = set()
    for index, item in enumerate(runs):
        path = f"manifest.runs[{index}]"
        run = _mapping(item, path)
        required_run = {
            "global_index",
            "phase",
            "pair_index",
            "slot",
            "order",
            "implementation",
            "run_id",
            "output_directory",
            "result_path",
            "command",
            "started_at",
            "finished_at",
            "duration_ns",
            "exit_code",
            "status",
            "result_sha256",
        }
        _exact_keys(run, path, required_run, {"error"})
        global_index = _integer(run["global_index"], f"{path}.global_index", minimum=1)
        if global_index != index + 1:
            raise ContractError(f"{path}.global_index does not match manifest run order")
        phase = str(run["phase"])
        pair_index = _integer(run["pair_index"], f"{path}.pair_index", minimum=1)
        slot = _integer(run["slot"], f"{path}.slot", minimum=1)
        if slot not in (1, 2):
            raise ContractError(f"{path}.slot must be 1 or 2")
        key = phase, pair_index, slot
        if key in seen:
            raise ContractError(f"duplicate run slot {key}")
        seen.add(key)
        order = str(run["order"])
        if pair_orders.get((phase, pair_index)) != order:
            raise ContractError(f"{path}.order does not match schedule")
        expected_implementation = ("pitch", "go") if order == "AB" else ("go", "pitch")
        if run["implementation"] != expected_implementation[slot - 1]:
            raise ContractError(f"{path}.implementation does not match AB/BA slot")
        actual_sequence = phase, pair_index, slot, str(run["implementation"])
        if index >= len(expected_run_sequence) or actual_sequence != expected_run_sequence[index]:
            raise ContractError(f"{path} does not follow the scheduled pair/slot order")
        _string(run["run_id"], f"{path}.run_id")
        _relative_path(run["output_directory"], f"{path}.output_directory")
        _relative_path(run["result_path"], f"{path}.result_path")
        command = _mapping(run["command"], f"{path}.command")
        _exact_keys(
            command,
            f"{path}.command",
            {"executable", "executable_sha256", "argv", "working_directory", "environment"},
            set(),
        )
        _string(command["executable"], f"{path}.command.executable")
        executable_hash = command["executable_sha256"]
        if executable_hash != "unavailable":
            _sha256(executable_hash, f"{path}.command.executable_sha256")
        _sequence(command["argv"], f"{path}.command.argv")
        _string(command["working_directory"], f"{path}.command.working_directory")
        _mapping(command["environment"], f"{path}.command.environment")
        _string(run["started_at"], f"{path}.started_at")
        if run["status"] == "success":
            _string(run["finished_at"], f"{path}.finished_at")
            _integer(run["duration_ns"], f"{path}.duration_ns")
            if run["exit_code"] != 0:
                raise ContractError(f"{path}.exit_code must be zero for success")
            _sha256(run["result_sha256"], f"{path}.result_sha256")
        elif require_complete:
            raise ContractError(f"{path}.status is not success")
    return dict(manifest)


__all__ = [
    "ANALYSIS_SCHEMA",
    "CONFIG_SCHEMA",
    "ContractError",
    "IMPLEMENTATIONS",
    "MANIFEST_SCHEMA",
    "RESULT_SCHEMA",
    "STATISTICS",
    "WORKLOAD_SCHEMA",
    "canonical_json",
    "load_json",
    "loads_json_object",
    "metric_identity",
    "semantics_identity",
    "sha256_file",
    "sha256_json",
    "validate_config",
    "validate_manifest",
    "validate_result",
    "validate_workload",
]
