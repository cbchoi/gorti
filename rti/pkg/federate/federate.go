// TASK-205½ (M21) — Federate SDK foundation. See docs/M21_DISPATCH_PLAN.md §2.7.
//
// This file holds the public Connection + Federate types, lifecycle
// methods (Connect/Close/JoinFederation/Resign), and the events-drain
// goroutine. FOM handle resolution lives in handles.go; per-RPC
// dispatchers live in declaration.go and interaction.go.

package federate

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// Connection wraps a gRPC channel to rtid + the cut-1 service stubs.
// One Connection MAY host multiple federates; the cut-3 / M21 happy
// path is one Federate per Connection.
type Connection struct {
	generationMu sync.RWMutex
	generations  map[string]uint64

	cc                           *grpc.ClientConn
	fed                          rtiv1.FederationServiceClient
	decl                         rtiv1.DeclarationServiceClient
	obj                          rtiv1.ObjectServiceClient
	confirmedObject              rtiv1.ConfirmedObjectServiceClient
	localLRC                     rtiv1.LocalLRCServiceClient
	stream                       rtiv1.StreamServiceClient
	sync                         rtiv1.SyncServiceClient
	interactionStreamEnabled     bool
	confirmedObjectStreamEnabled bool
	localLRCEnabled              bool
	localLRCQueueCapacity        int
	localLRCAckEvery             uint32
	localLRCBatchSize            uint32
	callbackRepresentation       CallbackRepresentation
	receiveOrderTransport        ReceiveOrderTransport
	tm                           rtiv1.TimeServiceClient    // wired here so time.go (TASK-206) can use it
	ddm                          rtiv1.DDMServiceClient     // M23 W4 — Go SDK DDM coverage
	cluster                      rtiv1.ClusterServiceClient // M15.2 + M16.2 — cross-node redirect / reconnect
}

// FederationSpec describes a federation to create-or-join.
type FederationSpec struct {
	Name       string
	FOMModules []FOMModule
	Seed       uint64
	// StallTimeoutSeconds is optional. Zero → server default (60s).
	StallTimeoutSeconds float64
}

// FOMModule is one FOM XML module submitted at federation create time.
type FOMModule struct {
	Path string
	XML  []byte
}

// Federate represents a federate that has joined a federation.
// Resign() MUST be called to cleanly leave; cancelling the context
// is not sufficient.
type Federate struct {
	conn                 *Connection
	federationName       string
	federateName         string
	federateHandle       uint64
	federationGeneration uint64

	// FOM-derived handle tables built once at JoinFederation time. The
	// SDK resolves string class/parameter names to wire handles via
	// these tables on every Publish/Subscribe/Send call.
	handles *fomTables

	// Events plumbing.
	eventCh chan Event
	// streamCancel cancels the events-drain goroutine's context.
	streamCancel context.CancelFunc
	// drainDone is closed by the drain goroutine on exit so Resign
	// can wait for it.
	drainDone chan struct{}

	// resignOnce gates Resign so a second call is a no-op.
	resignOnce sync.Once

	mu            sync.Mutex
	objectClasses map[uint64]uint64 // object handle -> object class handle
	// The LocalLRC admission path mirrors declaration and ownership state
	// established by confirmed RTI calls. These maps are guarded by mu.
	publishedObjectAttributes map[uint64]map[uint64]struct{}
	publishedInteractions     map[uint64]struct{}
	ownedObjectAttributes     map[uint64]map[uint64]struct{}

	interactionStreamMu              sync.Mutex
	interactionStream                *interactionRPCStream
	confirmedObjectStream            *confirmedObjectRPCStream
	interactionStreamUnsupported     bool
	confirmedObjectStreamUnsupported bool
	interactionContext               context.Context
	interactionCancel                context.CancelFunc
	interactionClosing               atomic.Bool
	interactionStats                 interactionTransportCounters
	confirmedObjectStats             interactionTransportCounters

	localLRCMu          sync.Mutex
	localLRC            *localLRC
	localLRCClosing     atomic.Bool
	localLRCUnsupported atomic.Bool
}

type interactionTransportCounters struct {
	total, streamSent, streamAcked, unarySent, unaryAcked   atomic.Uint64
	openAttempts, openSuccesses, resets, indeterminate      atomic.Uint64
	fallbackDisabled, fallbackMetadata, fallbackUnsupported atomic.Uint64
}

// InteractionTransportStats attests which transport handled interaction
// calls without exposing mutable counters.
type InteractionTransportStats struct {
	Total, StreamSent, StreamAcked, UnarySent, UnaryAcked   uint64
	OpenAttempts, OpenSuccesses, Resets, Indeterminate      uint64
	FallbackDisabled, FallbackMetadata, FallbackUnsupported uint64
}

func (f *Federate) InteractionTransportStats() InteractionTransportStats {
	if f == nil {
		return InteractionTransportStats{}
	}
	s := &f.interactionStats
	return InteractionTransportStats{
		Total: s.total.Load(), StreamSent: s.streamSent.Load(), StreamAcked: s.streamAcked.Load(),
		UnarySent: s.unarySent.Load(), UnaryAcked: s.unaryAcked.Load(),
		OpenAttempts: s.openAttempts.Load(), OpenSuccesses: s.openSuccesses.Load(),
		Resets: s.resets.Load(), Indeterminate: s.indeterminate.Load(),
		FallbackDisabled: s.fallbackDisabled.Load(), FallbackMetadata: s.fallbackMetadata.Load(),
		FallbackUnsupported: s.fallbackUnsupported.Load(),
	}
}

