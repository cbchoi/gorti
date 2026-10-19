# Time management

## Selecting the time representation

Federation creation accepts `logical_time_implementation_name`. Omitting it or
selecting `HLAfloat64Time` creates a Float64 federation. The selection is fixed
for that execution and appears in create, list and join results, the federation
roster, MOM's `HLAtimeImplementationName`, and new save manifests.

Go uses `FederationSpec.LogicalTimeImplementationName`; Python's standard facade
accepts `logical_time_implementation_name` when creating the federation. Java
uses the logical-time implementation argument of `createFederationExecution`
and exposes the joined factory through `getTimeFactory()`.

The single-server gRPC backend also accepts `HLAinteger64Time`. The CLI enables
it by default; `--enable-integer64=false` disables explicit Integer64 selection.
Float64 and Integer64 federations can coexist in one server. Custom factories,
DDS, clustered execution, and custom Float64 time strategies do not provide an
Integer64 execution path. An absent legacy response or save manifest field means
Float64; an unrecognized nonempty name does not.

Restore checks the saved representation against the target execution before
applying state. Integer64 requests, queries and callbacks use the selected exact
representation throughout the server.

### Exact typed foundation

The typed time, object, delivery and save/restore domains retain nonnegative
Integer64 values directly, including adjacent times above `2^53`. Finite final
time is `MaxInt64`, distinct from an unbounded horizon; arithmetic overflow is
checked rather than rounded. The additive `LogicalTimeValue` wire oneof carries
an explicit integer selector, including zero. Integer requests leave required
legacy double fields at positive zero and omit optional legacy double fields.
Conflicting or wrong-representation inputs are rejected before mutation.

Typed unary adapters cover OM, TM, timed regional interactions and savepoints,
with exact callback encoding. Integer64 federates use unary service requests and
the shared callback stream. Float64-only interaction, ConfirmedObject and
LocalLRC optimized request streams reject Integer64 input; SDKs must use the
unary path rather than convert times to Float64.

## Logical time and lookahead

A time-regulating federate promises not to send a timestamp-order message
earlier than its current logical time plus lookahead. For example, at logical
time 3 with lookahead 0.5, the earliest valid timestamp is 3.5. A timestamp of
3 is rejected as an invalid logical time.

During ordinary advances, time-constrained federates receive timestamp-order
callbacks in logical-time order before the corresponding grant. FQR explicitly
permits speculative delivery, as described below.

While an advance request is pending, a regulating sender's timestamp floor uses
the requested time plus the lookahead projected at that time. After a grant, it
uses the granted time plus actual lookahead. Comparisons use the represented
logical-time values; a timestamp below the floor is not accepted within a
floating-point tolerance.

Positive lookahead must remain distinguishable even when ordinary Float64
addition rounds back to the current time. At time `9007199254740992` (`2^53`)
with lookahead `1`, the first valid timestamp is `9007199254740994`, the next
representable Float64 value. LBTS, GALT, send validation, retraction and regulation
activation use the same arithmetic. Applications must not assume that ordinary
language-level addition always produces a valid timestamp at precision limits.

At zero lookahead, TAR and NMR require a timestamp strictly greater than their
pending target or resulting granted time. TARA and NMRA permit equality unless
a TAR/NMR grant already excluded that same time. An Available grant at the same
time does not erase that history; a later Available grant or re-enabling
regulation resets it. Positive lookahead retains an inclusive timestamp floor.
These rules describe finite values in the current float64 time representation;
the product's infinity sentinel is retained for compatibility.

`modifyLookahead()` is rejected while an advance request is pending. An increase
takes effect immediately. A decrease takes effect gradually as logical time
advances: `actual = max(requested, previous_actual - time_advanced)`.
`queryLookahead()` returns the actual value, not the requested target. For
example, reducing 5 to 2 leaves the actual value at 5 initially; grants advancing
time by 1 then reduce it to 4, 3 and 2. A same-time grant does not reduce it.
The implementation retains the reduction's starting time and interval, so small
advances accumulate instead of repeatedly rounding away from the interval.

## Advance requests

A joined federate may request a finite TAR, TARA, NMR, NMRA or FQR without
enabling either time regulation or time constraint. For an unconstrained
requester, other regulators' GALT does not limit the grant, and incoming messages
do not select an earlier next-message target. Normal target validation,
single-pending-request and scheduled-save rules still apply. Membership is
checked at the production service boundary, independently of the time-mode flags.

A constrained federate in the time-granted state holds incoming TSO even when
the timestamp equals its current logical time and asynchronous delivery is
enabled. Advance evaluation releases the required messages before TAG. NMR and
NMRA include a queued message at current time when selecting the next-message
target, so the next grant can equal the preceding grant.
Current subscriptions are checked at delivery. If an eligible NMR/NMRA timestamp
group is entirely discarded, the next target and its safety are reevaluated;
an undelivered message does not cause an empty early grant.

## Flush queue requests

