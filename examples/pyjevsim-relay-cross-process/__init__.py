"""Cross-process variant of ``examples/pyjevsim-relay``.

The pipeline shape (Generator -> Buffer -> Processor) is the same as the
in-process relay; what changes is *where the code runs*. In this
example:

  - ``rtid`` runs as a real subprocess with a real gRPC listener.
  - Each of the 3 federates runs as its own Python subprocess
    (``python3 -m examples.pyjevsim-relay-cross-process.<federate>``).
  - The federates dial ``grpc://127.0.0.1:<port>`` (port assigned at
    runtime) and exchange interactions through the real RTI -- the
    runner does no in-process fan-out tricks.

This is the deployment shape that ships to production. The in-process
example is faster to iterate on; this one is closer to what the
operator's runbook will look like.
"""