// ConfirmedObjectTransportStats reports use of the shared synchronous
// UpdateAttributeValues/SendInteraction stream. Total and Stream* include
// both operations, unlike InteractionTransportStats which remains available
// for compatibility with older instrumentation.
func (f *Federate) ConfirmedObjectTransportStats() InteractionTransportStats {
	if f == nil {
		return InteractionTransportStats{}
	}
	s := &f.confirmedObjectStats
	return InteractionTransportStats{
		Total: s.total.Load(), StreamSent: s.streamSent.Load(), StreamAcked: s.streamAcked.Load(),
		UnarySent: s.unarySent.Load(), UnaryAcked: s.unaryAcked.Load(),
		OpenAttempts: s.openAttempts.Load(), OpenSuccesses: s.openSuccesses.Load(),
		Resets: s.resets.Load(), Indeterminate: s.indeterminate.Load(),
		FallbackDisabled: s.fallbackDisabled.Load(), FallbackMetadata: s.fallbackMetadata.Load(),
		FallbackUnsupported: s.fallbackUnsupported.Load(),
	}
}

// ConnectOptions — M14 W2. Per-connection auth + transport tuning.
type ConnectOptions struct {
	// TLS — server identity verification + optional client cert.
	// Nil → insecure (current default). For mTLS, set
	// TLS.Certificates with the client cert pair.
	TLS *tls.Config

	// BearerToken — sent as `authorization: Bearer <token>` on every
	// RPC. Combinable with TLS. Empty → no token.
	BearerToken string

	// BearerTokenProvider — refreshable token source. Called per-RPC.
	// Empty BearerToken with non-nil Provider → Provider wins.
	// Both empty/nil → no token.
	BearerTokenProvider func(ctx context.Context) (string, error)

	// Transport tunes the gRPC channel itself (buffer and HTTP/2 flow
	// control window sizes). These are first-class knobs translated
	// into DialOptions by ConnectWithOptions — production callers
	// should use them instead of ExtraDialOptions for channel tuning.
	// The zero value appends no DialOptions: gRPC library defaults
	// (32KB buffers) and BDP-autotuned windows stay in effect, and
	// dial behavior is byte-identical to earlier SDKs. Setting
	// Transport options does NOT affect transport selection — the
	// streaming and LocalLRC fast paths stay enabled.
	Transport TransportOptions

	// ExtraDialOptions are appended to the gRPC DialOptions before
	// grpc.NewClient is called. Production callers use this for dial
	// tuning (keepalive, window sizes, custom resolvers); tests use it
	// to inject grpc.WithContextDialer for bufconn / service-mesh
	// fixtures so the redirect-follow path can reach in-process servers
	// without a real network round-trip. Setting ExtraDialOptions does
	// NOT affect transport selection: the streaming and LocalLRC fast
	// paths stay enabled regardless of dial tuning.
	ExtraDialOptions []grpc.DialOption

	// AdmissionMode selects the completion contract for standard
	// receive-order Object Management calls
	// (UpdateAttributeValues*/SendInteraction*).
	//
	//   - AdmissionModePipelined (the default; the empty string selects
	//     it): calls return after bounded LocalLRC local admission. IEEE
	//     1516 requires no server confirmation for receive-order
	//     delivery, so this is the standard production mode.
	//   - AdmissionModeConfirmed: every standard receive-order call
	//     waits for the server result over the unary confirmed path.
	//     This is a documented DEBUG option for isolating admission
	//     behavior; it forces the LocalLRC fast path and the shared
	//     synchronous streams off for this Connection.
	//
	// Any other value is rejected by ConnectWithOptions.
	AdmissionMode string

	// LocalLRCQueueCapacity bounds both queued and in-flight receive-order
	// Object Management operations. Zero selects the SDK default.
	LocalLRCQueueCapacity int

	// LocalLRCAckEvery requests a cumulative ACK interval. Zero selects the
	// SDK default; the server may clamp the value.
	LocalLRCAckEvery uint32

	// LocalLRCBatchSize requests the maximum number of receive-order
	// operations per transport frame. Zero selects the compatibility default
	// of 32. Supported explicit values are 32, 64, 128, and 256.
	LocalLRCBatchSize uint32

	// CallbackRepresentation selects name-oriented callbacks or direct
	// handle-oriented callbacks. The zero value preserves the existing
	// name-oriented SDK surface.
	CallbackRepresentation CallbackRepresentation

	// ReceiveOrderTransport selects the completion boundary of standard
	// untimed Object Management calls. The zero value uses LocalLRC local
	// admission; Confirmed waits for the server result on every call.
	// Prefer AdmissionMode for new code; AdmissionModeConfirmed implies
	// ReceiveOrderTransportConfirmed and additionally forces the unary
	// path.
	ReceiveOrderTransport ReceiveOrderTransport
}

// TransportOptions carries the W8 gRPC channel tuning knobs. Every
// field's zero value means "leave the gRPC library default in effect"
// — no DialOption is appended for it. Negative values are rejected by
// ConnectWithOptions.
type TransportOptions struct {
	// WriteBufferSize sets the per-connection gRPC write buffer in
	// bytes (grpc.WithWriteBufferSize). 0 = library default (32KB).
	WriteBufferSize int

	// ReadBufferSize sets the per-connection gRPC read buffer in
	// bytes (grpc.WithReadBufferSize). 0 = library default (32KB).
	ReadBufferSize int

	// InitialWindowSize sets the HTTP/2 initial per-stream flow
	// control window in bytes (grpc.WithInitialWindowSize). 0 keeps
	// the library's BDP-autotuned windows — setting any positive
	// value DISABLES BDP autotuning, so only set this from measured
	// evidence. gRPC ignores values below 64KB.
	InitialWindowSize int32

	// InitialConnWindowSize sets the HTTP/2 initial per-connection
	// flow control window in bytes (grpc.WithInitialConnWindowSize).
	// Same BDP-autotune caveat as InitialWindowSize.
	InitialConnWindowSize int32
}

