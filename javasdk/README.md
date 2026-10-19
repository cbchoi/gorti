# gorti Java SDK

Java federates use the standard `hla.rti1516e` interfaces to connect to the same
Go RTI server as Go and Python federates. The provider supports federation
lifecycle, synchronization, declaration, object/interaction exchange, ownership,
regional routing, save/restore, and Float64 and Integer64 time management.
Encoding helpers implement `EncoderFactory`. The service table below lists
the provider's interfaces and runtime options.

## Build

Use JDK 11 or later and Maven 3.9. PowerShell and Bash launchers are provided.
The recorded 4 October 2026 Java service campaign ran on Windows with Temurin 11.

From the repository root:

```sh
mvn -f javasdk/pom.xml verify
```

The build creates three artifacts:

| Module | Content |
|---|---|
| `ieee1516-api` | Official IEEE API declarations, with the two documented Java service-loader adaptations |
| `wire` | Java Protobuf and gRPC types generated from the existing `proto/` schemas |
| `client` | gorti service provider, handles, time types, callback dispatch and transport |

No Portico or commercial RTI JAR is required. Do not put another RTI's copy of
`hla.rti1516e` on the same classpath. The IEEE API material retains its own
[permission and attribution](ieee1516-api/NOTICE.md); it is not relicensed as MIT.

## Run

Start the server in a separate terminal from the repository root:

```sh
go run ./rti/cmd/rtid --listen=127.0.0.1:50051 --metrics-listen=127.0.0.1:0 --admin-listen= --log-level=warn
```

Run the Java time-management example:

```powershell
.\javasdk\run-example.ps1 -Endpoint grpc://127.0.0.1:50051
```

```sh
bash javasdk/run-example.sh --endpoint grpc://127.0.0.1:50051
```

The example creates its own federation, joins one Java federate, waits for actual
enabled callbacks, requests time advances, and resigns and destroys the federation
on exit. Use it to check the connection and callback lifecycle.

For object and interaction exchange, use the
[two-process Java example](../examples/java-exchange/README.md). Build the SDK
once with `mvn -f javasdk/pom.xml -DskipTests install`, then launch its publisher
and receiver in separate terminals. Both roles use the public `hla.rti1516e` API.

The connection designator is `grpc://host:port` for explicit plaintext or
`grpcs://host:port` for TLS with the JVM's trust configuration. The two-argument
`connect` overload reads the `gorti.endpoint` system property and otherwise uses
`grpcs://localhost:50051`. It does not silently downgrade TLS. The callback model
is `HLA_EVOKED`; federate callbacks run on the thread invoking an evoke method.

Callback delivery can be switched while connected, including before join.
Enable/disable calls reject an active federate save or restore without changing
the switch. Initiation queued while callbacks are disabled does not prevent
re-enabling them to enter that callback. Restore completion still requires
validated SDK state; an idle completion reports `RestoreNotRequested`.

```java
RTIambassador rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
rti.connect(callbacks, CallbackModel.HLA_EVOKED, "grpc://127.0.0.1:50051");
```

FOM/MIM arguments must be local `file:` URLs. The SDK reads the selected files
and sends their bytes to the connected server. A single file is limited to
4 MiB; the server's total RPC message limit still applies. Do not use private
model files with a server you do not trust.

Federation creation requires at least one FOM module. An empty creation array
is rejected before any creation RPC; an empty array of additional modules at
join remains valid. Omitting a MIM selects the standard MIM. The Go/Python
product interfaces retain a zero-FOM-module convenience extension, which still
includes the standard MIM; it is not the standard Java creation contract.

## Service Coverage

