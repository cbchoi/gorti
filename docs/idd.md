# Interface Design Document — Go HLA Evolved RTI

Status: draft, locked-by-conversation 2026-04-28.
Companion: `docs/sdd.md` (Software Design Document) describes structure and dynamics; this IDD specifies interfaces in detail.
Trace: every interface here cites the SRS requirement IDs it implements.

---

## 0. Document Conventions

- All interface signatures are **normative**. Implementations must match exactly.
- Go signatures are authoritative for Go interfaces. Python signatures are authoritative for Python interfaces. Wire signatures are defined by `proto/rti/v1/*.proto` (frozen, orchestrator-owned).
- "MUST" / "MUST NOT" / "SHOULD" follow RFC 2119.
- Error contracts are part of the interface. Returning an unspecified error is a contract violation.

---

## 1. External Interfaces

External interfaces are seen by entities **outside** the RTI process or library: federate processes, FOM authors, operators.

### 1.1 gRPC Wire Protocol

Implements: IR-PROTO-1..3, FR-FM-*, FR-DM-*, FR-OM-*, FR-TM-*.

The proto files live in `proto/rti/v1/` and are FROZEN. This section describes their **shape and semantics**; the actual `.proto` files are M0 deliverables.

#### 1.1.1 Services

```proto
service FederationService {
  rpc CreateFederation(CreateFederationRequest) returns (CreateFederationResponse);
  rpc DestroyFederation(DestroyFederationRequest) returns (DestroyFederationResponse);
  rpc JoinFederation(JoinFederationRequest) returns (JoinFederationResponse);
  rpc ResignFederation(ResignFederationRequest) returns (ResignFederationResponse);
  rpc ListFederations(ListFederationsRequest) returns (ListFederationsResponse);
}

service DeclarationService {
  rpc PublishObjectClassAttributes(PubObjAttrsRequest) returns (Empty);
  rpc UnpublishObjectClassAttributes(UnpubObjAttrsRequest) returns (Empty);
  rpc SubscribeObjectClassAttributes(SubObjAttrsRequest) returns (Empty);
  rpc UnsubscribeObjectClassAttributes(UnsubObjAttrsRequest) returns (Empty);
  rpc PublishInteractionClass(PubInterRequest) returns (Empty);
  rpc UnpublishInteractionClass(UnpubInterRequest) returns (Empty);
  rpc SubscribeInteractionClass(SubInterRequest) returns (Empty);
  rpc UnsubscribeInteractionClass(UnsubInterRequest) returns (Empty);
}

service ObjectService {
  rpc RegisterObjectInstance(RegisterRequest) returns (RegisterResponse);
  rpc UpdateAttributeValues(UpdateRequest) returns (Empty);
  rpc SendInteraction(SendInteractionRequest) returns (Empty);
}

service TimeService {
  rpc EnableTimeRegulation(EnableRegulationRequest) returns (Empty);
  rpc DisableTimeRegulation(DisableRegulationRequest) returns (Empty);
  rpc EnableTimeConstrained(EnableConstrainedRequest) returns (Empty);
  rpc DisableTimeConstrained(DisableConstrainedRequest) returns (Empty);
  rpc NextMessageRequest(NERRequest) returns (Empty);  // grant arrives via StreamService
}

service StreamService {
  // Server-streamed callbacks to a joined federate.
  rpc Events(EventsRequest) returns (stream FederateEvent);
}
```

Why a separate `StreamService.Events`: HLA callbacks (`reflectAttributeValues`, `receiveInteraction`, `timeAdvanceGrant`, `discoverObjectInstance`, etc.) are server-initiated. A single bidi stream per federate carries all callbacks. Federate-to-RTI calls go via the unary RPCs above for clarity.

#### 1.1.2 Key message shapes (illustrative; full proto in M0)

