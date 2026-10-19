# Interface Design Description

Document version: 4.6
Status: Current implementation profile
Updated: 2026-10-18

## 1. Interface hierarchy

gorti has five interface boundaries:

1. Go and Python federate APIs and the Java `hla.rti1516e` mapping;
2. Protobuf/gRPC messages and services under `proto/rti/v1/`;
3. server-streamed federate callbacks;
4. persistence-neutral HLA domain ports and typed callback events; and
5. internal adapter and application interfaces used to compose `rtid`.

The current wire package is `rti.v1`. Existing operations use `WIRE_VERSION_V1`.
Explicit MIM selection uses `WIRE_VERSION_V2` for `CreateFederation` and for a
`JoinFederation` carrying a MIM expectation. Other service and callback versions
are unchanged. The `.proto` files and
generated bindings define the exact fields and methods; the descriptor/RPC
golden test records the versioned descriptor and RPC inventory.

Generated Go bindings are isolated in
`rti/internal/adapters/protobuf/rti/v1`. HLA service packages never construct
those messages directly. The gRPC adapter validates wire input, invokes a
domain operation, maps domain errors, and encodes typed callback events. A
descriptor and RPC-surface golden test rejects accidental wire changes.

## 2. Language profiles

The SDK profiles do not expose an identical surface.

| Profile | Current principal surface | Important limits |
|---|---|---|
| Go `rti/pkg/federate` | Join/resign, declarations, OM, TM, ownership, synchronization, DDM, save/restore, MOM, Support, callbacks, confirmed object stream, LocalLRC | Handles are numeric; receiving an event channel value does not report user callback invocation |
| Python asynchronous API | Lifecycle and dedicated service clients, including ownership, save/restore, DDM, MOM, Support, GALT/LITS and retraction | Product-specific asynchronous interface, distinct from the synchronous standard-shaped adapter |
| Python `Rti1516eAmbassador` | Synchronous IEEE-shaped adapter, typed handle wrappers and collections | Typed handles remain integer values without embedded federation provenance |
| Java `hla.rti1516e` | Ambassador, callbacks, ownership, DDM, save/restore, Float64/Integer64 time, and encoding helpers | Evoked callbacks, file-based FOM inputs, application-managed reconnection |

The table is profile-specific; a service or overload listed for one language
is not implied for the others.

Retained C++ code and fixtures are legacy material outside the supported SDK
set. [Verification](../../../docs/verification.md) records executed language
and service selections.

## 3. Identity and lifecycle

Join returns a numeric federate handle and a private session token. Ordinary
service request messages identify a caller by federation name and federate
handle. Session-scoped controls and signed resign additionally authenticate
binary gRPC metadata; they do not put that token in normal request fields.
Handles in these language profiles are
numeric values and do not themselves prove FOM type, federation, or generation.

The server checks current federation membership and performs additional
operation-specific validation where implemented. A caller that can reach the
RPC endpoint is not cryptographically bound to a particular federate handle by
the normal request fields. TLS and optional identity middleware protect the
transport, but this is not a complete per-federate authorization mechanism.

Ownership queries include `QueryOwnershipRequest.federate_handle`. Production
queries reject a missing caller or an object the caller does not know, rather
than returning an unowned result for either case. SDK clients supply the caller;
raw requests that omit it fail validation.

The additive `AttributeOwnershipDivestitureIfWantedWithResult` RPC returns
`DivestIfWantedResponse.divested_attribute_handles`, the exact set committed by
that call. The original empty-response RPC remains available. Confirmation
requests and acquisition callbacks now carry binary tags without reconstructing
them in the language bridge.

XML class identities retain the complete ancestor path. Server class-name
queries return this qualified identity; full and root-omitted paths resolve to
the same handle, and local names are accepted only when unambiguous. Inherited
attributes and parameters follow the same path in both lookup directions.
Repeated module containers are coalesced before initial handle assignment;
conflicting definitions or duplicate siblings produce `FOM-014`. Additional
join modules append bindings without renumbering existing classes, inherited
members or dimensions. `JoinFederationResponse` and `GetFOMView` return the
authoritative revision, digest and bindings. SDKs must not reconstruct those
bindings by sorting the final XML model. These rules do not establish complete
OMT conformance.

