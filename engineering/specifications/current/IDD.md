# Interface Design Description

Document version: 4.0
Status: Current implementation profile
Updated: 2026-07-17

## 1. Interface hierarchy

gorti has four interface boundaries:

1. language-specific Go, Python, and C++ federate APIs;
2. Protobuf/gRPC messages and services under `proto/rti/v1/`;
3. server-streamed federate callbacks; and
4. internal Go interfaces used to compose `rtid`.

The current wire package is `rti.v1` with `WIRE_VERSION_V1`. The schema
contains 16 services and 115 RPC methods; `FederateEvent` contains 36 event
variants. These counts are inventory checks. The `.proto` files and generated
bindings define the exact fields and methods.

## 2. Language profiles

The three SDKs do not expose an identical surface.

| Profile | Current principal surface | Important limits |
|---|---|---|
| Go `rti/pkg/federate` | Join/resign, declarations, OM, TM, synchronization, DDM, callbacks, confirmed object stream, LocalLRC | No equivalent top-level ownership, save/restore, MOM, or Support clients; handles are numeric |
| Python asynchronous API | Lifecycle and dedicated service clients, including ownership, save/restore, DDM, MOM, and Support | Some standard operations, including outbound GALT/LITS and retraction paths, are not exposed uniformly |
| Python `Rti1516eAmbassador` | Synchronous IEEE-shaped adapter, typed handle wrappers and collections | Typed handles remain integer values without embedded federation provenance |
| C++ `RTI/` profile | IEEE 1516-2010 DLC-shaped headers, callbacks, exceptions, encodings, and broad service delegation | Some order/transport change operations are compatibility no-ops; conformance fixtures define the tested subset |

The table is profile-specific; a service or overload listed for one language
is not implied for the others.

## 3. Identity and lifecycle

Join returns a numeric federate handle. Normal unary requests identify a caller
by federation name and federate handle; they do not carry a session token or an
embedded federation generation. Handles in all three language profiles are
numeric values and do not themselves prove FOM type, federation, or generation.

The server checks current federation membership and performs additional
operation-specific validation where implemented. A caller that can reach the
RPC endpoint is not cryptographically bound to a particular federate handle by
the normal request fields. TLS and optional identity middleware protect the
transport, but this is not a complete per-federate authorization mechanism.

Resign performs the configured cleanup and drains required LocalLRC work in the
Go client. Go `Connection.Close` closes transport resources but does not imply
resign or drain. Applications shall resign explicitly before closing.

## 4. Declaration and Object Management

Declaration and OM methods exchange class, attribute, interaction, parameter,
instance, time, and value fields defined by the schema. Validation coverage is
operation-specific; numeric handle types alone cannot reject a handle from a
different class or prior federation.

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
Enablement currently completes through the unary RPC result. The callback
stream exposes `TimeAdvanceGrant`; it does not expose separate
`TimeRegulationEnabled` or `TimeConstrainedEnabled` variants.

Only one advance primitive may be pending per federate. Eligible timestamped
events are committed before the related grant in the server delivery sequence.
Language adapters may expose only a subset of GALT/LITS and retraction methods.

## 6. Callback contract

The callback envelope includes lifecycle, discovery, reflection, interaction,
ownership, save/restore, synchronization, advisory, removal, and grant event
variants. Language adapters do not all translate every variant.

The current wire forms of `ReflectAttributeValues` and `ReceiveInteraction`
carry handles, values, and optional logical time. They do not carry every field
present in the complete IEEE callback signatures, including all combinations
of user tag, sent/received order, transportation, producing federate, and
message-retraction data. API compatibility claims are therefore limited to the
fields and callbacks verified by the conformance fixtures.

The Go adapter projects those handles to FOM names by default. Its optional
handle representation exposes numeric attribute and parameter maps directly as
`ReflectAttributeValuesByHandle` and `ReceiveInteractionByHandle`. The option
changes only the adapter representation; callback sequence and wire content
remain the same.

Callbacks are ordered on each active server stream. Timestamped callbacks
eligible for a grant are placed before that grant by the Time Manager. Stream
readiness, reconnect, gap recovery, and slow-consumer behavior are transport
contracts and are not inferred from numeric event order alone.

## 7. Wire services and listeners

The wire includes federate-facing Federation, Declaration, Object, Time,
Ownership, DDM, Savepoint, Sync, MOM, Support, Stream, ConfirmedObject, and
LocalLRC services. It also includes Admin, Mutating, and Cluster services.

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
Python and C++ adapters use the trailing exception identity where supported.
Consumers shall not assume that every service and SDK returns the same concrete
error class for an equivalent failure.

## 9. Persisted interfaces

The event log uses structural format 2 with a fixed 64-byte little-endian
header. Save bundles use manifest format 2 for generation-aware restore and
retain tested read compatibility with manifest format 1.

`rtid` defaults to the HLA core profile with no audit/replay provider.
`--audit-replay-plugin=event-journal` loads the optional provider and requires
a non-empty `--log-dir` for generation-qualified journal files.
`AdminService.TailEvents` is unavailable without that plugin. Plugin selection
does not alter the federate wire schema or HLA service results.

## 10. Change control

Public changes require matching SRS, SDD, IDD, STD, schema, generated-binding,
compatibility-test, and migration updates. Performance work shall document
whether it changes local admission, confirmed completion, callback fields,
error visibility, stream ordering, or logical-time behavior.