```proto
message FederateEvent {
  uint64 seq = 1;
  oneof event {
    DiscoverObjectInstance discover = 10;
    ReflectAttributeValues reflect = 11;
    RemoveObjectInstance remove = 12;
    ReceiveInteraction interaction = 13;
    TimeAdvanceGrant grant = 14;
    FederationHalted halted = 99;
  }
}

message ReflectAttributeValues {
  uint64 object_handle = 1;
  uint64 class_handle = 2;
  map<uint64, bytes> attributes = 3;  // attribute_handle -> HLA-encoded bytes
  optional double logical_time = 4;
  RoutingInfo routing = 5;
}
```

Attribute / parameter values cross the wire as raw HLA-encoded bytes (per IR-PROTO-3 / FR-ENC-*). Protobuf is the envelope; HLA Evolved encoding is the payload.

#### 1.1.3 Error codes (`proto/rti/v1/errors.proto`)

All wire errors carry a `code` (string from a closed set) and a `message` (human-readable). Codes are namespaced by domain:

- **`ERR_FED_*`** — Federation Management:
  - `ERR_FED_NOT_FOUND`, `ERR_FED_ALREADY_EXISTS`, `ERR_FED_NOT_JOINED`, `ERR_FED_ALREADY_JOINED`, `ERR_FED_HAS_FEDERATES_JOINED`.
- **`ERR_FOM_*`** — FOM parsing/validation. See §1.2 for full numbered list.
- **`ERR_OBJ_*`** — Object Management:
  - `ERR_OBJ_NOT_FOUND`, `ERR_OBJ_CLASS_NOT_PUBLISHED`, `ERR_OBJ_ATTR_NOT_OWNED`, `ERR_OBJ_HANDLE_INVALID`.
- **`ERR_TIME_*`** — Time Management:
  - `ERR_TIME_NOT_REGULATING`, `ERR_TIME_NOT_CONSTRAINED`, `ERR_TIME_INVALID_LOOKAHEAD`, `ERR_TIME_REQUEST_IN_PAST`.
- **`ERR_ENC_*`** — Encoding:
  - `ERR_ENC_INSUFFICIENT_BYTES`, `ERR_ENC_TYPE_MISMATCH`, `ERR_ENC_PADDING_VIOLATION`.
- **`ERR_WIRE_*`** — Transport:
  - `ERR_WIRE_VERSION_MISMATCH`, `ERR_WIRE_MALFORMED_MESSAGE`.

Codes are **closed**; agents may not invent new codes. Contract change request needed.

#### 1.1.4 gRPC status mapping

| RTI error class | gRPC status code |
|---|---|
| `ERR_FED_NOT_FOUND`, `ERR_OBJ_NOT_FOUND` | NOT_FOUND |
| `ERR_FED_ALREADY_EXISTS`, `ERR_FED_ALREADY_JOINED` | ALREADY_EXISTS |
| `ERR_FED_HAS_FEDERATES_JOINED`, `ERR_TIME_NOT_REGULATING` | FAILED_PRECONDITION |
| `ERR_FOM_*`, `ERR_ENC_*`, `ERR_TIME_INVALID_LOOKAHEAD`, `ERR_WIRE_MALFORMED_MESSAGE` | INVALID_ARGUMENT |
| `ERR_WIRE_VERSION_MISMATCH` | UNIMPLEMENTED |
| Internal panic / unknown | INTERNAL |

The error code is also placed in the gRPC status detail (`google.rpc.ErrorInfo`) so federate SDKs can match deterministically.

#### 1.1.5 Wire protocol versioning

- Every gRPC envelope carries a `wire_version` field (uint32). Current: `1`.
- RTI rejects mismatched versions with `ERR_WIRE_VERSION_MISMATCH`.
- Pre-1.0: bumping `wire_version` is permitted; contract-change-request required.

### 1.2 FOM XML Format

Implements: IR-FOM-1, FR-FOM-1..4.

