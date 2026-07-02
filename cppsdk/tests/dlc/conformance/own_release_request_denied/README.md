# own_release_request_denied

Owner refuses release via `attributeOwnershipReleaseDenied(object, attrs)`
(§7.12). Two-federate scenario:

1. **Alice** owns `car-1.Position`.
2. **Bob** calls `attributeOwnershipAcquisition(object, attrs, tag)` (§7.8 with
   mandatory `tag` per divergence catalogue row 12.2).
3. RTI fires `requestAttributeOwnershipRelease(object, attrs, tag)` on
   Alice (§7.11, catalogue row 4.30 — gorti M17 absent).
4. Alice calls `attributeOwnershipReleaseDenied(object, attrs)` (§7.12,
   catalogue row 12.4 — gorti M17 absent).
5. Bob does NOT receive `attributeOwnershipAcquisitionNotification`;
   ownership stays with Alice.

M31 dispatch plan §2.2 fixture #17.

## Spec citations per event in goldens

Per TASK-362 traceability lint:

### Alice

- `ALICE: CONNECT` — §4.2 connect
- `ALICE: JOIN` — §4.9 joinFederationExecution
- `ALICE: REGISTER` — §6.8 registerObjectInstance
- `ALICE: RELEASE_REQUEST` — §7.11 requestAttributeOwnershipRelease
- `ALICE: RELEASE_DENIED` — §7.12 attributeOwnershipReleaseDenied
- `ALICE: RESIGN` — §4.10 resignFederationExecution

### Bob

- `BOB: CONNECT` — §4.2 connect
- `BOB: JOIN` — §4.9 joinFederationExecution
- `BOB: DISCOVER` — §6.9 discoverObjectInstance
- `BOB: OWNERSHIP_ACQUISITION` — §7.8 attributeOwnershipAcquisition
- `BOB: RESIGN` — §4.10 resignFederationExecution

The absence of an `ACQUISITION_NOTIFICATION` line in `expected.bob.log`
is itself the assertion that ownership transfer was refused.

## M31 status

RED. `TBD-pitch-capture` until Agent E TASK-363 clears.

## gorti parity status (M35, parity-CD)

Verdict: **PARTIAL 10/11** (goldens are spec-derived; Pitch capture
still pending). Alice PARTIAL 5/6, Bob FULL 5/5. Captured run:
`gorti-captured.{alice,bob}.log`.

Missing event (exactly one): `ALICE: RELEASE_REQUEST attrs=1`.

gorti never fires §7.11 requestAttributeOwnershipRelease: the
FederateEvent oneof (proto/rti/v1/stream.proto) carries only three
ownership events — `ownership_assumption` (§7.5),
`ownership_acquired` (§7.7), `ownership_divest_confirmed` — and has
no RequestAttributeOwnershipRelease slot (catalogue row 4.30). When
Bob's §7.8 acquisition targets an attribute Alice still owns, the
server queues it as a pending acquire (PendingAcquireEntry) and emits
nothing to the owner. Alice therefore takes the fixture's tolerated
timeout path: her §7.12 `attributeOwnershipReleaseDenied` (a
documented DLC no-op, parity-CA) is invoked with a never-populated
handle and still logs `RELEASE_DENIED`.

The negative assertion holds under gorti: Bob receives no §7.7
acquisitionNotification (his evoke-drain wait would have surfaced it
as `BOB: UNEXPECTED_ACQUISITION_NOTIFICATION`), and no such line
appears in the capture — ownership stays with Alice for the wrong
reason (transfer never initiated) rather than the spec reason
(owner denied).

Missing impl: (1) server event RequestAttributeOwnershipRelease when
an acquire targets owned attributes, (2) a real
attributeOwnershipReleaseDenied RPC to resolve the pending acquire,
(3) bridge conversion for the §7.11 callback. Both federate wait
loops use the evoke-drain pattern, so the event will be captured as
soon as the impl lands.