// dialOptions translates the set knobs into first-class DialOptions.
// The zero value returns nil — nothing appended.
func (o TransportOptions) dialOptions() []grpc.DialOption {
	var opts []grpc.DialOption
	if o.WriteBufferSize > 0 {
		opts = append(opts, grpc.WithWriteBufferSize(o.WriteBufferSize))
	}
	if o.ReadBufferSize > 0 {
		opts = append(opts, grpc.WithReadBufferSize(o.ReadBufferSize))
	}
	if o.InitialWindowSize > 0 {
		opts = append(opts, grpc.WithInitialWindowSize(o.InitialWindowSize))
	}
	if o.InitialConnWindowSize > 0 {
		opts = append(opts, grpc.WithInitialConnWindowSize(o.InitialConnWindowSize))
	}
	return opts
}

func (o TransportOptions) validate() error {
	if o.WriteBufferSize < 0 || o.ReadBufferSize < 0 ||
		o.InitialWindowSize < 0 || o.InitialConnWindowSize < 0 {
		return errors.New("federate: Transport options must not be negative")
	}
	return nil
}

// ReceiveOrderTransport selects the completion boundary for standard
// receive-order UpdateAttributeValues and SendInteraction calls.
type ReceiveOrderTransport uint8

const (
	ReceiveOrderTransportLocalLRC ReceiveOrderTransport = iota
	ReceiveOrderTransportConfirmed
)

// Admission modes accepted by ConnectOptions.AdmissionMode.
//
// AdmissionModePipelined is the SDK default: standard receive-order
// Object Management calls complete on LocalLRC local admission (IEEE
// 1516 requires no server confirmation for receive-order delivery).
//
// AdmissionModeConfirmed is a documented debug option: every standard
// receive-order call waits for the server result on the unary confirmed
// path. It forces the LocalLRC fast path off (localLRCEnabled=false).
const (
	AdmissionModePipelined = "pipelined"
	AdmissionModeConfirmed = "confirmed"
)

// CallbackRepresentation selects how object and interaction callback maps are
// exposed. Handle callbacks avoid a second name projection and payload copy.
type CallbackRepresentation uint8

const (
	CallbackRepresentationNames CallbackRepresentation = iota
	CallbackRepresentationHandles
)

// Connect dials rtid at addr (host:port) with insecure credentials.
// Use ConnectWithOptions for TLS / bearer-token configurations.
func Connect(ctx context.Context, addr string) (*Connection, error) {
	return ConnectWithOptions(ctx, addr, ConnectOptions{})
}

// WrapGRPCClientConn builds a Connection over an externally-dialed
// gRPC channel. Useful for advanced callers that supply a custom
// dialer (bufconn for in-process tests, alternative resolver,
// service-mesh sidecar) — the federate SDK then operates over the
// provided channel without re-dialing.
//
// Caller owns the channel's lifecycle: Close() on the returned
// Connection releases the underlying *grpc.ClientConn.
func WrapGRPCClientConn(cc *grpc.ClientConn) *Connection {
	return &Connection{
		generations:                  make(map[string]uint64),
		cc:                           cc,
		fed:                          rtiv1.NewFederationServiceClient(cc),
		decl:                         rtiv1.NewDeclarationServiceClient(cc),
		obj:                          rtiv1.NewObjectServiceClient(cc),
		confirmedObject:              rtiv1.NewConfirmedObjectServiceClient(cc),
		localLRC:                     rtiv1.NewLocalLRCServiceClient(cc),
		confirmedObjectStreamEnabled: true,
		localLRCEnabled:              true,
		localLRCQueueCapacity:        defaultLocalLRCQueueCapacity,
		localLRCAckEvery:             defaultLocalLRCAckEvery,
		localLRCBatchSize:            defaultLocalLRCBatchSize,
		receiveOrderTransport:        ReceiveOrderTransportLocalLRC,
		stream:                       rtiv1.NewStreamServiceClient(cc),
		sync:                         rtiv1.NewSyncServiceClient(cc),
		tm:                           rtiv1.NewTimeServiceClient(cc),
		ddm:                          rtiv1.NewDDMServiceClient(cc),
		cluster:                      rtiv1.NewClusterServiceClient(cc),
	}
}