- **Schema**: IEEE 1516.2-2010 DIF (the published XSD). Strict — anything not in the schema is rejected.
- **Encoding rules in the FOM** must reference identifiers from IEEE 1516.2-2010 §4 (e.g. `HLAfixedRecord`, `HLAvariableArray`).
- Multi-module FOMs accepted in cut 2; cut 1 accepts standard MIM + one user module.

#### 1.2.1 Numbered diagnostics

| Code | Meaning |
|---|---|
| `FOM-001` | DataType referenced but not defined |
| `FOM-002` | Object class hierarchy contains a cycle |
| `FOM-003` | Object class has multiple parents |
| `FOM-004` | Attribute name duplicated within class (including inherited) |
| `FOM-005` | Interaction parameter duplicated within class |
| `FOM-006` | Unknown encoding rule identifier |
| `FOM-007` | Unknown order rule identifier |
| `FOM-008` | Unknown transportation rule identifier |
| `FOM-009` | Unknown XML element or attribute (strict mode) |
| `FOM-010` | Required field missing |
| `FOM-011` | Class references non-existent parent |
| `FOM-012` | Interaction class references non-existent parent |
| `FOM-013` | Variant record without discriminator field |
| `FOM-014` | Fixed array with non-positive cardinality |
| `FOM-101` | User module attempts to redefine MIM type or class |

Diagnostics extend (range `FOM-200`+) when new validation is added; never renumber existing codes.

### 1.3 Python SDK Public API (`pysdk/rti1516e/`)

Implements: IR-PYAPI-1, FR-PYJ-*.

#### 1.3.1 Layer 1 — idiomatic API

```python
class RtiConnection:
    @classmethod
    async def connect(
        cls, *, url: str, tls: bool = False,
        ca_cert: Path | None = None,
    ) -> "RtiConnection": ...
    async def __aenter__(self) -> "RtiConnection": ...
    async def __aexit__(self, *exc: object) -> None: ...
    async def close(self) -> None: ...
    async def join_federation(
        self, spec: FederationSpec, *, federate_name: str,
    ) -> "Federation": ...

@dataclass(frozen=True, slots=True)
class FederationSpec:
    name: str
    fom_modules: list[Path]
    mode: Literal["verbose", "best_effort"] = "verbose"
    stall_timeout: float = 60.0  # seconds; create-only

class Federation:
    federate_handle: int  # property; assigned at join
    fom: "FOM"            # property; immutable after join
    async def __aenter__(self) -> "Federation": ...
    async def __aexit__(self, *exc: object) -> None: ...
    async def resign(self) -> None: ...
    # Declaration
    async def publish_object_class(self, class_name: str, attributes: list[str]) -> None: ...
    async def subscribe_object_class(self, class_name: str, attributes: list[str]) -> None: ...
    async def publish_interaction_class(self, class_name: str) -> None: ...
    async def subscribe_interaction_class(self, class_name: str) -> None: ...
    # Object/Interaction
    async def register_object(
        self, class_name: str, name: str | None = None,
    ) -> "ObjectInstance": ...
    async def send_interaction(
        self, class_name: str, parameters: dict[str, object],
        timestamp: float | None = None,
    ) -> None: ...
    # Time
    async def enable_time_regulation(self, lookahead: float) -> None: ...
    async def enable_time_constrained(self) -> None: ...
    async def next_message_request(self, time: float) -> float: ...  # awaits grant; returns granted time
    # Events
    def events(self) -> AsyncIterator["FederateEvent"]: ...

class ObjectInstance:
    handle: int
    class_name: str
    async def update_attributes(
        self, values: dict[str, object], timestamp: float | None = None,
    ) -> None: ...
```

#### 1.3.2 Layer 2 — standard adapter

