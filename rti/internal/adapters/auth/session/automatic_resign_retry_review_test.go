package session_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/metadata"
)

type transientResignMembers struct {
	*members
	attempts atomic.Int32
}

func (m *transientResignMembers) AutomaticResignFederation(ctx context.Context, id core.MemberIdentity, condition func() bool) error {
	if m.attempts.Add(1) == 1 {
		return core.ErrFederateOverflow
	}
	return m.members.AutomaticResignFederation(ctx, id, condition)
}

func TestAutomaticResignRetriesTransientCallbackAdmissionFailure(t *testing.T) {
	mm := &transientResignMembers{members: &members{id: core.MemberIdentity{Federation: "f", Generation: 2, Name: "a", Handle: 1, Incarnation: 1}, gone: make(chan struct{})}}
	failed := make(chan error, 4)
	mgr, err := session.New(session.Options{Membership: mm, Grace: 10 * time.Millisecond, OnError: func(err error) { failed <- err }})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	token, err := session.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(mm.id, token)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(token)))
	done, err := mgr.Open(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	done()
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("first cleanup attempt did not run")
	}
	// The callback queue is available after the first failed admission. There
	// is no new stream-close event to trigger another attempt for this session.
	select {
	case <-mm.gone:
	case <-time.After(150 * time.Millisecond):
		t.Fatalf("transient failure permanently abandoned disconnected membership; attempts=%d", mm.attempts.Load())
	}
}