Successful same-generation restoration can replace a current participant handle
with its saved handle. Explicit cross-generation restoration allocates target
handles and remaps saved references. `FederationRestored` carries the restored identity and callback epoch;
SDKs invalidate prior queues and refresh the saved FOM before exposing success.
Applications suspend service calls until this terminal callback. Go LocalLRC
remains disabled until `ReinitializeLocalLRC` installs an authenticated state
image at its ordered callback barrier. Failure leaves confirmed behavior active
and does not reopen the local queue with stale state.

The additive `FederationRestored.next_retraction_handle` field is a suggested
next ID derived from complete saved/current accepted-ID history. Presence is
retained in Go and Python callbacks; the SDKs do not fabricate an allocator
from an absent field. It is not a minimum admissible caller value or a
reservation. The server rejects exact accepted/in-flight reuse while allowing
unused nonmonotonic IDs.

Resign performs the configured cleanup and drains required LocalLRC work in the
Go client. Go `Connection.Close` closes transport resources but does not imply
resign or drain. Applications shall resign explicitly before closing.

## 4. Declaration and Object Management

Object discovery conveys the most specific subscribed ancestor selected for that
recipient. The RTI retains this known class even if subscriptions later change.
Reflection attributes, object-bound service inputs and ownership callbacks use
handles for that known class; internal ownership state uses registered-class
handles. A local deletion and later rediscovery can establish different knowledge.
Production ownership handlers pass caller-known handles directly to the atomic
ownership command interface. The registry maps and validates them while pinning
knowledge through state mutation. A divest-if-wanted result uses the mapping
captured during that command, not a later lookup after callbacks. This changes
neither the public RPCs nor their wire fields.
DDM association changes map ancestor-known attributes while pinning object
knowledge and ownership. Associations created by a nonowner remain inactive until
acquisition; ownership loss discards the old owner's association. Local deletion
and region deletion remove the relevant inactive associations. Saved associations
use registered-class attributes and explicit associator identities.

Ownership-assumption callbacks retain a process-local knowledge epoch until server
dispatch. An obsolete assumption is suppressed, including during batched dispatch.
This is not an SDK invocation acknowledgement. Terminal ownership notifications
use the negotiated receipt/lifetime contract below and are not discarded as stale
offers. `FederateEvent.object_knowledge_epoch` and
`LocalDeleteObjectInstanceFenced` let SDKs fence buffered old discovery/assumption
advice, including same-class rediscovery; a server dispatch check alone cannot
prevent invalidation after transport Send starts.

Python string-key `update_attributes` resolves names in the caller's current known
object class, queried through Support. It does not search unrelated FOM classes or
retain a class cache across rediscovery. Numeric-key updates remain direct. A
string/numeric alias pair that identifies one attribute is rejected instead of
overwriting a payload; unknown names fail before the update RPC. The string path
adds a known-class query and may query uncached attribute names.

Declaration and OM methods exchange class, attribute, interaction, parameter,
instance, time, and value fields defined by the schema. Validation coverage is
operation-specific; numeric handle types alone cannot reject a handle from a
different class or prior federation.

Delete Object Instance now rejects an unknown caller/object relationship before
checking deletion privilege. Repeated ordinary acquisition preserves its pending
request and original tag without another release/confirmation callback, while
acquisition-if-available rejects that pending condition. Empty regional object
subscription sets preserve existing subscriptions and their options. These are
service-semantic corrections; the schema, field numbers and RPC list did not
change.

Preferred order is resolved on the server in every federation profile. Explicit
attribute-instance and publisher interaction-class order changes override FOM
preferences. Mixed-order updates produce separate RO/TSO attribute groups;
deletion uses `HLAprivilegeToDeleteObject` order. Untimestamped sends remain RO.
Timestamped sends classified as RO retain their original callback timestamp
without using it as a delivery bound. Missing order metadata or a missing
deletion-privilege attribute retains the timestamp-order fallback for timestamped
embedded-model calls. No SDK mode flag or new RPC selects this behavior.

Completion boundaries differ:

- confirmed unary calls return after their unary server result;
- `ConfirmedObjectService` is a lockstep bidirectional stream with one result
  for the same request sequence before the next request;
- LocalLRC first reports bounded local queue admission, then sends operations
  on a persistent stream and reports cumulative ACK progress; and
- Go untimed update and interaction calls may select LocalLRC by default, so
  their immediate return is local admission rather than confirmed server
  completion.