```python
class Rti1516eAmbassador:
    """Mirrors IEEE 1516.1 Java/C++ ambassador conventions for users
    porting from Pitch / Portico / MAK. Internally wraps Layer 1."""

    async def create_federation_execution(self, name: str, fom_modules: list[Path]) -> None: ...
    async def join_federation_execution(
        self, federate_name: str, federation_name: str,
        fed_amb: "FederateAmbassador",
    ) -> int: ...
    # ... mirror of standard service calls ...

class FederateAmbassador:
    """User implements callbacks; bridge invokes."""
    def reflect_attribute_values(
        self, obj: int, attrs: dict[int, bytes], time: float | None,
    ) -> None: ...
    def receive_interaction(
        self, cls: int, params: dict[int, bytes], time: float | None,
    ) -> None: ...
    def time_advance_grant(self, time: float) -> None: ...
    def discover_object_instance(self, obj: int, cls: int, name: str) -> None: ...
    def remove_object_instance(self, obj: int) -> None: ...
    def federation_halted(self, cause: str) -> None: ...
```

#### 1.3.3 Errors

Inherit from `RtiError(Exception)`. Mapped 1:1 from wire `ERR_*` codes.

```python
class RtiError(Exception): ...
class FederationNotFound(RtiError): ...                # ERR_FED_NOT_FOUND
class FederationAlreadyExists(RtiError): ...           # ERR_FED_ALREADY_EXISTS
class FederateAlreadyJoined(RtiError): ...             # ERR_FED_ALREADY_JOINED
class ObjectNotFound(RtiError): ...                    # ERR_OBJ_NOT_FOUND
class FOMValidationError(RtiError):                    # ERR_FOM_*
    code: str  # "FOM-001" etc.
    line: int | None
class InvalidLookahead(RtiError): ...                  # ERR_TIME_INVALID_LOOKAHEAD
class WireVersionMismatch(RtiError): ...               # ERR_WIRE_VERSION_MISMATCH
# ...
```

### 1.4 pyjevsim Bridge Public API

Implements: FR-PYJ-1..4.

```python
@dataclass(frozen=True, slots=True)
class PortMapping:
    """Maps pyjevsim port names to FOM interaction class names."""
    outputs: dict[str, str]   # port_name -> interaction_class_name
    inputs: dict[str, str]    # interaction_class_name -> port_name

class HLAFederate:
    def __init__(
        self,
        coupled_model: "CoupledModel",  # pyjevsim type
        *,
        federation: FederationSpec,
        federate_name: str,
        port_mapping: PortMapping,
        rti_url: str,
    ) -> None: ...
    async def run(self, until: float | None = None) -> None: ...
    async def stop(self) -> None: ...
```

Contract:
- `run(until=T)` returns when logical time reaches T or the federation halts.
- pyjevsim's coupled model is driven through the bridge's event loop; users do not call pyjevsim's own scheduler directly while wrapped.
- Exceptions raised by user atomic models propagate, federate resigns cleanly, federation NOT halted (federate-local fault).

### 1.5 Configuration Interfaces

Implements: NFR-DEPLOY-1..2, NFR-OPS-1..2.

