"""M31 conformance fixture: xlang_python_cpp_pubsub — Python publisher.

Uses gorti's pysdk M28 typed-handle path at ``pysdk/rti1516e/standard.py``.

Spec § anchors:
  §4.2  connect
  §4.9  joinFederationExecution
  §5.2  publishObjectClassAttributes
  §6.8  registerObjectInstance
  §6.10 updateAttributeValues (RO, mandatory tag)
  §4.10 resignFederationExecution

Goal: prove the cppsdk DLC subscriber reads back exactly the same wire
bytes that pysdk emits — verifies the encoding surface is consistent
across the two SDKs against the same rtid.
"""

from __future__ import annotations

import argparse
import sys
import time
from pathlib import Path

# Allow running from anywhere with --pysdk PATH or default repo layout.
_REPO_ROOT = Path(__file__).resolve().parents[5]
sys.path.insert(0, str(_REPO_ROOT / "pysdk"))

from rti1516e.standard import Rti1516eAmbassador  # noqa: E402


class PubFed(Rti1516eAmbassador):
    """Plain ambassador — no overridden callbacks needed for the pub side."""


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="grpc://127.0.0.1:8080")
    parser.add_argument("--fom", default="./federation.fom.xml")
    args = parser.parse_args()

    fed = PubFed()
    # §4.2 — pysdk Layer 2 wraps the connect signature; same wire result.
    fed.connect(fed, args.url)
    print("PUB: CONNECT", flush=True)

    fed_name = "xlang_python_cpp_pubsub"
    try:
        fed.createFederationExecution(fed_name, [args.fom])
    except Exception:
        # idempotent — subscriber may have created first
        pass
    fed.joinFederationExecution("py-pub", fed_name)
    print("PUB: JOIN federate=py-pub", flush=True)

    v_class = fed.getObjectClassHandle("HLAobjectRoot.Vehicle")
    p_attr = fed.getAttributeHandle(v_class, "Position")
    fed.publishObjectClassAttributes(v_class, {p_attr})
    print("PUB: PUBLISH class=Vehicle attributes=[Position]", flush=True)

    fed.reserveObjectInstanceName("car-1")
    # M28 reservation API blocks until the ack lands (Layer 2 sync).
    inst = fed.registerObjectInstance(v_class, "car-1")
    print("PUB: REGISTER class=Vehicle name=car-1", flush=True)

    # §6.10 updateAttributeValues — RO; mandatory tag (catalogue 17.1).
    # pysdk M28 encodes HLAfloat64BE the same way as cppsdk's
    # rti1516e::HLAfloat64BE so the C++ subscriber decodes byte-identical.
    import struct
    for value in (10.0, 20.0, 30.0):
        encoded = struct.pack(">d", value)  # HLAfloat64BE — big-endian double
        fed.updateAttributeValues(inst, {p_attr: encoded}, tag=b"")
        print(f"PUB: UPDATE name=car-1 Position={value:.6f}", flush=True)
        time.sleep(0.05)

    fed.resignFederationExecution("CANCEL_THEN_DELETE_THEN_DIVEST")
    print("PUB: RESIGN action=CANCEL_THEN_DELETE_THEN_DIVEST", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