`flushQueueRequest(T)` releases all queued applicable RO and TSO, including
timestamps beyond `T` or GALT. It then grants the minimum of `T`, applicable
GALT and the earliest TSO delivered for the request. A same-time grant is valid;
FQR does not wait for a stationary peer. Unconstrained requesters are not bound
by peer GALT. Partially delivered requests retain their delivered minimum across
retries and save/restore.

Flushed TSO retains timestamp-order callback metadata, but a later message can
have a lower timestamp than a previously flushed message. Applications must also
handle `requestRetraction` for flushed messages still eligible for retraction.

## Safe mode activation

Enabling regulation or constraint submits a request. Completion arrives through
`timeRegulationEnabled(time)` or `timeConstrainedEnabled(time)`, carrying the
safe activation time. Go and Python receive actual server callbacks; unary RPC
success alone is not completion. An enable can remain pending while peers or
callback capacity prevent activation. Advances reject pending enable requests.

Regulation activation respects constrained peers' time guarantees. If the
requester is constrained, eligible messages precede its regulation-enabled
callback. Activation never resets a federate to an earlier logical time.
It selects the smallest representable safe time, not a rounded subtraction of
lookahead from a peer's time. For an inclusive peer frontier of `10` and lookahead
`2`, this can be the Float64 value immediately below `8`, whose sum with `2`
rounds to `10`.

The Go `rti/pkg/logicaltime` package provides exact Integer64 and standardized
Float64 value arithmetic and encoding. The root selects the appropriate typed
manager for each federation without converting Integer64 values to Float64.

## Retraction

Only the original, currently regulating sender can retract a valid TSO
designator. The timestamp must be strictly greater than current time plus
actual lookahead, or requested time plus actual current lookahead while an
advance is pending. Equality is too late. This is not the projected-lookahead
calculation used to validate a future outgoing send.

Queued copies are removed; recipients of already delivered copies receive
`requestRetraction`, including FQR and unconstrained RO recipients. Retraction
of a mixed-order update affects only its sent-TSO attributes. A legal deletion
retraction restores the object and its owners from deletion invocation.

A removal received in RO immediately removes the recipient's object knowledge.
A speculative TSO removal retains that knowledge until the recipient reaches its
timestamp or disables constraint. Legal retraction restores the removed
knowledge without requiring another discovery callback. Save/restore retains
these per-recipient boundaries as well as the retraction ledger.

## Preferred message order

FOM Receive and TimeStamp preferences apply in every federation profile.
Explicit attribute-instance or publisher interaction-class order changes take
precedence over the FOM. A timestamped update containing mixed-order attributes
is partitioned by effective order and transportation; an RO attribute does not
become TSO merely because another attribute in the update is TSO. Timestamped
deletion uses the effective order of `HLAprivilegeToDeleteObject`.

An untimestamped send remains RO even when TimeStamp is preferred. Conversely,
an original timestamp on an RO-classified callback is retained without becoming
its delivery bound. Sent order and received order remain distinct: an
unconstrained recipient receives a timestamp-order send in RO. Embedded models
without order metadata, or without the deletion-privilege attribute, retain the
existing timestamp-order fallback for timestamped sends.

## Receive-order delivery

The server holds receive-order (RO) reflections, interactions and object removals
in a separate per-federate queue when the receiver is time-constrained, has no
pending advance, and has HLA asynchronous delivery disabled. They become eligible
when any of these conditions holds:

- the receiver is not time-constrained;
- the receiver is advancing; or
- HLA asynchronous delivery is enabled.

An advance request releases queued RO messages even when a regulating peer still
prevents the grant. TAR, TARA, NMR, NMRA and FQR deliver the queued RO messages
that remain applicable before Time Advance Grant (TAG). A timestamp supplied
with an RO message is retained in its callback; it does not make that message
TSO or postpone it until that logical time. Discovery, synchronization,
ownership and save/restore control callbacks are not held by this RO gate.

Before releasing a queued reflection or interaction, the server rechecks current
subscriptions and trims attributes or suppresses the message as appropriate.
Removal still follows prior discovery, including after unsubscription. This
recheck does not implement the complete Delay Subscription Evaluation Switch:
it does not retain messages for receivers excluded by send-time subscriptions.

### Asynchronous delivery

Python's `async`/`await` API and HLA asynchronous delivery are different settings.
`enableAsynchronousDelivery()` does not disable timestamp ordering: a constrained
receiver still waits for advance evaluation before receiving same-current-time
or future TSO messages.
Queued TSO callbacks retain their timestamp-order metadata and precede the grant.

Regulation restricts outgoing timestamps; it does not constrain incoming
messages. A regulating-only receiver receives incoming timestamped messages in
receive order. Disabling time constraint releases its queued messages in receive
order, subject to any scheduled-save boundary.

Enabling HLA asynchronous delivery or disabling time constraint can succeed while
transport capacity still prevents the entire backlog from leaving the server.
The setting has taken effect; the server continues draining in the background.
For other drain errors, the setting is rolled back and unsent messages remain
queued. Disabling asynchronous delivery does not recall callbacks already
released to the stream.

