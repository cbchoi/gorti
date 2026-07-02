# own_acquire_if_available_race

Two federates simultaneously call `attributeOwnershipAcquisitionIfAvailable`
(§7.9) on the same attribute of a carrier-owned object. Exactly one
succeeds (gets §7.7 `attributeOwnershipAcquisitionNotification`); the
other gets §7.10 `attributeOwnershipUnavailable`.

M31 dispatch plan §2.2 fixture #16. Enforces divergence catalogue:
- Row 12.3 BLOCKING — `attributeOwnershipAcquisitionIfAvailable` is
  spec-mandated but absent from gorti M17.
- Row 4.29 MAJOR — `attributeOwnershipUnavailable` callback absent
  from gorti M17.

Three federates:
- `carrier` registers `car-race`, then §7.2-divests Position and stays
  joined (parity-CD fix: the M31 carrier held Position throughout, but
  §7.9 acquisitionIfAvailable only acquires UNOWNED attributes and
  never solicits release from an owner — with the carrier owning
  Position, the bob-wins golden was unreachable under any compliant
  RTI; golden updated accordingly, spec-wins).
- `bob` (racer) calls `acquireIfAvailable`.
- `carol` (racer) calls `acquireIfAvailable` at the same moment.

The race winner is non-deterministic; the goldens capture the
"bob-wins / carol-loses" branch. If Pitch reports the other branch,
the goldens should be swapped at TASK-363 capture time.

## Spec citations per event in goldens

Per TASK-362 traceability lint:

### Carrier

- `CARRIER: CONNECT` — §4.2 connect
- `CARRIER: JOIN` — §4.9 joinFederationExecution
- `CARRIER: REGISTER` — §6.8 registerObjectInstance
- `CARRIER: UNCONDITIONAL_DIVEST` — §7.2 unconditionalAttributeOwnershipDivestiture
- `CARRIER: RESIGN` — §4.10 resignFederationExecution

### Bob (winner)

- `BOB: CONNECT` — §4.2 connect
- `BOB: JOIN` — §4.9 joinFederationExecution
- `BOB: DISCOVER` — §6.9 discoverObjectInstance
- `BOB: ACQUIRE_IF_AVAILABLE` — §7.9 attributeOwnershipAcquisitionIfAvailable
- `BOB: ACQUISITION_NOTIFICATION` — §7.7 attributeOwnershipAcquisitionNotification
- `BOB: RESIGN` — §4.10 resignFederationExecution

### Carol (loser)

- `CAROL: CONNECT` — §4.2 connect
- `CAROL: JOIN` — §4.9 joinFederationExecution
- `CAROL: DISCOVER` — §6.9 discoverObjectInstance
- `CAROL: ACQUIRE_IF_AVAILABLE` — §7.9 attributeOwnershipAcquisitionIfAvailable
- `CAROL: OWNERSHIP_UNAVAILABLE` — §7.10 attributeOwnershipUnavailable
- `CAROL: RESIGN` — §4.10 resignFederationExecution

## M31 status

RED. Links fail with undefined reference to `rti1516e::*`. Goldens are
`TBD-pitch-capture` placeholders until Agent E's TASK-363 clears.

## gorti parity status (M36, agent-DC)

Carrier **FULL 5/5**; bob **FULL 6/6**; carol **PARTIAL 5/6 strict,
6/6 by content** (one adjacent line swap). Captured run:
`gorti-captured.{carrier,bob,carol}.log` (canonicalized), against the
M36-DC rtid — the golden's bob-wins / carol-§7.10 branch.

Carol's single strict miss: `OWNERSHIP_UNAVAILABLE` prints BEFORE her
`ACQUIRE_IF_AVAILABLE` line. The DLC emulates §7.9 by query-then-
acquire and synthesizes the §7.10 callback synchronously INSIDE
`attributeOwnershipAcquisitionIfAvailable`
(cppsdk/src/dlc/RTIambassadorImpl.cpp:960-985), so the callback
precedes the fixture's own post-call print. Client-side artifact
(DA-owned if the synthesized callback is to be deferred to the next
evoke).

### The §7.9 race hole — M36 DC-5 findings

Re-verified: the emulation's 1-in-5 hole is REAL and, with both racers
woken by the same DISCOVER stream push, near-deterministic — the
loser's query lands before the winner's grant, its acquire is queued
as a plain §7.4 pending acquire, and it receives NEITHER §7.7 NOR
§7.10 (observed 6/6 unskewed runs; this capture used OS-level
scheduling skew — SIGSTOP the losing racer across the winner's grant —
to record the golden branch). Worse, the stale pending acquire can
fire LATER: when bob resigns CANCEL_THEN_DELETE_THEN_DIVEST, Position
goes unowned and a queued carol gets an ACQUISITION_NOTIFICATION she
only ever requested "if available" (observed live during M36 capture
runs).

Server-side residual (documented in
rti/internal/ownership/manager.go Acquire): a clean closure needs an
if-available semantic flag on the AcquireRequest proto so the server
can answer deny-fast instead of queueing — proto changes are out of
M36-DC scope. Rejecting already-owned acquires without the flag would
break legitimate §7.4 callers and throw through the DLC emulation.
What M36 DC-5 DID land: Acquire is now atomic
(classify-then-mutate — a duplicate-pending rejection can no longer
leave partially granted, unnotified ownership behind). The §7.6
ConfirmDivestiture gate (needs a new RPC) is likewise out of scope —
noted as residual.