| Area | Current provider |
|---|---|
| Discovery | Named/default/enumerated RTI factory; local `HLAfloat64Time` and `HLAinteger64Time` factories |
| Federation lifecycle | Connect/disconnect; create, list and destroy; join and resign |
| Synchronization | Register for current members or an explicit set; an empty set selects current members; announce, achieve and report failed participants |
| Declaration | Publish/unpublish and active/passive subscribe/unsubscribe for object attributes and interactions |
| Object management | Single/multiple name reservation and release; registration; RO/TSO update, interaction and deletion; local deletion; attribute-update requests; retraction |
| Lookups and support | Object classes, attributes, interactions, parameters, federates, callback-visible objects, order and transportation names; update rates, automatic resign directives, normalization and advisory switches |
| Time management | Regulation/constrained enable and disable; TAR, TARA, NMR, NMRA and FQR; lookahead; logical time, GALT and LITS queries; asynchronous delivery switch |
| Ownership | Divest, negotiate, acquire, deny release, confirm, cancel, query and test ownership; evoked ownership callbacks |
| Transportation | Attribute/interaction order changes; transportation change requests, confirmations and reports |
| DDM | Region creation, staged bounds and commit, deletion, dimension queries, regional registration, additive associations, atomic heterogeneous regional subscriptions and update requests, RO/TSO regional interactions |
| Save/restore | Immediate and timed save, progress, abort and participant failure reasons; persisted SDK object knowledge, staged region bounds, synchronization labels and time state; restored callback epochs |
| Callbacks | Actual server enable callbacks and TAG; discovery, all nine reflect/receive/remove overloads, update requests, reservation, synchronization, declaration/scope advisories and retraction; evoked dispatch and callback enable/disable |
| Basic types | Immutable typed handles, eight handle factories, seven collection factories, exact Integer64 and Float64 time/interval values; scalar, string, opaque, array, fixed-record and variant-record encoders |
| Server integration | Exact Integer64 execution; DDM association lifecycle and conveyed region copies; delayed subscription evaluation; MOM requests and callback accounting |
| Connection profile | Evoked callbacks over gRPC; local-file FOM/MIM inputs; application-managed reconnection |

[service-coverage.tsv](service-coverage.tsv) enumerates every standard
`RTIambassador` signature as `MAPPED` or `PENDING`. `MAPPED` means a typed provider
method is present. All 172 signatures are mapped. Behavioral results and
clause-level review are recorded in the
[verification guide](../docs/verification.md).

Regional subscription, unsubscription, association and attribute-update requests
carry attribute/region groups in one RPC. Every supplied handle is validated before
mutation; explicit empty groups do not silently become clear-all operations.
Associations are additive. The server retains pre-acquisition associations
separately and activates them on ownership acquisition. Local staged bounds,
committed region specifications, and callback-conveyed copies have distinct
lifetimes and are preserved through their respective checkpoint paths.

Transportation changes take effect at evoked confirmation entry, not at request
return. Authenticated, generation-bound activation prevents old confirmations from
changing a rejoined or restored federation. Attributes lost before confirmation
are excluded even when subsequently reacquired. The Java provider requires the
new preparation/activation RPCs; an older server rejects the request explicitly.

Java sends its private SDK checkpoint with `federateSaveComplete`; the server stores
it in the same save bundle as the federation state and returns only that participant's
checkpoint during restore. Rejoining participants are matched by federate name
and checked against the saved federate type when present.
The format is versioned and checksummed, with a 1 MiB per-participant limit and an
8 MiB federation total for these runtime checkpoints. It contains SDK state, not the
federate's application model or session authentication token. Applications remain
responsible for their own model state.

Apply `federationRestored` through an evoke call before modifying restored state.
Permitted support queries can read the restored identity and object knowledge
before that callback, even with callbacks disabled; those queries do not invoke
application callbacks. Recognized pending callbacks are restored under fresh
authorization; known finished results are reported without invoking their bodies
again. Completion markers reconcile results already accepted by the server.
Typed queued query notifications retain local FIFO without repeating the query.
Arbitrary closures, unexecuted local activation closures and executing callbacks
reject export rather than disappearing from the checkpoint. Restore success
requires validated SDK state. Supported versioned GRS1 images can be translated
into a recreated federation; legacy bundles without SDK state still require the
original in-memory checkpoint.

`HLAinteger64Time` factories preserve exact integer values and perform checked
arithmetic, including values above `2^53`. The server supports Float64 and
Integer64 federations. Integer64 requests, queries, callbacks and SDK checkpoints
use exact additive wire fields, never a conversion through `double`.