A backlog larger than transport capacity drains in bounded prefixes. The server
retains the unsent suffix and retries without repeating successful deliveries.
A failed grant release retains the pending advance and withholds TAG until the
required messages are released. Retrying the same advance is not the recovery
mechanism: a second request can be rejected as already pending, while the
server-owned retry worker resumes the accepted request. Successful admission
or release to a stream does not mean the SDK has invoked the user callback.

MOM queue lengths combine waiting time-domain messages with committed data
callbacks in ready, transport, and SDK stages until authenticated callback
entry. `HLAROlength` and `HLATSOlength` use received order, so transfer out of
the time queue does not reduce the total by itself. Control callbacks and
tentative reservations are excluded. In asynchronous reporting builds, the
server observes entry when the report arrives; a report drain establishes a
settled observation point.

The [verification guide](verification.md) links the service tests and their
recorded executions, including queue conservation and callback accounting.

## Time queries

`queryLITS()` considers both GALT and the incoming TSO queue. An empty queue does
not imply an undefined result when another regulator provides a finite GALT.
The separate RO queue does not contribute a timestamp bound to GALT or LITS.

GALT reflects pending requests as well as granted time. For example, when the
only other regulator has a pending TAR to 10 and effective lookahead 2, its
contribution is 12 rather than its old logical time plus 2. A next-message
request can complete earlier, so queued messages and possible early grants
must also be considered. The querying federate does not directly bound itself;
with no other regulator, GALT is undefined.

## Save and restore

Save/restore retains pending RO messages, their original optional timestamps,
payloads, binary tags and callback metadata alongside time mode, advance state,
pending lookahead decrease and strict grant history. Restore replaces the live
queues with the saved state; it does not append the saved messages to callbacks
from after the save. Delivery resumes after the restored identity and callback
epoch are installed at the terminal restore boundary, using the restored RO
eligibility state.

Time snapshots also retain logical time for a federate that advanced with
neither time mode enabled, and queued same-current-time TSO. Restoring that state
does not itself deliver a message or grant. Persistence and production membership
guards have separate verification cases; neither is established by advance-mode
tests alone.

The time-state component uses version 5 for pending activation, receive-bound
history or a partially completed FQR. Older states use version 4 when an RO queue
is present. Without an
RO queue it uses version 3 for strict history, version 2 for a pending decrease
without that history, or version 1 otherwise. All five Float64 versions are readable. This is a
component version, not a change to the save-bundle manifest or public RPC schema.
Older snapshots cannot recover queues or history they never stored; a reader
that does not support a newer version must reject it rather than discard state.

Typed Integer64 snapshots use time v6, object v5 and retraction v3, with the
`HLAinteger64Time` discriminator and exact decimal integer decoding. MOM's
exact-value extension and integer save history use version 2; integer histories
have separate versioned storage. Restore checks factory compatibility before
publication and preserves exact queued timestamps, deletion and retraction state.
The selected factory also determines the server's checkpoint manager.

Scheduled saves retain the original advance target and release eligible RO and
required TSO messages before save initiation. An immediate save does not itself
make an idle constrained receiver eligible for RO delivery.

The [RO implementation record](https://github.com/cbchoi/gorti/blob/main/engineering/verification/ieee1516-2010-ro-queue-20260922.md)
documents the original queue and persistence design. Later callback and SDK
recovery results are linked from [verification](verification.md).

## Transport and subscription options

Go, Python, and Java use actual server callbacks for pending time-mode
activation. Delayed subscription evaluation rechecks queued object and
interaction interest, including DDM routing, when delivery becomes eligible.

Integer64 uses exact unary requests; optimized request streams currently use
Float64. The Float64 `+Inf` compatibility sentinel is distinct from Integer64's
finite final value. Select one factory when creating the federation and use
that representation throughout its lifetime.

## Federation-wide blocking

A constrained federate's time-advance request is evaluated against regulating
members' bounds. In the quickstart, the waiter requests time 5 while the peer
remains at time 0
with lookahead 2. The peer's state constrains the federation lower bound, so
the waiter cannot receive a grant. When the peer also requests time 5, the
lower bound advances and both requests become grantable.

A single-federate run cannot check this condition because there is no peer to
hold back the lower bound. The quickstart therefore uses two federates.

## Example sequence

```text
waiter                         peer
  | join, regulate, constrain    | join, regulate, constrain
  | <--------- ready ----------- |
  | TAR(5)                       | time 0, sleep 3 seconds
  | ... pending ...              |
  |                              | TAR(5)
  | <------ TAG(5) ------------- |
  |                    TAG(5) -> |
```

Run the sequence using the [quickstart](quickstart.md). The example exits after
the grant so automated checks can distinguish success from a hung federation.

## Safety properties

The time-management tests enforce:

- atomic admission across the complete recipient set, including recipients
  whose RO messages must wait;
- final sender timestamp validation for timestamp-order sends held through
  delivery admission, including a send with no recipients or RO reception by an
  unconstrained recipient;
- all still-applicable queued RO and required TSO messages before a grant;
- withholding a grant when delivery fails;
- bounded-prefix release and background retry without duplicate messages;
- RO queue and metadata preservation across save/restore;
- generation checks that prevent callbacks from an old federation instance;
- teardown that drains or cancels pending streams without duplicate grants.