Callers that require server confirmation shall use the explicit confirmed path
or wait for the relevant LocalLRC cumulative ACK. Resign drains required queued
work; Go time-advance and connection close do not currently provide a universal
LocalLRC flush barrier.

## 5. Time Management

The wire exposes enable/disable regulation and constraint calls, lookahead
modification, time queries, NER, TAR, TARA, NMRA, and flush-queue requests.
Unary enablement accepts a request; completion uses the additive callback
variants `TimeRegulationEnabled` and `TimeConstrainedEnabled`, with the safe
logical time. Go, Python, and Java dispatch these server callbacks. Unary
request success indicates acceptance, while the enabled callback completes
activation.

Only one advance primitive may be pending per federate. Eligible TSO
events are committed before the related grant in the server delivery sequence.
Language adapters may expose only a subset of GALT/LITS and retraction methods.
FQR may deliver TSO callbacks beyond the granted time. Their received order and
retraction designators remain TSO; a speculative removal retains receiver object
knowledge until its timestamp is reached or constraint is disabled. A valid
RequestRetraction restores knowledge without a second discovery callback.

All five advance services accept valid finite requests from a joined caller with
neither regulation nor constraint enabled. An unconstrained caller's grant is
not bounded by GALT or an incoming next-message timestamp; target, pending-request
and scheduled-save checks still apply. The production membership interceptor
rejects unknown and resigned handles with `FederateNotExecutionMember` before
time-state mutation. Direct manager calls do not establish that membership guard.

A constrained receiver in the time-granted state keeps same-current-time TSO
queued even with HLA asynchronous delivery enabled. NMR/NMRA consider that queued
timestamp as a possible target; required TSO callbacks precede TAG at that time.

RO reflections, interactions and removals wait while a receiver is constrained,
not advancing and has asynchronous delivery disabled. Nonconstrained receivers,
advancing receivers and receivers with HLA asynchronous delivery enabled may
receive RO messages. All still-applicable queued RO messages precede TAG for
TAR, TARA, NMR, NMRA and FQR; a blocked grant does not itself block RO release.
This setting is unrelated to Python `async`/`await`. Original timestamps remain
present on RO callbacks without imposing a timestamp delivery gate.

An enable-asynchronous or disable-constraint call may succeed before its entire
backlog reaches the callback stream. Capacity pressure retains the new setting
and an unsent suffix for background release. Other drain errors restore the
prior flag and retain unsent messages. A failed advance delivery can return an
error after recording the request; that request remains pending and is retried
by the server. Submitting it again can return the existing pending-request error.
No TAG precedes required messages, and already released prefixes are not resent.
The RO retry behavior does not add RPCs. Time-mode activation separately adds
the two callback variants described above without replacing existing fields.

The existing GALT/LITS RPC fields and definition indicator are unchanged.
Pending requests can affect the reported GALT before a grant; it is not simply
the minimum of peers' last granted times plus lookahead. No other regulator
means undefined GALT, not a numeric zero bound. Incoming queued TSO messages may
still define LITS in that case.

Lookahead modification rejects a pending advance. Increases apply immediately;
decreases reduce the actual lookahead by granted logical-time progress until the
requested target is reached. Queries return actual lookahead. Pending send bounds
use the lookahead projected at the requested time, without a fixed comparison
tolerance. Finite zero-lookahead TAR/NMR bounds are strict, including their grant
history across same-time Available requests. Save-bundle time state uses version
4 for a nonempty RO queue; without that queue it uses version 3 for strict
history, version 2 for a pending decrease without that history, and version 1
otherwise. Version 5 additionally preserves pending enables, independent receive
bounds and partially delivered requests. Versions 1-5 are readable; older images
cannot supply history they never recorded. These are Float64 component versions;
typed Integer64 time state uses version 6. Existing public RPC fields are retained.

The additive `LogicalTimeValue` oneof selects `float64_value` or signed
`integer64_value`, including a present selector for zero. Typed Integer64 adapters
require the integer selector: required legacy scalar fields must remain canonical
positive zero, and optional legacy fields must be absent, even for time zero.
Float64 adapters accept legacy-only, exact-only or matching dual forms; conflicting
values and representation mismatches fail before mutation. Integer values never
convert through a double. Query definition/finite indicators distinguish an
unbounded horizon from finite Integer64 final time (`MaxInt64`).