// ConnectWithOptions dials rtid at addr with the given auth options.
// Caller MUST call Close() when done.
//
// M14 W2: opts.TLS nil → insecure (matches Connect). opts.TLS non-nil
// → grpc.WithTransportCredentials(credentials.NewTLS(opts.TLS)). Bearer
// token (literal or via Provider) attaches via PerRPCCredentials.
func ConnectWithOptions(ctx context.Context, addr string, opts ConnectOptions) (*Connection, error) {
	if opts.ReceiveOrderTransport > ReceiveOrderTransportConfirmed {
		return nil, errors.New("federate: invalid receive-order transport")
	}
	receiveOrderTransport := opts.ReceiveOrderTransport
	confirmedAdmission := false
	switch opts.AdmissionMode {
	case "", AdmissionModePipelined:
		// Pipelined LocalLRC admission is the default. An explicit
		// ReceiveOrderTransportConfirmed remains honored for
		// compatibility, but contradicting an explicit
		// AdmissionModePipelined is an error.
		if opts.AdmissionMode == AdmissionModePipelined &&
			opts.ReceiveOrderTransport == ReceiveOrderTransportConfirmed {
			return nil, errors.New("federate: AdmissionMode \"pipelined\" conflicts with ReceiveOrderTransportConfirmed")
		}
	case AdmissionModeConfirmed:
		receiveOrderTransport = ReceiveOrderTransportConfirmed
		confirmedAdmission = true
	default:
		return nil, fmt.Errorf("federate: invalid admission mode %q", opts.AdmissionMode)
	}
	if opts.LocalLRCQueueCapacity < 0 {
		return nil, errors.New("federate: LocalLRC queue capacity must not be negative")
	}
	if !validLocalLRCBatchSize(opts.LocalLRCBatchSize) {
		return nil, errors.New("federate: LocalLRC batch size must be 32, 64, 128, or 256")
	}
	if opts.CallbackRepresentation > CallbackRepresentationHandles {
		return nil, errors.New("federate: invalid callback representation")
	}
	if err := opts.Transport.validate(); err != nil {
		return nil, err
	}
	queueCapacity := opts.LocalLRCQueueCapacity
	if queueCapacity == 0 {
		queueCapacity = defaultLocalLRCQueueCapacity
	}
	ackEvery := opts.LocalLRCAckEvery
	if ackEvery == 0 {
		ackEvery = defaultLocalLRCAckEvery
	}
	if ackEvery > uint32(queueCapacity) {
		ackEvery = uint32(queueCapacity)
	}
	batchSize := opts.LocalLRCBatchSize
	if batchSize == 0 {
		batchSize = defaultLocalLRCBatchSize
	}
	dialOpts := []grpc.DialOption{}
	// W8 — first-class channel tuning. Zero-value Transport appends
	// nothing; these options never influence streamEligible below.
	dialOpts = append(dialOpts, opts.Transport.dialOptions()...)
	if opts.TLS != nil {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(opts.TLS)))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	if opts.BearerToken != "" || opts.BearerTokenProvider != nil {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(bearerCreds{
			token:    opts.BearerToken,
			provider: opts.BearerTokenProvider,
			// requireTLS: only require TLS when TLS is actually
			// configured. Insecure + bearer is allowed for tests
			// (real deployments should pair them).
			requireTLS: opts.TLS != nil,
		}))
	}
	if len(opts.ExtraDialOptions) > 0 {
		dialOpts = append(dialOpts, opts.ExtraDialOptions...)
	}
	cc, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("federate: dial %s: %w", addr, err)
	}
	// streamEligible gates the shared synchronous streams and the
	// LocalLRC fast path. It depends ONLY on the auth options: per-RPC
	// bearer credentials are not yet propagated onto the long-lived
	// streams, so token configurations stay on the unary path. Dial
	// tuning via ExtraDialOptions deliberately does NOT downgrade
	// transport selection.
	streamEligible := opts.BearerToken == "" && opts.BearerTokenProvider == nil
	// AdmissionModeConfirmed is a debug mode: force the unary confirmed
	// path by disabling the LocalLRC fast path and the shared streams.
	streamEnabled := streamEligible && !confirmedAdmission
	_ = ctx
	return &Connection{
		generations:                  make(map[string]uint64),
		cc:                           cc,
		fed:                          rtiv1.NewFederationServiceClient(cc),
		decl:                         rtiv1.NewDeclarationServiceClient(cc),
		obj:                          rtiv1.NewObjectServiceClient(cc),
		confirmedObject:              rtiv1.NewConfirmedObjectServiceClient(cc),
		localLRC:                     rtiv1.NewLocalLRCServiceClient(cc),
		interactionStreamEnabled:     streamEnabled,
		confirmedObjectStreamEnabled: streamEnabled,
		localLRCEnabled:              streamEnabled,
		localLRCQueueCapacity:        queueCapacity,
		localLRCAckEvery:             ackEvery,
		localLRCBatchSize:            batchSize,
		callbackRepresentation:       opts.CallbackRepresentation,
		receiveOrderTransport:        receiveOrderTransport,
		stream:                       rtiv1.NewStreamServiceClient(cc),
		sync:                         rtiv1.NewSyncServiceClient(cc),
		tm:                           rtiv1.NewTimeServiceClient(cc),
		ddm:                          rtiv1.NewDDMServiceClient(cc),
		cluster:                      rtiv1.NewClusterServiceClient(cc),
	}, nil
}

func validLocalLRCBatchSize(size uint32) bool {
	switch size {
	case 0, 32, 64, 128, 256:
		return true
	default:
		return false
	}
}

// bearerCreds satisfies grpc.PerRPCCredentials for the M14 W2
// bearer-token path. token / provider are mutually exclusive at the
// API surface; provider wins when both set.
type bearerCreds struct {
	token      string
	provider   func(ctx context.Context) (string, error)
	requireTLS bool
}

func (c bearerCreds) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	tok := c.token
	if c.provider != nil {
		t, err := c.provider(ctx)
		if err != nil {
			return nil, fmt.Errorf("federate: bearer token provider: %w", err)
		}
		tok = t
	}
	if tok == "" {
		return nil, nil
	}
	return map[string]string{"authorization": "Bearer " + tok}, nil
}

func (c bearerCreds) RequireTransportSecurity() bool { return c.requireTLS }

// Close releases the gRPC connection. Open Federates created from
// this Connection should be Resigned first; Close() does NOT auto-resign.
func (c *Connection) Close() error {
	if c == nil || c.cc == nil {
		return nil
	}
	return c.cc.Close()
}

// DestroyFederation destroys an empty federation execution. It is deliberately
// non-idempotent: a second call returns the server's not-found error.
func (c *Connection) DestroyFederation(ctx context.Context, federationName string) error {
	if c == nil || c.cc == nil || c.fed == nil {
		return errors.New("federate: Connection is closed")
	}
	generation, err := c.resolveFederationGeneration(ctx, federationName)
	if err != nil {
		return err
	}
	_, err = c.fed.DestroyFederation(ctx, &rtiv1.DestroyFederationRequest{
		WireVersion:                  rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:               federationName,
		ExpectedFederationGeneration: &generation,
	})
	if err == nil {
		c.generationMu.Lock()
		delete(c.generations, federationName)
		c.generationMu.Unlock()
	}
	return wrapStatusErr(err)
}