#### 1.5.1 RTI server flags / env

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `--listen` | `RTI_LISTEN` | `:8442` | gRPC listen address |
| `--metrics-listen` | `RTI_METRICS_LISTEN` | `:9090` | Prometheus HTTP listen |
| `--tls-cert` | `RTI_TLS_CERT` | (unset) | PEM cert path (optional TLS) |
| `--tls-key` | `RTI_TLS_KEY` | (unset) | PEM key path |
| `--eventlog-dir` | `RTI_EVENTLOG_DIR` | `./eventlogs/` | Event log directory |
| `--log-level` | `RTI_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `--log-format` | `RTI_LOG_FORMAT` | `json` | `json` / `text` |
| `--config` | `RTI_CONFIG` | (unset) | YAML config file path |

Federation-level config is per-`CreateFederationRequest`, not server-wide:
- `mode: verbose | best_effort` (REQUIRED, no default)
- `stall_timeout` (default `60s`)
- `seed` (default = hash of name + creation time)

#### 1.5.2 Federate (Python) configuration

| Construct | Source |
|---|---|
| RTI URL | `RtiConnection.connect(url=...)` arg, or env `RTI_URL` |
| TLS | `tls=True` arg + optional `ca_cert` |
| Federation spec | `FederationSpec` dataclass |

### 1.6 Event Log Binary Format

Implements: FR-EVT-1..3.

#### 1.6.1 Header (one per file, at offset 0)

```
magic       : 8 bytes = "KDRTI\0\1\0"
version     : uint32 BE = 1
fed_name_len: uint32 BE
fed_name    : UTF-8 bytes, length = fed_name_len
created_at  : uint64 BE (ns since epoch; informational only)
seed        : uint64 BE
mode        : uint8 (0=verbose, 1=best_effort)
reserved    : 7 bytes (zero)
```

#### 1.6.2 Record (repeating, after header)

```
record_len : uint32 BE = N
record     : N bytes = protobuf-encoded Event message
```

`Event` is a Protobuf message defined in `proto/rti/v1/eventlog.proto`:

```proto
message Event {
  uint64 seq = 1;            // monotonic, gapless
  uint64 wall_ns = 2;        // informational only, NOT used for ordering
  oneof body {
    FederateJoined fed_joined = 10;
    FederateResigned fed_resigned = 11;
    ObjectRegistered obj_registered = 12;
    ObjectDeleted obj_deleted = 13;
    AttributeUpdated attr_updated = 14;
    InteractionSent inter_sent = 15;
    TimeAdvanceRequested time_requested = 16;
    TimeAdvanceGranted time_granted = 17;
    FederationHalted halted = 99;
  }
}
```

Each `body` variant carries the deterministic state-mutating data needed for replay.

#### 1.6.3 Append protocol

- Each record is fully written before the writer advances. Partial trailing record on crash = ignored on read (length-prefix detection).
- fsync batch policy configurable; default 64 records or 100ms whichever first; final fsync at federation destroy.

### 1.7 Metrics Namespace (Prometheus)

Implements: NFR-OPS-2.

All metrics prefixed `rti_`. Standardized labels: `federation` (always), `federate_handle` (where applicable, integer as string), `class`, `interaction_class`, `type`.

| Metric | Type | Labels | Notes |
|---|---|---|---|
| `rti_messages_total` | counter | `type, federation` | type = `update` / `interaction` / `discover` / etc. |
| `rti_events_logged_total` | counter | `type, federation` | event log writes |
| `rti_advance_grant_latency_seconds` | histogram | `federation` | NER request → grant latency |
| `rti_message_size_bytes` | histogram | `type, federation` | wire size including envelope |
| `rti_federations` | gauge | (none) | currently created |
| `rti_federates` | gauge | `federation` | currently joined |
| `rti_objects` | gauge | `federation, class` | currently registered |
| `rti_event_log_batch_seconds` | histogram | `federation` | flush latency |

Histogram buckets: SI-friendly defaults (`5e-4, 1e-3, 5e-3, 1e-2, 5e-2, 1e-1, 5e-1, 1, 5`).

---

## 2. Internal Interfaces

Internal interfaces are seen by Go packages within the RTI process. They live in `rti/internal/core/` and are FROZEN — only the orchestrator may edit. Agents A and B implement against them.

### 2.1 `core.Clock`

Why: D-1 (no wall-clock dependence in core path).

```go
package core

type Clock interface {
    // Now returns the current wall time. Production uses time.Now;
    // tests inject a fake clock with explicit advancement.
    Now() time.Time
}
```

Implementations:
- `RealClock` — `time.Now()`. Used in production.
- `FakeClock` — manually advanced; used in unit tests and the determinism harness.

### 2.2 `core.FederationStore`

Implements: FR-FM-1..5.

```go
type FederationStore interface {
    CreateFederation(ctx context.Context, req CreateFederationRequest) error
    DestroyFederation(ctx context.Context, name string) error
    JoinFederation(ctx context.Context, req JoinFederationRequest) (FederateHandle, error)
    ResignFederation(ctx context.Context, fed string, h FederateHandle, action ResignAction) error
    Get(name string) (*Federation, error)
    List() []FederationName
}

