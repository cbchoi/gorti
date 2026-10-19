"""Production local-notification codec exchange with the central runtime adapter."""

import argparse
import copy
import hashlib
from pathlib import Path

from rti1516e._callbacks import ServiceCallback
from rti1516e._local_notifications import prepare, to_row
from rti1516e._runtime_checkpoint import FORMAT, decode, encode, time_dict
from rti1516e._services import FederationExecutionInformation
from rti1516e.savepoint import (
    FederateRestoreStatus,
    FederateSaveStatus,
    RestoreState,
    RestoreStatus,
    RestoreStatusResponse,
    SaveState,
    SaveStatus,
    SaveStatusResponse,
)


def events():
    live = (7 << 32) | 8
    return [
        ServiceCallback("reportFederationExecutions", ((FederationExecutionInformation(
            "other-execution", 2, 3, 9007199254740993, "HLAinteger64Time"),),)),
        ServiceCallback("confirmAttributeTransportationTypeChange", (17, [2, 3], 2)),
        ServiceCallback("reportAttributeTransportationType", (17, 2, 1)),
        ServiceCallback("confirmInteractionTransportationTypeChange", (19, 2)),
        ServiceCallback("reportInteractionTransportationType", (live, 19, 1)),
        SaveStatusResponse(SaveState.IDLE, "historical-save",
                           (FederateSaveStatus(live, SaveStatus.NO_SAVE_IN_PROGRESS),)),
        RestoreStatusResponse(RestoreState.COMPLETED, "historical-restore",
                              (FederateRestoreStatus(live, 9007199254740993,
                                                    RestoreStatus.NO_RESTORE_IN_PROGRESS),)),
        ServiceCallback("reportAttributeTransportationType", (17, 2, 1)),
    ]


def emit(directory):
    directory.mkdir(parents=True, exist_ok=True)
    for family, time in (("HLAfloat64Time", 2.5), ("HLAinteger64Time", 9007199254740993)):
        state = {
            "format": FORMAT, "version": 1, "federation_generation": 7,
            "federate_handle": (7 << 32) | 8, "callback_epoch": 2,
            "logical_time_implementation_name": family, "logical_time": time_dict(time, family),
            "object_knowledge": {"latest": [], "retired": []},
            "next_retraction_handle": None, "used_retraction_handles": [],
            "pending_callbacks": [], "callback_recovery": "server-v2", "pending_outcomes": [],
            "pending_local_notifications": [to_row(prepare(event, index))
                                            for index, event in enumerate(events(), 1)],
        }
        payload = encode(state)
        path = directory / f"python-local-{family}.json"
        path.write_bytes(payload)
        inspect(path, 7, (7 << 32) | 8, path)
        print(f"{path}: {len(payload)} bytes SHA256 {hashlib.sha256(payload).hexdigest()}")


def inspect(path, generation, handle, original):
    for family in ("HLAfloat64Time", "HLAinteger64Time"):
        try:
            state = decode(path.read_bytes(), family)
            break
        except ValueError:
            if family == "HLAinteger64Time":
                raise
    source = decode(original.read_bytes(), family)
    assert state["federation_generation"] == generation
    assert state["federate_handle"] == handle
    expected = copy.deepcopy(source["pending_local_notifications"])
    for row in expected:
        payload = row["payload"]
        if row["kind"] == "reportInteractionTransportationType":
            payload["federate_handle"] = handle
        elif row["kind"] == "federationSaveStatusResponse":
            for value in payload["statuses"]:
                value["federate_handle"] = handle
        elif row["kind"] == "federationRestoreStatusResponse":
            for value in payload["statuses"]:
                value["pre_restore_handle"] = handle
    assert state["pending_local_notifications"] == expected
    assert len(state["decoded_local_notifications"]) == 8
    assert expected[2]["payload"] == expected[7]["payload"]
    assert expected[2]["local_id"] != expected[7]["local_id"]
    assert expected[6]["payload"]["statuses"][0]["post_restore_handle"] == 9007199254740993
    assert expected[0]["payload"]["executions"][0]["federation_generation"] == 9007199254740993
    assert state["decoded_time"] == (2.5 if family == "HLAfloat64Time" else 9007199254740993)
    print(f"{family}: 8 local rows verified, generation={generation} self={handle}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("emit", "inspect"))
    parser.add_argument("path", type=Path)
    parser.add_argument("--original", type=Path)
    parser.add_argument("--generation", type=int, default=9)
    parser.add_argument("--handle", type=int, default=(9 << 32) | 41)
    args = parser.parse_args()
    if args.action == "emit":
        emit(args.path)
    else:
        inspect(args.path, args.generation, args.handle, args.original)