`NewServerOf[T]` supplies typed unary OM/TM, timed DDM interaction and savepoint
transport plus callback encoding. Integer64 optimization flags for interaction
streams, ConfirmedObject and LocalLRC fail closed. The root also composes both
typed backends and routes unary requests using the federation's immutable
factory selection. The shared callback stream encodes the selected representation.
Custom factories and Integer64 experimental transports remain unsupported.

The existing time-state representation also retains logical time after an
advance with neither mode enabled and holds same-current-time TSO through
restore. Import does not itself emit callbacks; mode enablement does not reset
the restored time. Persistence and production membership checks are separate
verification scopes from live grant/delivery behavior.

The [order and advance record](../../verification/ieee1516-2010-order-advance-20260922.md)
records passing Windows/Linux regression and Linux race results for this bounded
batch. The [next record](../../verification/ieee1516-2010-flush-enable-retract-20260922.md)
covers FQR flushing/minimum grants, safe activation and Retract preconditions and
callbacks, with a separate current verification status. Corrupt past-TSO rejection,
full delayed subscription evaluation with DDM extents and complete source-based
time coverage remain open. Unary acceptance is
not enable completion.

Confirm Divestiture distinguishes `AttributeDivestitureWasNotRequested` from
`NoAcquisitionPending`. The existing gRPC error metadata carries this distinction
to Python exception translation. A legacy ambiguous transfer-state error remains
ambiguous rather than being assigned either meaning without evidence.
Ownership checkpoints with pending divestitures use a versioned extension around
the legacy protobuf snapshot to retain confirmation-request history. Existing
protobuf-only snapshots remain readable; their two-phase offer plus acquirer
combination retains legacy confirmability, but unstored withdrawn-request history
cannot be recovered. Old readers reject the extension instead of losing history.

Both Python range-setting interfaces require strict lower/upper ordering and the
dimension's upper limit. Equality is rejected with `InvalidRangeBound`, including
through the asynchronous Python DDM client and standard-style ambassador. This
does not alter the wire schema or retroactively rewrite saved legacy ranges.

## 6. Callback contract

The callback envelope includes lifecycle, discovery, reflection, interaction,
ownership, save/restore, synchronization, advisory, removal, and grant event
variants. Language adapters do not all translate every variant.

The wire forms of `ReflectAttributeValues` and `ReceiveInteraction` carry
handles, values, optional logical time, user tags, sent/received order,
transportation selection, producing federate, received regions and retraction
metadata. Optional fields preserve presence. Java selects the matching
standard callback overload from the delivered time, order, retraction, and
supplemental metadata.

RO eligibility applies to reflection, interaction and removal messages, not
discovery, grants, ownership, synchronization or save/restore control callbacks.
Queued reflections and interactions are rechecked against current subscriptions
when released, trimming attributes or suppressing the callback when appropriate.
Removal retains prior-discovery semantics after unsubscription. This recheck is
not full Delay Subscription Evaluation Switch support: messages excluded by
send-time subscriptions are not retained for later subscribers, and retained
region identities do not establish full delayed-delivery region-extent semantics.

Attribute-update advisories carry the selected designator when present.
Support queries expose named and effective update rates and contextual
dimension sets. These advisories request producer behavior; they do not change
timestamp delivery or grant ordering.

The Go adapter projects those handles to FOM names by default. Its optional
handle representation exposes numeric attribute and parameter maps directly as
`ReflectAttributeValuesByHandle` and `ReceiveInteractionByHandle`. The option
changes only the adapter representation; callback sequence and wire content
remain the same.

Callbacks are ordered on each active server stream. Required RO and TSO messages
are placed before their grant by the Time Manager. Stream
readiness, reconnect, gap recovery, and slow-consumer behavior are transport
contracts and are not inferred from numeric event order alone.

`OwnershipCallbackService` provides `EnableOwnershipCallbackReceipts`,
`ReportOwnershipCallbackInvocation` and `RetryOwnershipCallbacks`. Negotiation is
explicit for raw dispatch clients and precedes pending acquisition work. Strict
acquired, unavailable and acquisition-cancellation events carry
`FederateEvent.ownership_receipt`: federation generation, callback epoch, stable
callback ID and delivery attempt. Session authentication and current membership
are checked under checkpoint observation admission. Queue admission, channel
consumption and transport Send do not acknowledge invocation; an entered user
handler does, including one that throws. Early reports complete only after
successful transport completion, and failed delivery retains a retryable outcome.
This correctness ledger is independent of optional MOM `callback_receipt` coverage.
The Python and Java ambassadors attempt automatic negotiation after a modern join
with nonzero federation generation. An unimplemented receipt service retains the
legacy delivery-only profile; other negotiation failures do not silently downgrade.
Selected Python and Java localhost campaigns exercise this negotiation. Raw
Go/Python dispatch still requires explicit negotiation and actual-invocation
reporting; channel or queue consumption is not an invocation report.