type CreateFederationRequest struct {
    Name         string
    FOMModules   []FOMModule
    Mode         Mode  // Verbose | BestEffort
    StallTimeout time.Duration
    Seed         uint64
}

type FOMModule struct {
    Path     string  // optional, for diagnostics
    XMLBytes []byte  // canonical
}

type ResignAction uint8
const (
    UnconditionallyDivestAttributes ResignAction = iota
    // others: deferred to cut 2
)
```

Errors: `ErrFederationNotFound`, `ErrFederationAlreadyExists`, `ErrFederationHasFederatesJoined`, `ErrFOMValidation` (wraps `*fom.ValidationError`).

Threading: callers MAY call concurrently; `FederationStore` serializes per-federation internally.

### 2.3 `core.ObjectRegistry`

Implements: FR-OM-1..5.

```go
type ObjectRegistry interface {
    Register(ctx context.Context, fed FederationName, h FederateHandle,
        cls ObjectClassHandle, name string) (ObjectHandle, error)
    UpdateAttributes(ctx context.Context, fed FederationName, h FederateHandle,
        obj ObjectHandle, attrs map[AttributeHandle][]byte, ts *LogicalTime) error
    SendInteraction(ctx context.Context, fed FederationName, h FederateHandle,
        cls InteractionClassHandle, params map[ParameterHandle][]byte, ts *LogicalTime) error
    // Discover/Reflect/Receive are emitted via core.Outbox (see 2.7), not returned here.
}
```

Errors: `ErrObjectNotFound`, `ErrAttrNotOwned`, `ErrClassNotPublished`.

### 2.4 `core.TimeManager`

Implements: FR-TM-1..6.

```go
type TimeManager interface {
    EnableRegulation(ctx context.Context, fed FederationName, h FederateHandle, lookahead LogicalTime) error
    DisableRegulation(ctx context.Context, fed FederationName, h FederateHandle) error
    EnableConstrained(ctx context.Context, fed FederationName, h FederateHandle) error
    DisableConstrained(ctx context.Context, fed FederationName, h FederateHandle) error
    NextMessageRequest(ctx context.Context, fed FederationName, h FederateHandle, t LogicalTime) error
    // Grants emitted via core.Outbox.
}

type LogicalTime float64  // double per HLA Evolved time representation
```

Errors: `ErrInvalidLookahead`, `ErrTimeRequestInPast`, `ErrNotRegulating`.

### 2.5 `core.EventLog`

Implements: FR-EVT-1..3.

```go
type EventLog interface {
    Append(ctx context.Context, fed FederationName, evt *pb.Event) error
    Sync(ctx context.Context, fed FederationName) error
    Open(ctx context.Context, fed FederationName, mode OpenMode) (*Writer, error)
    Read(ctx context.Context, path string) (*Reader, error)
}

type OpenMode uint8
const (
    OpenWrite OpenMode = iota
    OpenReplay
)