func (c *Connection) resolveFederationGeneration(ctx context.Context, federationName string) (uint64, error) {
	c.generationMu.RLock()
	generation, ok := c.generations[federationName]
	c.generationMu.RUnlock()
	if ok {
		return generation, nil
	}
	resp, err := c.fed.ListFederations(ctx, &rtiv1.ListFederationsRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err != nil {
		return 0, wrapStatusErr(err)
	}
	for _, summary := range resp.GetFederations() {
		if summary.GetName() != federationName {
			continue
		}
		generation = summary.GetFederationGeneration()
		c.generationMu.Lock()
		if c.generations == nil {
			c.generations = make(map[string]uint64)
		}
		c.generations[federationName] = generation
		c.generationMu.Unlock()
		return generation, nil
	}
	return 0, fmt.Errorf("federate: federation %q does not exist", federationName)
}

// JoinFederation creates the federation if it does not exist
// (idempotent — ALREADY_EXISTS swallowed) and joins it under the
// given federate name.
//
// On success, spawns a background goroutine that drains
// StreamService.Events into the buffered Events() channel. The
// goroutine exits cleanly on Resign(). The channel buffer is sized
// per the server-side default (256 events); callers should drain
// promptly to avoid backpressure into the gRPC stream.
func (c *Connection) JoinFederation(
	ctx context.Context, spec FederationSpec, federateName string,
) (*Federate, error) {
	if c == nil || c.cc == nil {
		return nil, errors.New("federate: Connection is closed")
	}

	// Build the proto FOM modules + parse locally for handle resolution.
	protoMods := make([]*rtiv1.FOMModule, len(spec.FOMModules))
	parserMods := make([]fomParseModule, len(spec.FOMModules))
	for i, m := range spec.FOMModules {
		protoMods[i] = &rtiv1.FOMModule{Path: m.Path, Xml: m.XML}
		parserMods[i] = fomParseModule{Path: m.Path, XML: m.XML}
	}
	tables, err := buildFOMTables(parserMods)
	if err != nil {
		return nil, fmt.Errorf("federate: parse FOM: %w", err)
	}

	// 1. Create federation (idempotent).
	createReq := &rtiv1.CreateFederationRequest{
		WireVersion:         rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:      spec.Name,
		FomModules:          protoMods,
		Seed:                spec.Seed,
		StallTimeoutSeconds: spec.StallTimeoutSeconds,
	}
	createResp, cErr := c.fed.CreateFederation(ctx, createReq)
	var generation uint64
	if cErr != nil {
		if status.Code(cErr) != codes.AlreadyExists {
			return nil, fmt.Errorf("federate: CreateFederation: %w", cErr)
		}
		generation, err = c.resolveFederationGeneration(ctx, spec.Name)
		if err != nil {
			return nil, fmt.Errorf("federate: resolve federation generation: %w", err)
		}
	} else {
		generation = createResp.GetFederationGeneration()
		c.generationMu.Lock()
		if c.generations == nil {
			c.generations = make(map[string]uint64)
		}
		c.generations[spec.Name] = generation
		c.generationMu.Unlock()
	}

	// 2. Join.
	joinResp, err := c.fed.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
		WireVersion:                  rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:               spec.Name,
		FederateName:                 federateName,
		ExpectedFederationGeneration: &generation,
	})
	if err != nil {
		return nil, fmt.Errorf("federate: JoinFederation: %w", err)
	}

	// 3. Open the batched callback stream + spawn drainer.
	streamCtx, streamCancel := context.WithCancel(context.Background())
	eventsRequest := &rtiv1.EventsRequest{
		WireVersion:                  rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:               spec.Name,
		FederateHandle:               joinResp.GetFederateHandle(),
		ExpectedFederationGeneration: &generation,
	}
	batchStream, batchErr := c.stream.EventBatches(streamCtx, eventsRequest)
	var legacyStream grpc.ServerStreamingClient[rtiv1.FederateEvent]
	failCallbackInitialization := func(callbackErr error) (*Federate, error) {
		streamCancel()
		return nil, c.compensateFailedJoin(ctx, spec.Name, joinResp.GetFederateHandle(), callbackErr)
	}
	openLegacy := func(unimplementedErr error) error {
		var legacyErr error
		legacyStream, legacyErr = c.stream.Events(streamCtx, eventsRequest)
		if legacyErr == nil {
			return nil
		}
		return fmt.Errorf("federate: callback stream open: %w", errors.Join(
			fmt.Errorf("EventBatches unsupported: %w", unimplementedErr),
			fmt.Errorf("Events fallback: %w", legacyErr),
		))
	}

	if batchErr != nil {
		if status.Code(batchErr) != codes.Unimplemented {
			return failCallbackInitialization(fmt.Errorf("federate: EventBatches callback stream open: %w", batchErr))
		}
		if err := openLegacy(batchErr); err != nil {
			return failCallbackInitialization(err)
		}
		batchStream = nil
	} else {
		ready, readyErr := receiveCallbackReady(ctx, streamCancel, batchStream)
		if readyErr != nil {
			if status.Code(readyErr) == codes.Unimplemented {
				if err := openLegacy(readyErr); err != nil {
					return failCallbackInitialization(err)
				}
				batchStream = nil
			} else {
				return failCallbackInitialization(fmt.Errorf("federate: EventBatches ready: %w", readyErr))
			}
		} else if ready == nil || !ready.GetReady() || len(ready.GetEvents()) != 0 {
			return failCallbackInitialization(errors.New(
				"federate: EventBatches ready: server did not send an empty ready batch",
			))
		}
	}

	interactionContext, interactionCancel := context.WithCancel(context.Background())
	f := &Federate{
		conn:                      c,
		federationName:            spec.Name,
		federateName:              federateName,
		federateHandle:            joinResp.GetFederateHandle(),
		federationGeneration:      generation,
		handles:                   tables,
		eventCh:                   make(chan Event, 256),
		streamCancel:              streamCancel,
		drainDone:                 make(chan struct{}),
		objectClasses:             make(map[uint64]uint64),
		publishedObjectAttributes: make(map[uint64]map[uint64]struct{}),
		publishedInteractions:     make(map[uint64]struct{}),
		ownedObjectAttributes:     make(map[uint64]map[uint64]struct{}),
		interactionContext:        interactionContext,
		interactionCancel:         interactionCancel,
	}
	if batchStream != nil {
		go f.drainEventBatches(batchStream)
	} else {
		go f.drainEvents(legacyStream)
	}
	return f, nil
}

