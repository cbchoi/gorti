"""Specification tests for milestone M12 — Python
SDK exposure for cut-2 service groups (sync, ownership, MOM, DDM,
savepoint).

See docs/srs.md §10.4 (cut 3) for the milestone gate.

Cut-2 implemented these as Go internal packages reachable via gRPC.
M12's Python side: extend the SDK's Federate class (or add new client
classes alongside) to invoke the new RPCs, mirroring the cut-1 pattern
where Federate exposes publish/subscribe/update/send_interaction.

Spec test scope: each new service has at least one round-trip test
that uses a real GrpcTransport against a running rtid binary (mirrors
the M5 cross-language smoke pattern).

The suite exercises the supported Python surface for each service group.
"""
