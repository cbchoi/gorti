// TASK-205½ (M21) — Federate SDK foundation. See docs/M21_DISPATCH_PLAN.md §2.7.
//
// This file holds the public Connection + Federate types, lifecycle
// methods (Connect/Close/JoinFederation/Resign), and the events-drain
// goroutine. FOM handle resolution lives in handles.go; per-RPC
// dispatchers live in declaration.go and interaction.go.

package federate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// Connection wraps a gRPC channel to rtid + the cut-1 service stubs.
// One Connection MAY host multiple federates; the cut-3 / M21 happy
// path is one Federate per Connection.
type Connection struct {
	cc     *grpc.ClientConn
	fed    rtiv1.FederationServiceClient
	decl   rtiv1.DeclarationServiceClient
	obj    rtiv1.ObjectServiceClient
	stream rtiv1.StreamServiceClient
	tm     rtiv1.TimeServiceClient // wired here so time.go (TASK-206) can use it
	ddm    rtiv1.DDMServiceClient  // M23 W4 — Go SDK DDM coverage
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
	conn           *Connection
	federationName string
	federateName   string
	federateHandle uint64

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

	mu sync.Mutex // protects future state mutators added in W3A
}

// Connect dials rtid at addr (host:port). Caller MUST call Close()
// when done. The connection uses insecure credentials for cut-3 —
// TLS is M14 territory.
func Connect(ctx context.Context, addr string) (*Connection, error) {
	cc, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("federate: dial %s: %w", addr, err)
	}
	_ = ctx // grpc.NewClient is non-blocking; ctx unused at this layer.
	return &Connection{
		cc:     cc,
		fed:    rtiv1.NewFederationServiceClient(cc),
		decl:   rtiv1.NewDeclarationServiceClient(cc),
		obj:    rtiv1.NewObjectServiceClient(cc),
		stream: rtiv1.NewStreamServiceClient(cc),
		tm:     rtiv1.NewTimeServiceClient(cc),
		ddm:    rtiv1.NewDDMServiceClient(cc),
	}, nil
}

// Close releases the gRPC connection. Open Federates created from
// this Connection should be Resigned first; Close() does NOT auto-resign.
func (c *Connection) Close() error {
	if c == nil || c.cc == nil {
		return nil
	}
	return c.cc.Close()
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
	if _, cErr := c.fed.CreateFederation(ctx, createReq); cErr != nil {
		if status.Code(cErr) != codes.AlreadyExists {
			return nil, fmt.Errorf("federate: CreateFederation: %w", cErr)
		}
	}

	// 2. Join.
	joinResp, err := c.fed.JoinFederation(ctx, &rtiv1.JoinFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: spec.Name,
		FederateName:   federateName,
	})
	if err != nil {
		return nil, fmt.Errorf("federate: JoinFederation: %w", err)
	}

	// 3. Open the events stream + spawn drainer.
	streamCtx, streamCancel := context.WithCancel(context.Background())
	stream, err := c.stream.Events(streamCtx, &rtiv1.EventsRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: spec.Name,
		FederateHandle: joinResp.GetFederateHandle(),
	})
	if err != nil {
		streamCancel()
		return nil, fmt.Errorf("federate: Events stream open: %w", err)
	}

	f := &Federate{
		conn:           c,
		federationName: spec.Name,
		federateName:   federateName,
		federateHandle: joinResp.GetFederateHandle(),
		handles:        tables,
		eventCh:        make(chan Event, 256),
		streamCancel:   streamCancel,
		drainDone:      make(chan struct{}),
	}
	go f.drainEvents(stream)
	return f, nil
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
		// Best-effort wire-level resign. We tolerate failure here
		// because the server may have already torn down (federation
		// halted, stream dropped, etc.) — we still want to release
		// our local goroutine + channel.
		_, err := f.conn.fed.ResignFederation(ctx, &rtiv1.ResignFederationRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: f.federationName,
			FederateHandle: f.federateHandle,
			Action:         action.wire(),
		})
		// NotFound is fine — federation may have been destroyed by a peer.
		if err != nil && status.Code(err) != codes.NotFound {
			resignErr = err
		}
		f.streamCancel()
		<-f.drainDone
	})
	return resignErr
}

// drainEvents pumps the gRPC stream into f.eventCh. Translates each
// proto FederateEvent into a typed Event (ReceiveInteraction with
// parameter NAMES, TimeAdvanceGrant, FederationHalted). Exits when
// the stream returns io.EOF, is cancelled, or rtid drops it.
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
		f.eventCh <- typed
	}
}

func isCanceledOrUnavailable(err error) bool {
	c := status.Code(err)
	return c == codes.Canceled || c == codes.Unavailable
}

// translate maps a proto FederateEvent oneof variant to a typed
// public Event. Returns nil for variants the SDK does not yet
// surface (object-class events, sync, ownership, etc.).
//
// Cut-1 + M21 surface: ReceiveInteraction (with parameter names
// resolved via fomTables), TimeAdvanceGrant, FederationHalted.
// Other oneof variants are silently dropped — they belong to
// follow-up SDK extensions.
func (f *Federate) translate(evt *rtiv1.FederateEvent) Event {
	if evt == nil {
		return nil
	}
	switch v := evt.GetEvent().(type) {
	case *rtiv1.FederateEvent_Receive:
		ri := v.Receive
		if ri == nil {
			return nil
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
		if ri.GetLogicalTime() != 0 || hasLogicalTime(ri) {
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

// hasLogicalTime is a helper for the "is timestamp set" check —
// proto3 doubles default to 0, so we have to inspect by other means.
// For interactions, the wire layer always sets logical_time when
// the publisher passed one; absent → 0. We treat 0 as "no timestamp"
// in the cut-1 SDK; senders that genuinely want t=0 can use NaN
// translation in a follow-up. Keeps the surface simple.
func hasLogicalTime(_ *rtiv1.ReceiveInteraction) bool { return false }

// errorString trims the gRPC error message of its "rpc error: code = X desc = " prefix.
// Used by error translation in errors.go.
func errorString(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, "desc = "); i >= 0 {
		return s[i+len("desc = "):]
	}
	return s
}