type callbackReadyResult struct {
	batch *rtiv1.FederateEventBatch
	err   error
}

func receiveCallbackReady(
	ctx context.Context,
	streamCancel context.CancelFunc,
	stream grpc.ServerStreamingClient[rtiv1.FederateEventBatch],
) (*rtiv1.FederateEventBatch, error) {
	if stream == nil {
		return nil, errors.New("callback batch stream is nil")
	}
	result := make(chan callbackReadyResult, 1)
	go func() {
		batch, err := stream.Recv()
		result <- callbackReadyResult{batch: batch, err: err}
	}()
	select {
	case received := <-result:
		return received.batch, received.err
	case <-ctx.Done():
		streamCancel()
		return nil, ctx.Err()
	}
}

func (c *Connection) compensateFailedJoin(
	ctx context.Context,
	federationName string,
	federateHandle uint64,
	originalErr error,
) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_, resignErr := c.fed.ResignFederation(cleanupCtx, &rtiv1.ResignFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: federationName,
		FederateHandle: federateHandle,
		Action:         rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
	})
	if resignErr == nil {
		return originalErr
	}
	return errors.Join(originalErr, fmt.Errorf("federate: compensating ResignFederation: %w", resignErr))
}

// Handle returns the federate handle assigned by rtid at join time.
func (f *Federate) Handle() uint64 { return f.federateHandle }

// Name returns the federate name passed to JoinFederation.
func (f *Federate) Name() string { return f.federateName }

// Events returns a receive-only channel of incoming events. The
// channel closes when Resign() completes or rtid drops the stream.
func (f *Federate) Events() <-chan Event { return f.eventCh }

// ResignAction — IEEE 1516.1-2010 §4.10. M24 expanded the accepted
// set from 1 to 6.
type ResignAction uint8

const (
	ResignActionUnspecified ResignAction = iota
	ResignActionUnconditionallyDivestAttributes
	ResignActionDeleteThenDivest
	ResignActionCancelThenDelete
	ResignActionCancelPendingOwnership
	ResignActionNoAction
	ResignActionDeleteObjects
)

func (a ResignAction) wire() rtiv1.ResignAction {
	switch a {
	case ResignActionUnconditionallyDivestAttributes:
		return rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES
	case ResignActionDeleteThenDivest:
		return rtiv1.ResignAction_RESIGN_ACTION_DELETE_THEN_DIVEST
	case ResignActionCancelThenDelete:
		return rtiv1.ResignAction_RESIGN_ACTION_CANCEL_THEN_DELETE
	case ResignActionCancelPendingOwnership:
		return rtiv1.ResignAction_RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP
	case ResignActionNoAction:
		return rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION
	case ResignActionDeleteObjects:
		return rtiv1.ResignAction_RESIGN_ACTION_DELETE_OBJECTS
	default:
		return rtiv1.ResignAction_RESIGN_ACTION_UNSPECIFIED
	}
}

// Resign sends ResignFederation to rtid with the default action
// (UnconditionallyDivestAttributes). Use ResignWithAction to pass a
// different action. Idempotent — second call is a no-op.
func (f *Federate) Resign(ctx context.Context) error {
	return f.ResignWithAction(ctx, ResignActionUnconditionallyDivestAttributes)
}

// FederationMember — IEEE 1516.1-2010 §4.8 (M24 W3) entry.
type FederationMember struct {
	Handle       uint64
	Name         string
	FederateType string
}

// ListFederationMembers — IEEE 1516.1-2010 §4.8 (M24 W3). Returns
// every joined federate's (handle, name, type) for the federation
// in handle-ascending order.
func (f *Federate) ListFederationMembers(ctx context.Context) ([]FederationMember, error) {
	resp, err := f.conn.fed.ListFederationMembers(ctx, &rtiv1.ListFederationMembersRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: f.federationName,
	})
	if err != nil {
		return nil, wrapStatusErr(err)
	}
	out := make([]FederationMember, 0, len(resp.GetMembers()))
	for _, m := range resp.GetMembers() {
		out = append(out, FederationMember{
			Handle:       m.GetFederateHandle(),
			Name:         m.GetFederateName(),
			FederateType: m.GetFederateType(),
		})
	}
	return out, nil
}

// ResignWithAction sends ResignFederation with an explicit action.
// IEEE 1516.1-2010 §4.10. M24 W2.
func (f *Federate) ResignWithAction(ctx context.Context, action ResignAction) error {
	var resignErr error
	f.resignOnce.Do(func() {
		f.localLRCClosing.Store(true)
		if err := f.closeLocalLRC(ctx); err != nil {
			resignErr = err
		}
		// Stop new admission first. An already-transmitted request is allowed to
		// reach its ACK boundary; only the resign deadline forces cancellation.
		f.interactionClosing.Store(true)
		if err := f.drainAndCloseInteractionStream(ctx); err != nil && resignErr == nil {
			resignErr = err
		}
		if f.interactionCancel != nil {
			f.interactionCancel()
		}
		// Best-effort wire-level resign. We tolerate failure here
		// because the server may have already torn down (federation
		// halted, stream dropped, etc.) — we still want to release
		// our local goroutine + channel.
		rpcCtx := ctx
		cancelRPC := func() {}
		if ctx.Err() != nil {
			rpcCtx, cancelRPC = context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		}
		_, err := f.conn.fed.ResignFederation(rpcCtx, &rtiv1.ResignFederationRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: f.federationName,
			FederateHandle: f.federateHandle,
			Action:         action.wire(),
		})
		cancelRPC()
		// NotFound is fine — federation may have been destroyed by a peer.
		if err != nil && status.Code(err) != codes.NotFound && resignErr == nil {
			resignErr = err
		}
		f.streamCancel()
		<-f.drainDone
	})
	return resignErr
}