The standard ambassador automatically negotiates terminal ownership invocation
receipts before subscribing to callbacks. Only `UNIMPLEMENTED` from an older
server permits delivery-only compatibility. Mandatory receipts are independent of
optional MOM telemetry: queued callbacks are not acknowledged, throwing handlers
count as invoked, retries do not invoke the handler twice, and local-deletion
epochs suppress stale ownership advice without dropping terminal outcomes.
Cancellation confirmation uses the server stream in negotiated mode; legacy
servers retain the SDK's evoked RPC-result callback. Private GRJ2 checkpoints
preserve tagged Integer64 time; existing GRJ1 Float64 checkpoints remain readable.

The encoding factory is available through `RtiFactory.getEncoderFactory()`.
Encoded object arrays share a limit of 1,048,576 entries per decoded value;
strings and opaque data are bounded by the supplied input buffer. Encoder
instances are mutable and are not thread-safe.

An explicit empty synchronization set selects all currently joined federates,
as does the overload without a participant set. Attribute removal
uses a separate presence field: an explicit empty unpublish/unsubscribe set
is a no-op, while the whole-class overload removes the class declaration.

Retraction handles include federation name, generation and producing federate.
When a native Go/Python sender opts out of retraction tracking (wire ID zero),
Java still delivers a timestamp-ordered callback using a unique, non-retractable
local message identity. It does not invent a server-side retraction record.

Save/restore failure callbacks add an optional failure-reason field to the
protocol. Existing RPC signatures and field numbers are unchanged. Older
servers that omit the reason cannot supply a complete Java failure callback;
the provider reports that omission instead of inventing a reason.
Unary service calls carry immediate errors. Callback readiness, authenticated
session identity and federation generation are checked independently of request
acceptance. Time-enabled callbacks and TAG are not fabricated after an RPC return.

## Tests

For a local run with a freshly built server and the Python interoperation peer:

```sh
python -m pip install -e "./pysdk[dev]"
buf generate
python scripts/validate_sdks.py java
```

This is also the Java CI entry point. Use `--maven PATH` when Maven is not on
`PATH`. Reports are written outside the repository; `--artifacts PATH` selects
their location. IEEE source-archive/schema diagnostics and cross-binding
checkpoint fixture tests need separately supplied inputs and are reported as
skipped without them.

The default Maven test run executes unit and in-process transport tests. It
explicitly skips live tests unless an endpoint is supplied. With a running
local server, enable endpoint-based tests with:

```sh
mvn -f javasdk/pom.xml "-Dgorti.endpoint=grpc://127.0.0.1:50051" "-Dgorti.fom=/absolute/path/to/gorti/examples/go-tar-wait/tar-wait-fom.xml" "-Dgorti.python=/absolute/path/to/python" "-Dgorti.evidence.dir=/absolute/path/to/evidence" test
```

Create the evidence directory outside the repository first. On Windows use
absolute Windows paths. `validate_sdks.py` supplies these properties automatically.
Live tests run independent RTI
sessions in one JVM. They cover peer-blocked TAR, all five advance requests,
typed errors, object/interaction bytes and tags, synchronization, producing
federates, timestamped callback ordering, ownership negotiation, DDM routing and
immediate/timed save and restore. The stateful tests use their own FOM fixtures.
DDM includes both two-session and three-session cases. The separate example
covers two independent JVMs. Cross-binding and checkpoint-fixture executions
are listed separately in the [full service report](../engineering/verification/ieee2010-full-service-rerun-post-sync-20261004.md).

The generated facade preserves each method's declared checked exceptions and
forwards directly to typed service methods, without runtime method-name dispatch.
After changing its mapped-service list, regenerate it from the compiled API:

```sh
java --class-path javasdk/ieee1516-api/target/classes javasdk/tools/GenerateAmbassador.java javasdk
java --class-path javasdk/ieee1516-api/target/classes javasdk/tools/GenerateAmbassador.java javasdk --check
```

The [4 October full service report](../engineering/verification/ieee2010-full-service-rerun-post-sync-20261004.md)
records 591 passing Java tests with live-server and diagnostic cases enabled.
Earlier exchange, stateful-service, and checkpoint reports remain under
`engineering/verification/` as implementation history. The
[verification guide](../docs/verification.md) summarizes requirements review
and the current test evidence.
