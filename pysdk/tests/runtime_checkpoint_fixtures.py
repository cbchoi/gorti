"""Emit production codec fixtures and independently inspect adapter output."""

import argparse
import hashlib
from pathlib import Path

from rti1516e._runtime_checkpoint import FORMAT, decode, encode, time_dict
from rti1516e._transport import _ensure_generated_path


def emit(directory, recovery_v2=False):
    directory.mkdir(parents=True, exist_ok=True)
    for family, logical_time in (("HLAfloat64Time", 2.5),
                                 ("HLAinteger64Time", 9007199254740993)):
        state = {
            "format": FORMAT, "version": 1,
            "federation_generation": 7, "federate_handle": (7 << 32) | 8,
            "logical_time_implementation_name": family,
            "callback_epoch": 9007199254740995,
            "logical_time": time_dict(logical_time, family),
            "object_knowledge": {"latest": [[17, 0], [19, 9007199254740993]],
                                 "retired": [[23, 0]]},
            "next_retraction_handle": 9007199254740994,
            "used_retraction_handles": [9007199254740993, (1 << 64) - 1],
            "pending_callbacks": [], "callback_recovery": "server",
        }
        if recovery_v2:
            state["callback_recovery"] = "server-v2"
            state["pending_outcomes"] = [{"identity": hashlib.sha256(b"s" * 32).hexdigest(),
                                           "success": False, "exception": "ValueError: body"}]
        payload = encode(state)
        path = directory / f"python-{family}.json"
        path.write_bytes(payload)
        inspect(path, 7, (7 << 32) | 8)
        print(f"{path}: {len(payload)} bytes SHA256 {hashlib.sha256(payload).hexdigest()}")


def inspect(path, generation, handle):
    for family in ("HLAfloat64Time", "HLAinteger64Time"):
        try:
            state = decode(path.read_bytes(), family)
            break
        except ValueError:
            if family == "HLAinteger64Time":
                raise
    assert state["federation_generation"] == generation
    assert state["federate_handle"] == handle
    assert state["callback_epoch"] == 9007199254740995
    assert state["object_knowledge"] == {"latest": [[17, 0], [19, 9007199254740993]],
                                         "retired": [[23, 0]]}
    assert state["next_retraction_handle"] == 9007199254740994
    assert state["used_retraction_handles"] == [9007199254740993, (1 << 64) - 1]
    assert state["decoded_callbacks"] == []
    if state["callback_recovery"] == "server-v2":
        assert state["pending_outcomes"] == [{"identity": hashlib.sha256(b"s" * 32).hexdigest(),
                                              "success": False, "exception": "ValueError: body"}]
    expected_time = 2.5 if family == "HLAfloat64Time" else 9007199254740993
    assert type(state["decoded_time"]) is type(expected_time)
    assert state["decoded_time"] == expected_time
    print(f"{family} verified: generation={generation} self={handle} exacttime={expected_time}")


if __name__ == "__main__":
    _ensure_generated_path()
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("emit", "inspect"))
    parser.add_argument("path", type=Path)
    parser.add_argument("--generation", type=int, default=9)
    parser.add_argument("--handle", type=int, default=(9 << 32) | 41)
    parser.add_argument("--recovery-v2", action="store_true")
    arguments = parser.parse_args()
    if arguments.action == "emit":
        emit(arguments.path, arguments.recovery_v2)
    else:
        inspect(arguments.path, arguments.generation, arguments.handle)