type Reader interface {
    io.Closer
    Header() *Header
    Next() (*pb.Event, error)  // returns io.EOF when done
}
```

The `Reader` MUST iterate events in stored order with no skipping. Replayer reads with this; never random-access.

### 2.6 `core.FOMRepository`

Implements: FR-FOM-1..4.

```go
type FOMRepository interface {
    Load(ctx context.Context, modules []FOMModule) (*model.FOM, error)
    Get(fed FederationName) (*model.FOM, error)
}
```

Returns immutable `*model.FOM`; callers MUST NOT mutate.

### 2.7 `core.Outbox`

Why: server-initiated callbacks (Discover/Reflect/Receive/Grant) need a uniform delivery channel.

```go
type Outbox interface {
    // Send delivers an event to the federate's outbound stream.
    // Blocks if the federate is slow (bounded buffer); on overflow,
    // marks the federate as crashed (per NFR-CRASH-1) and returns ErrFederateOverflow.
    Send(ctx context.Context, fed FederationName, h FederateHandle, evt *pb.FederateEvent) error
}
```

### 2.8 `core.Codec` (Encoding entry point)

Implements: FR-ENC-1..2. Lives in `rti/pkg/encoding`, surfaced via `core` for injection.

```go
type Codec interface {
    Encode(v any) ([]byte, error)
    Decode(b []byte) (v any, n int, err error)
    OctetBoundary() int
}

type CodecFactory interface {
    For(dt model.DataType) (Codec, error)
}
```

Errors: `ErrInsufficientBytes`, `ErrTypeMismatch`, `ErrPaddingViolation`.

### 2.9 Handle Types

All handles are typed integers, never raw `int`/`uint64`. Defined in `rti/internal/core/handles.go`:

```go
type FederationName string

type FederateHandle uint64
type ObjectHandle uint64
type ObjectClassHandle uint64
type AttributeHandle uint64
type InteractionClassHandle uint64
type ParameterHandle uint64

const InvalidFederateHandle FederateHandle = 0
// (and so on for each)
```

Handle 0 is reserved as "invalid"; first valid handle is 1.

---

## 3. Inter-Package Dependency Rules

```
                 +---------------------+
                 | rti/cmd/rtid (main) |
                 +----------+----------+
                            | imports
                            v
+-----------------+   +-----+-----+   +------------------+
| rti/internal/   |   |           |   | rti/internal/    |
| transport/grpc  +-->|           |<--+ federation, decl |
+-----------------+   |  core/    |   | object, time,    |
                      | (interfac.|   | eventlog impls   |
                      |  + types) |   +------------------+
                      |           |
                      +-----+-----+
                            ^
                            | imports (read-only)
                            |
              +-------------+-------------+
              |                           |
        +-----+------+              +-----+------+
        | rti/pkg/   |              | rti/pkg/   |
        | encoding   |              | fom        |
        +------------+              +------------+
```

Rules:

| Package | May import |
|---|---|
| `rti/cmd/rtid` | All `rti/internal/*`, `rti/pkg/*` |
| `rti/internal/transport/grpc` | `rti/internal/core`, `proto/rti/v1` |
| `rti/internal/{federation,declaration,object,time,eventlog}` | `rti/internal/core`, `rti/pkg/*` |
| `rti/internal/core` | stdlib, `proto/rti/v1`, `rti/pkg/*` |
| `rti/pkg/encoding` | stdlib, `rti/pkg/fom/model` |
| `rti/pkg/fom/{parser,mim,model}` | stdlib only (no `rti/internal/*`) |

Violations are linter-enforced via `depguard`.

`rti/pkg/*` MUST be importable as a standalone library by external Go projects. No transitive deps on `rti/internal/*`.

---

## 4. Versioning Policy

- **Pre-1.0 (current)**: breaking changes allowed in any interface. Wire version field still present so federates can fail loudly.
- **Post-1.0**: SemVer. Minor = additive interfaces. Major = breaking. Wire version bumps require migration plan.
- **Proto changes**: governed by Buf breaking-change detection in CI.
- **Go core interface changes**: governed by `apidiff` in CI; agent PRs that touch `rti/internal/core/` are rejected by the frozen-paths hook.

---

## 5. Open Items

These interfaces are NOT YET designed; they belong to deferred features (SRS §9):

- Ownership Management interfaces.
- DDM region interfaces.
- Save/Restore protocol.
- Full MOM interfaces.
- DDS data plane (cut 2 SDD addendum).

When their cuts are scheduled, this IDD gains addenda; existing interfaces above remain stable.
