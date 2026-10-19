package federate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/metadata"
)

// JoinCleanupError retains a remotely joined membership whose local setup and
// compensating resignation failed. Use errors.As to obtain it, then RetryResign
// before closing the Connection. It never exposes a usable Federate or token.
type JoinCleanupError struct {
	mu             sync.Mutex
	client         rtiv1.FederationServiceClient
	federationName string
	federateHandle uint64
	sessionToken   []byte
	completed      bool
	failure        error
}

func (e *JoinCleanupError) Error() string {
	if e.failure == nil {
		return "federate: failed join cleanup pending"
	}
	return e.failure.Error()
}

// Format keeps private membership capabilities out of diagnostic formatting.
func (e *JoinCleanupError) Format(state fmt.State, _ rune) {
	fmt.Fprint(state, e.Error())
}

// Unwrap preserves both the original setup failure and the initial cleanup failure.
func (e *JoinCleanupError) Unwrap() error { return e.failure }

func (e *JoinCleanupError) FederationName() string { return e.federationName }
func (e *JoinCleanupError) Handle() uint64         { return e.federateHandle }

// RetryResign uses the captured identity, replacing any caller-supplied session
// metadata. Failure retains ownership for another retry; success is idempotent.
// The caller controls the retry deadline through ctx.
func (e *JoinCleanupError) RetryResign(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.completed {
		return nil
	}
	if e.client == nil {
		return ErrNotJoined
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Delete("gorti-session-token-bin")
	if len(e.sessionToken) != 0 {
		md.Set("gorti-session-token-bin", string(e.sessionToken))
	}
	_, err := e.client.ResignFederation(metadata.NewOutgoingContext(ctx, md), &rtiv1.ResignFederationRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: e.federationName,
		FederateHandle: e.federateHandle,
		Action:         rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES,
	})
	if err != nil {
		return err
	}
	e.completed = true
	e.sessionToken = nil
	return nil
}

func (c *Connection) compensateFailedJoin(ctx context.Context, federationName string, joined *rtiv1.JoinFederationResponse, originalErr error) error {
	recovery := &JoinCleanupError{
		client:         c.fed,
		federationName: federationName,
		federateHandle: joined.GetFederateHandle(),
		sessionToken:   append([]byte(nil), joined.GetSessionToken()...),
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := recovery.RetryResign(cleanupCtx); err != nil {
		recovery.failure = errors.Join(originalErr, fmt.Errorf("federate: compensating ResignFederation: %w", err))
		return recovery
	}
	return originalErr
}