// drainEvents pumps the gRPC stream into f.eventCh. Translates each
// proto FederateEvent into a typed Event with FOM names resolved where
// applicable. Exits when the stream returns io.EOF, is cancelled, or rtid
// drops it.
func (f *Federate) drainEvents(stream grpc.ServerStreamingClient[rtiv1.FederateEvent]) {
	defer close(f.eventCh)
	defer close(f.drainDone)
	for {
		evt, err := stream.Recv()
		if err != nil {
			// EOF / Canceled / other transport errors — clean exit.
			if errors.Is(err, io.EOF) || isCanceledOrUnavailable(err) {
				return
			}
			// Unexpected error: log via panic in tests would be too
			// loud; just exit. Future hook: surface via an error
			// channel if callers care.
			return
		}
		typed := f.translate(evt)
		if typed == nil {
			continue
		}
		// Non-blocking send on the event channel? Not yet — backpressure
		// surfaces as a slow drainer (the server-side outbox already
		// has its own queue limit). Block here so the federate's
		// internal queue can serialize consumption.
		select {
		case f.eventCh <- typed:
		case <-stream.Context().Done():
			return
		}
	}
}

// drainEventBatches keeps the public callback channel unchanged while
// amortizing transport framing. JoinFederation consumes the ready handshake
// before starting this drainer, so every batch here contains callbacks.
func (f *Federate) drainEventBatches(
	stream grpc.ServerStreamingClient[rtiv1.FederateEventBatch],
) {
	defer close(f.eventCh)
	defer close(f.drainDone)
	for {
		batch, err := stream.Recv()
		if err != nil {
			return
		}
		for _, evt := range batch.GetEvents() {
			typed := f.translate(evt)
			if typed == nil {
				continue
			}
			select {
			case f.eventCh <- typed:
			case <-stream.Context().Done():
				return
			}
		}
	}
}

func isCanceledOrUnavailable(err error) bool {
	c := status.Code(err)
	return c == codes.Canceled || c == codes.Unavailable
}

// translate maps a proto FederateEvent oneof variant to a typed
// public Event. Returns nil for variants the SDK does not yet surface.
//
// Known object and interaction handles are resolved through fomTables. Other
// oneof variants are silently dropped until their SDK types are added.
func (f *Federate) translate(evt *rtiv1.FederateEvent) Event {
	if evt == nil {
		return nil
	}
	switch v := evt.GetEvent().(type) {
	case *rtiv1.FederateEvent_Discover:
		discover := v.Discover
		if discover == nil {
			return nil
		}
		classHandle := discover.GetObjectClassHandle()
		className, ok := f.handles.objectClassName(classHandle)
		if !ok {
			return nil
		}
		f.rememberObjectClass(discover.GetObjectHandle(), classHandle)
		return DiscoverObjectInstance{
			ObjectHandle: discover.GetObjectHandle(),
			ClassName:    className,
			ObjectName:   discover.GetObjectName(),
		}
	case *rtiv1.FederateEvent_Reflect:
		reflect := v.Reflect
		if reflect == nil {
			return nil
		}
		classHandle := reflect.GetObjectClassHandle()
		if f.conn != nil && f.conn.callbackRepresentation == CallbackRepresentationHandles {
			var timestamp *float64
			if reflect.LogicalTime != nil {
				value := reflect.GetLogicalTime()
				timestamp = &value
			}
			f.rememberObjectClass(reflect.GetObjectHandle(), classHandle)
			return ReflectAttributeValuesByHandle{
				ObjectHandle:      reflect.GetObjectHandle(),
				ObjectClassHandle: classHandle,
				Attributes:        reflect.GetAttributes(),
				Timestamp:         timestamp,
			}
		}
		className, ok := f.handles.objectClassName(classHandle)
		if !ok {
			return nil
		}
		attributes := make(map[string][]byte, len(reflect.GetAttributes()))
		for attributeHandle, payload := range reflect.GetAttributes() {
			name, found := f.handles.attributeName(classHandle, attributeHandle)
			if !found {
				continue
			}
			attributes[name] = append([]byte(nil), payload...)
		}
		var timestamp *float64
		if reflect.LogicalTime != nil {
			value := reflect.GetLogicalTime()
			timestamp = &value
		}
		f.rememberObjectClass(reflect.GetObjectHandle(), classHandle)
		return ReflectAttributeValues{
			ObjectHandle: reflect.GetObjectHandle(),
			ClassName:    className,
			Attributes:   attributes,
			Timestamp:    timestamp,
		}
	case *rtiv1.FederateEvent_Receive:
		ri := v.Receive
		if ri == nil {
			return nil
		}
		if f.conn != nil && f.conn.callbackRepresentation == CallbackRepresentationHandles {
			var timestamp *float64
			if hasLogicalTime(ri) {
				value := ri.GetLogicalTime()
				timestamp = &value
			}
			return ReceiveInteractionByHandle{
				InteractionClassHandle: ri.GetInteractionClassHandle(),
				Parameters:             ri.GetParameters(),
				Timestamp:              timestamp,
			}
		}
		className, ok := f.handles.interactionName(ri.GetInteractionClassHandle())
		if !ok {
			return nil
		}
		params := map[string][]byte{}
		for ph, payload := range ri.GetParameters() {
			pname, pok := f.handles.parameterName(ri.GetInteractionClassHandle(), ph)
			if !pok {
				continue
			}
			// Defensive copy so the caller can't see proto-internal buffer reuse.
			b := make([]byte, len(payload))
			copy(b, payload)
			params[pname] = b
		}
		var ts *float64
		if hasLogicalTime(ri) {
			lt := ri.GetLogicalTime()
			ts = &lt
		}
		return ReceiveInteraction{
			ClassName:  className,
			Parameters: params,
			Timestamp:  ts,
		}
	case *rtiv1.FederateEvent_Grant:
		return TimeAdvanceGrant{Time: v.Grant.GetLogicalTime()}
	case *rtiv1.FederateEvent_SyncAnnounced:
		if v.SyncAnnounced == nil {
			return nil
		}
		return SynchronizationPointAnnounced{
			Label:             v.SyncAnnounced.GetLabel(),
			Tag:               append([]byte(nil), v.SyncAnnounced.GetTag()...),
			RequiredFederates: append([]uint64(nil), v.SyncAnnounced.GetRequiredFederates()...),
		}
	case *rtiv1.FederateEvent_SyncSynchronized:
		if v.SyncSynchronized == nil {
			return nil
		}
		return FederationSynchronized{
			Label:        v.SyncSynchronized.GetLabel(),
			FailedToSync: append([]uint64(nil), v.SyncSynchronized.GetFailedToSync()...),
		}
	case *rtiv1.FederateEvent_ReservationSucceeded:
		if v.ReservationSucceeded == nil {
			return nil
		}
		return ObjectInstanceNameReservationSucceeded{
			ObjectName: v.ReservationSucceeded.GetObjectName(),
		}
	case *rtiv1.FederateEvent_ReservationFailed:
		if v.ReservationFailed == nil {
			return nil
		}
		return ObjectInstanceNameReservationFailed{
			ObjectName: v.ReservationFailed.GetObjectName(),
		}
	case *rtiv1.FederateEvent_Halted:
		return FederationHalted{Reason: v.Halted.GetCause()}
	case *rtiv1.FederateEvent_Remove:
		// M23 W1 — IEEE 1516.1-2010 §6.16 RemoveObjectInstance callback.
		rm := v.Remove
		if rm == nil {
			return nil
		}
		var ts *float64
		if rm.LogicalTime != nil {
			lt := rm.GetLogicalTime()
			ts = &lt
		}
		// Defensive copy on the tag bytes.
		tag := append([]byte(nil), rm.GetUserSuppliedTag()...)
		f.forgetObjectClass(rm.GetObjectHandle())
		return RemoveObjectInstance{
			ObjectHandle: rm.GetObjectHandle(),
			Tag:          tag,
			Timestamp:    ts,
		}
	case *rtiv1.FederateEvent_ProvideUpdate:
		// M23 W2 — IEEE 1516.1-2010 §6.26 ProvideAttributeValueUpdate callback.
		pv := v.ProvideUpdate
		if pv == nil {
			return nil
		}
		attrs := append([]uint64(nil), pv.GetAttributeHandles()...)
		tag := append([]byte(nil), pv.GetUserSuppliedTag()...)
		return ProvideAttributeValueUpdate{
			ObjectHandle:     pv.GetObjectHandle(),
			AttributeHandles: attrs,
			Tag:              tag,
		}
	default:
		return nil
	}
}