Same-epoch replay is deduplicated by the SDK and re-acknowledged for its new attempt.
Restore clears old-scope acknowledgement/deduplication state; saved pending outcomes
must be invoked under the fresh scope. Pending acquisition/local-delete obligations
remain until authenticated invocation. Atomic unpublication prepare/commit ports
are integrated with declaration and wire admission, including explicit-empty
versus omitted whole-class selection, ownership guards and all-recipient callback
reservation. No full-conformance claim follows from these bounded interfaces.

MOM RO and TSO data-queue lengths combine disjoint time-waiting and admitted
ready/transport/client stages until authenticated callback entry. Moving a
message to transport does not remove it from the combined measurement. Control
callbacks and tentative reservations are excluded; unknown coverage is reported
as unavailable rather than zero. Already-ready and client-queue recovery across
a fresh-process checkpoint remains a separate acceptance boundary.

## 7. Wire services and listeners

The wire includes federate-facing Federation, Declaration, Object, Time,
Ownership, OwnershipCallback, DDM, Savepoint, Sync, MOM, Support, Stream, ConfirmedObject, and
LocalLRC services. It also includes Admin, Mutating, and Cluster services.

`DDMService.RegisterObjectInstanceWithRegions` now returns the actual object
handle from one registration transaction. All SDK registration overloads call
only this RPC; older Go/Python register-first clients must be upgraded with the
server. Repeated attribute entries union their region sets. Invalid numeric
bounds and incompatible dimension contexts use the existing exception trailer
with `InvalidRangeBound` and `InvalidRegionContext`. No message or RPC signature
changed. A strict raw audit writer rejects nonempty region registration with
`Unimplemented`. The production audit observer instead reports incomplete
recording without blocking the HLA operation. Application logging is separate.

Admin and optional mutating control run on the configured admin listener.
Mutating operations include force resign and federation destruction. Cluster
assignment operations affect routing state. Deployments shall keep these
listeners on trusted interfaces or protect them externally; current request
fields do not provide complete command-level generation fencing and principal
authorization for every control operation.

Existing protobuf field numbers are not reused, and generated bindings are
regenerated from one schema. Some unknown enum values currently fall back to a
default mode or transport rather than failing. Global payload and in-flight
limits are not uniformly configured; specific paths such as LocalLRC have
their own bounds.

The LocalLRC opening frame may request a maximum operation count per transport
frame. Zero preserves the legacy default of 32; current clients accept explicit
values 32, 64, 128, and 256, and the server advertises the negotiated limit in
its opening ACK. This limit is independent of cumulative ACK cadence.

## 8. Error channels

Errors use several transport forms:

- unary gRPC status codes and messages;
- the `rti-spec-exception` trailing-metadata key used by compatible adapters;
- embedded serialized status in confirmed-stream results; and
- local SDK sentinel or translated exception types.

Mapping is not uniform across all language profiles. The Go SDK translates a
subset of status text to sentinels and may not preserve operation context. The
Python and Java adapters use the trailing exception identity where supported.
Consumers shall not assume that every service and SDK returns the same concrete
error class for an equivalent failure.

## 9. Persisted interfaces

The event log uses structural format 2 with a fixed 64-byte little-endian
header. Save bundles use manifest format 2 for generation-aware restore and
retain tested read compatibility with manifest format 1.

The time-state v4 component adds `ro_queue` beside the existing TSO `queue`.
It preserves the original optional callback time, payload, binary tag and
metadata with typed sender and destination identities. Restore validates the
saved graph and rebinds federate identities without renumbering object or region
handles. Saved queues replace live queues and resume under the restored time
state after the terminal identity/epoch callback boundary. Immediate save control
callbacks do not release otherwise ineligible RO messages.

