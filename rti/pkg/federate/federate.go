// Scaffold owned by TASK-205½ (M21) — see docs/M21_DISPATCH_PLAN.md §2.7.0.
//
// This file declares the public types + method signatures of the Go federate
// SDK foundation. Bodies are TODO panics; the M21 W2½ sub-agent fills them in.
//
// SCAFFOLD CONTRACT: types and method signatures are orchestrator-frozen —
// extend bodies but do NOT change exported signatures without revising the
// dispatch plan. Test scaffold lives in federate_test.go.

package federate

import (
	"context"
	"sync"
)

// Connection wraps a gRPC channel to rtid. One Connection MAY host
// multiple federates (across multiple federations) for advanced use;
// the cut-3 / M21 happy path is one Federate per Connection.
type Connection struct {
	// Implementation note (TASK-205½): hold the *grpc.ClientConn plus
	// the cut-1 service stubs (FederationServiceClient,
	// DeclarationServiceClient, ObjectServiceClient,
	// StreamServiceClient, TimeServiceClient). Loaded lazily once the
	// connection is opened.
}

// FederationSpec describes a federation to create-or-join.
type FederationSpec struct {
	Name       string
	FOMModules []FOMModule
	Seed       uint64
	// StallTimeoutSeconds is optional. Zero → server default (60s).
	StallTimeoutSeconds uint32
}

// FOMModule is one FOM XML module submitted at federation create time.
type FOMModule struct {
	Path string
	XML  []byte
}

// Federate represents a federate that has joined a federation. The
// Resign() method MUST be called to cleanly leave; cancelling the
// context is not sufficient.
type Federate struct {
	mu             sync.Mutex //nolint:unused // wired by TASK-205½
	federationName string     //nolint:unused
	federateName   string     //nolint:unused
	federateHandle uint64     //nolint:unused
	// Implementation note (TASK-205½): also hold a context.CancelFunc
	// for the events-stream goroutine, plus the buffered chan Event
	// the goroutine writes to.
}

// Connect opens a gRPC connection to rtid at addr. Caller MUST call
// Close() when done.
func Connect(ctx context.Context, addr string) (*Connection, error) {
	_ = ctx
	_ = addr
	panic("TODO: TASK-205½ — implement Connect")
}

// Close releases the gRPC connection. Open Federates created from
// this Connection should be Resigned first; Close() does NOT auto-resign.
func (c *Connection) Close() error {
	panic("TODO: TASK-205½ — implement Connection.Close")
}

// JoinFederation creates the federation if it does not exist
// (idempotent — ALREADY_EXISTS swallowed) and joins it under the
// given federate name.
//
// On success, spawns a background goroutine that drains the federate's
// StreamService.Events stream into the buffered Events() channel.
// The goroutine exits cleanly on Resign().
func (c *Connection) JoinFederation(
	ctx context.Context, spec FederationSpec, federateName string,
) (*Federate, error) {
	_ = ctx
	_ = spec
	_ = federateName
	panic("TODO: TASK-205½ — implement JoinFederation")
}

// Handle returns the federate handle assigned by rtid at join time.
func (f *Federate) Handle() uint64 {
	panic("TODO: TASK-205½ — implement Federate.Handle")
}

// Name returns the federate name passed to JoinFederation.
func (f *Federate) Name() string {
	panic("TODO: TASK-205½ — implement Federate.Name")
}

// Events returns a receive-only channel of incoming events. The channel
// closes when Resign() completes or rtid drops the stream.
func (f *Federate) Events() <-chan Event {
	panic("TODO: TASK-205½ — implement Federate.Events")
}

// Resign sends ResignFederation to rtid, cancels the events-drain
// goroutine, and closes the Events() channel. Idempotent — second
// Resign is a no-op.
func (f *Federate) Resign(ctx context.Context) error {
	_ = ctx
	panic("TODO: TASK-205½ — implement Federate.Resign")
}

// PublishInteractionClass declares this federate publishes the named
// interaction class. Idempotent.
func (f *Federate) PublishInteractionClass(ctx context.Context, className string) error {
	_ = ctx
	_ = className
	panic("TODO: TASK-205½ — implement Federate.PublishInteractionClass")
}

// SubscribeInteractionClass declares this federate subscribes to the
// named interaction class. Idempotent.
func (f *Federate) SubscribeInteractionClass(ctx context.Context, className string) error {
	_ = ctx
	_ = className
	panic("TODO: TASK-205½ — implement Federate.SubscribeInteractionClass")
}

// SendInteraction sends an interaction. parameters is keyed by parameter
// name; the SDK resolves to wire handles via the FOM tables built at
// JoinFederation. timestamp is optional (nil → untimed).
func (f *Federate) SendInteraction(
	ctx context.Context, className string, parameters map[string][]byte, timestamp *float64,
) error {
	_ = ctx
	_ = className
	_ = parameters
	_ = timestamp
	panic("TODO: TASK-205½ — implement Federate.SendInteraction")
}