// ReceiveInteraction.logical_time is optional, so presence distinguishes an
// untimed callback from a timestamped callback at logical time zero.
func hasLogicalTime(event *rtiv1.ReceiveInteraction) bool {
	return event != nil && event.LogicalTime != nil
}

// errorString trims the gRPC error message of its "rpc error: code = X desc = " prefix.
// Used by error translation in errors.go.
func errorString(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, "desc = "); i >= 0 {
		return s[i+len("desc = "):]
	}
	return s
}

// --- M16.2 cross-node redirect / reconnect --------------------------------

// MaxRedirectFollow caps the number of REDIRECT hops the SDK
// follows before giving up. Prevents infinite redirect loops in
// misconfigured clusters.
const MaxRedirectFollow = 3

// ErrTooManyRedirects is returned when ResolveFederationHost
// follows MaxRedirectFollow REDIRECT responses without reaching
// CURRENT.
var ErrTooManyRedirects = errors.New("federate: too many redirects")

// ResolveFederationHost asks the rtid at the current connection
// which node hosts `federationName`, following REDIRECT responses
// transparently. Returns:
//   - the original Connection unchanged when the rtid hosts the
//     federation (Status=CURRENT) OR the federation is unknown
//     (Status=NOT_FOUND — caller will create it on this node)
//   - a NEW Connection to the host rtid when Status=REDIRECT
//
// When a new Connection is returned, the caller is responsible for
// closing the prior one (if no longer needed). Use the returned
// Connection for the subsequent JoinFederation call.
//
// M15 cut-2 demo wire surface; M16 cut-3 will add reconnect-on-
// stream-drop tied to the consensus liveness signal.
func (c *Connection) ResolveFederationHost(
	ctx context.Context,
	federationName string,
	opts ConnectOptions,
) (*Connection, error) {
	if c == nil || c.cluster == nil {
		return c, errors.New("federate: cluster service not available on this connection")
	}
	current := c
	for hop := 0; hop < MaxRedirectFollow; hop++ {
		resp, err := current.cluster.LookupFederationHost(ctx,
			&rtiv1.LookupFederationHostRequest{
				WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
				FederationName: federationName,
			})
		if err != nil {
			return current, fmt.Errorf("federate: LookupFederationHost: %w", err)
		}
		switch resp.GetStatus() {
		case rtiv1.LookupFederationHostResponse_CURRENT,
			rtiv1.LookupFederationHostResponse_NOT_FOUND:
			return current, nil
		case rtiv1.LookupFederationHostResponse_REDIRECT:
			// Dial the host. Close the prior intermediate (but not
			// the caller's original Connection — that's their
			// responsibility).
			next, err := ConnectWithOptions(ctx, resp.GetHostAddress(), opts)
			if err != nil {
				return current, fmt.Errorf("federate: redial %s: %w",
					resp.GetHostAddress(), err)
			}
			if current != c {
				_ = current.Close()
			}
			current = next
		default:
			return current, fmt.Errorf("federate: LookupFederationHost: unexpected status %v",
				resp.GetStatus())
		}
	}
	return current, ErrTooManyRedirects
}