Version 4 is a component-format extension, not manifest format 4. Readers still
accept v1-v3, and reject an RO extension mislabeled as an older version, including
an empty or null field. A reader without v4 support must reject v4 instead of
silently dropping waiting messages. The
[RO queue record](../../verification/ieee1516-2010-ro-queue-20260922.md) tracks
compatibility and verification for this change without asserting full conformance
or performance non-regression.

Typed Integer64 components carry `HLAinteger64Time`: time v6, object v5,
retraction v3, MOM exact-value extension v2 and save-history v2. Exact decimal
integer parsing preserves adjacent times above `2^53` and finite final time;
factory mismatch is rejected before restore publication. Integer histories have
separate versioned storage; Float64 legacy formats remain readable.
Ownership component v3 persists enabled receipt sessions and pending terminal
outcomes. Restore validates all imported enabled sessions in one prepared scope
batch, retains callback IDs, resets transport attempts and selects fresh callback
epochs above both saved and current scopes. Credentials are never restored.

`rtid` defaults to the HLA core profile with no audit/replay provider.
`--audit-replay-plugin=event-journal` loads the optional provider and requires
a non-empty `--log-dir` for generation-qualified journal files.
`AdminService.TailEvents` is unavailable without that plugin. Plugin selection
does not alter the federate wire schema or HLA service results.

HLA managers expose persistence-neutral state DTOs. Save-bundle and event-log
adapters own Protobuf serialization and filesystem access. This internal
boundary does not change the existing event-log header, save-bundle manifest,
or public RPC schema.

## 10. Change control

### Explicit MIM selection

`CreateFederationRequest.mim_module` (field 8) supplies a selected MIM separately
from ordinary FOM modules. It requires a nonempty designator and XML bytes.
The exact explicitly supplied designator `HLAstandardMIM` is reserved and
rejected; the filename `HLAstandardMIM.xml` is distinct. Omission still selects
the default path rather than transmitting that reserved string as user input.
The SDK sends the V2 create profile when this field is present; ordinary create
requests remain V1. A mismatch is rejected, and clients must not retry with the
MIM removed. Older servers reject V2 before creation instead of silently ignoring
an unknown selection field.

`JoinFederationRequest.expected_mim_module` (field 7) carries an optional exact
selection precondition. Its V2 join profile is checked under exclusive lifecycle
admission before extending the FOM or allocating membership. A mismatch must not
be handled solely by joining and then resigning: that cannot undo a committed
FOM extension. Ordinary joins without the expectation remain V1.

The selected source is retained in `FOMView.mim_module` (field 7), live FOM
extensions and repository snapshots. The compatibility digest includes its role
and bytes. Omission preserves the historical FOM-only digest and embedded-MIM
behavior. MOM obtains its MIM designator from the committed repository, not a
fixed value. Failed selection must publish neither a federation nor its model.

The implemented selection profile preserves the embedded MIM's required ordered
structure and non-descriptive definitions; comments, formatting and supported
semantics descriptions may differ. Replacement definitions outside this profile
are rejected.

### Session and restoration additions

`JoinFederationResponse.session_token` is a private capability. Sensitive
session controls use binary gRPC metadata `gorti-session-token-bin`; tokens
are excluded from saved state, journals and MOM invocation arguments.
Restore preserves the live session but may change its federate handle.
Signed resign captures the authenticated generation, name and membership
incarnation under a retained read lease, then revalidates that identity under
the exclusive lifecycle gate. Missing or mismatched credentials never fall
back to unsigned behavior when session metadata is present.

`RequestFederationRestoreRequest.saved_federation_generation` selects an exact
source execution. Absence retains the same-generation interpretation; explicit
zero is not absence. This wire extension is distinct from a standard binding's
ordinary label-only restore signature.

`LocalLRCService.GetLocalValidationState` returns an authenticated image together
with a request nonce and callback epoch. `LocalValidationBarrier` is an internal
ordered stream marker, not an IEEE federate callback. The response alone cannot
enable LocalLRC. The old queue is never replayed automatically.

`OwnershipService.AttributeOwnershipReleaseDenied` preserves ownership and
emits grouped acquisition-unavailable callbacks. Object name reservation release
uses one `ReleaseMultipleObjectInstanceNames` request for all-or-nothing
validation, not a loop of single-name releases.

`FederateEvent.callback_receipt` is an opaque capability for an actual callback
envelope. `ReportCallbackInvocation` carries the outcome, not client-supplied
service or object identities. MOM query `attribute_values` contain MIM-encoded
measurements; `unavailable_attributes` explain missing coverage. Legacy scalar
counter fields retain their existing meaning.
The server may reserve `callback_receipt` before queue admission for mandatory
data or lifecycle callbacks. Confirmed callback-entry reporting authenticates
the receiver before the SDK enters the user callback; outcome reporting follows
the callback body. Entry and outcome are distinct and idempotent. Optional MOM
observation cannot retire a required domain obligation after a failed outcome.
This receipt remains separate from `ownership_receipt` and cannot substitute
for the ownership service's authenticated completion report.

The optional `CallbackReportService.Exchange` stream retains those report
messages, in batches of at most 64 with a cumulative successful-prefix ACK.
Its `gorti-callback-report-stream: 1` response header identifies the transport.
The additional `gorti-local-ro-callback-entry: 1` header delegates admission of
receipt-backed, untimed, nonregional RO data callbacks to the session's Go SDK.
The client activates this capability only after a successful authenticated
report on that stream. Absent or unknown capability values retain confirmed
entry. No Protobuf field, service method or persisted schema changes for this
negotiation.

The server advertises local admission only on the mandatory data receipt path,
whose nonregional RO entry observers cannot reject for domain preconditions.
The SDK validates its tracked receiver and runtime epoch, records local entry,
and queues ENTRY before OUTCOME without waiting for each ACK. TSO, callbacks
carrying region handles, ownership, recovery and controls stay confirmed. A known closed
callback stream disables local entry for its remaining buffered events.
Subsequent SDK services and checkpoint transitions wait for preceding reports;
explicit drain retries failed entries before their retained outcomes, never
the callback body. Authentication and receipt validation still apply to every
server report. Report errors remain observable and block later service calls
until successfully retried. The default build and explicit ReportCallbackEntry
operation keep their synchronous boundary.

When entry accounting is configured before joining, incoming data counters are
authoritative at entry even while callback outcomes remain pending. This applies
only to `HLAinteractionsReceived`, `HLAreflectionsReceived` and
`HLAobjectInstancesReflected`; failures still take precedence. Optional MOM
measurement failure cannot block the user callback body. Legacy scalar counters
retain their existing boundaries rather than being silently reinterpreted.
With local admission, the server incorporates entry evidence when its ordered
report arrives. Local dispatch alone is not a remote counter-settled boundary;
use an ordered SDK query or explicit report drain for that boundary.

Schema 44 adds `DDMManagerState.conveyed_regions` at field 7 for exact copied
geometry and receiver leases. Restore validates the candidate graph before
publication and rebinds recipients to the restored generation. Existing field
numbers and the RPC list are unchanged. This persistence extension does not
alone establish recovery of already-ready transport or unread SDK callbacks.

The additive `callback_pending` save-bundle component separately persists
unentered discovery/reflection/interaction/removal and server-streamed control
carriers, without their old receipt capabilities. Version 1 remains readable;
version 2 adds lowercase SHA-256 invocation identity, pending/outcome/completed
phase, optional known result, and a validated ownership callback-ID relationship.
Recovered callbacks follow the restored identity/epoch terminal and obtain fresh
receipts. Entered callback bodies are not replayed. Unknown
versions, duplicate JSON keys/case aliases, unknown wire fields, invalid scalar
times and copied-region lease mismatches reject before publication.

Additive FederateEvent field 6 is CallbackRecovery: invocation_identity (bytes,
tag 1), outcome_only (bool, tag 2) and completed (bool, tag 3). An outcome-only
carrier retains its body for server validation but the SDK must not invoke it,
report Entry, or repeat cache/time/lifecycle effects. A completed carrier has no
body or receipt and clears the matching SDK result without an RPC. Invocation
identity is correlation evidence, never authorization. Previous descriptors are
checked by additive projection; the RPC list has not changed.

Private callback ledger rows may include `transportation`: object or interaction
target, required `attributes`, `actual_attributes`, `transportation` and
`activated` fields. Empty attribute lists are `[]`, not null. Nested wire ticket,
generation and callback epoch are removed before ledger serialization and freshly
issued for pending replay. Known finished results allocate no activation ticket.
Missing phase evidence, unknown/duplicate/null fields, inconsistent mappings and
overlapping pending intents reject preparation. This is a private checkpoint
extension, not an additional IEEE service or RPC.

Go, Python and Java SDKs emit GRS1 private runtime envelopes in the existing
`client_state` field. Fields are `magic`, `version`, `language`, `scalar`,
`generation`, `self`, `state`, and `checksum`. The checksum is SHA-256 over the
newline-delimited GRS1/version/language/scalar/generation/self header and compact
raw state JSON. Private payloads are language-specific and version-validated.
Cross-generation translation changes explicit federate references only. The
original source handle in `initiateFederateRestore` is not the target envelope's
identity; successful terminal hydration verifies the installed target instead.
The private recovery profile is callback_recovery: server-v2 with required
pending_outcomes containing identity, success and exception. Known completed
results survive failed reporting; executing bodies and unknown results reject
export. Source ledger/runtime reconciliation rejects missing or contradictory
evidence before persistence or allocation. Unsupported target runtime staging
must fail before participant restore completion.
Optional `pending_local_notifications` is an array of records with exactly
`local_id`, `order`, `scope`, `kind` and `payload`. The ID is lowercase hex64
local correlation, not a server invocation identity or authority. Order is an
exact positive uint64 JSON number, strictly increasing; repeated payloads with
different IDs remain distinct. Member-scoped kinds cover ownership query results
and legacy cancellation completion, transportation query reports and completed
direct-change confirmations, and save/restore status vectors. The execution-list
notification is connection-scoped. Unknown kinds, fields, nulls and capabilities
reject during staging. Historical post-restore status handles and foreign
execution-list generations are not rewritten. Pending server callbacks still
use fresh ledger carriers; local records require neither Entry nor Outcome RPCs.
Existing 1 MiB envelope, collection and aggregate limits apply. Prepared
activation tickets and arbitrary function closures are not portable local data.
Empty legacy state remains supported but does not reconstruct private SDK state.

Conveyed-region range and dimension queries carry the federate's session token.
The Float64 and Integer64 handlers bind the request identity to that session and
allow a copied designator only during its invoking receiver's callback lease.
After restore they resolve the stable session name, generation and incarnation
to the current handle; a cached pre-restore handle is not receiver authority.
Missing authentication, foreign receiver identities and expired copies fail;
the SDK must not fall back to querying the sender's mutable region specification.

### Python federation inputs

The standard-style Python facade preserves its existing positional call forms.
`joinFederationExecution(federate_name, federation_name, additional_fom_modules=None, *, federate_type="")`
forwards the keyword-only type through the existing join protocol. A string or
bytes value in the additional-module position is rejected rather than split
into individual characters or interpreted as a Java overload.

`createFederationExecution(name, fom_modules, *, logical_time_implementation_name="", mim_module=None)`
accepts empty, `HLAfloat64Time` or `HLAinteger64Time` locally. Other strings raise
`CouldNotCreateLogicalTimeFactory` (also catchable as `ValueError`), and non-string
selectors raise `TypeError`, before creation or cached-state changes. The selected
name is forwarded to the server and reported in create/list/join metadata;
omission on a legacy response means Float64 only. Selection is immutable for an
execution. The single-server gRPC root accepts Integer64 when enabled and selects
its exact backend. Unsupported execution profiles reject the factory before
federation publication; custom implementations remain unsupported.
MIM selection uses the
separate create profile described above. Existing callers that omit the keyword
are unchanged.

The `mim_module` keyword and `FederationSpec.mim_module` accept a file path or an
immutable `MIMModule(designator, xml)` value. Successful creation retains the
selection for a later name-only join. Failed creation does not replace the
facade's model or federation cache. Returned views retain the selection separately
from FOM modules, including after restore. No fallback substitutes the embedded
MIM when an explicit selection cannot be parsed locally.

Federate-bound support and savepoint clients forward their session token for
handleless lookups and save/restore status queries. Standalone clients retain
their existing tokenless behavior unless an optional `session_token` is supplied.

MOM observation of handleless queries may use the same session-token metadata
to identify live membership under generation/incarnation checks. It does not
make an unsigned query a signed mutation or expose tokens in MOM reports.

Public changes require matching SRS, SDD, IDD, STD, schema, generated-binding,
compatibility-test, and migration updates. Performance work shall document
whether it changes local admission, confirmed completion, callback fields,
error visibility, stream ordering, or logical-time behavior.
