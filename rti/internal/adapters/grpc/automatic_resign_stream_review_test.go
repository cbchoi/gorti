package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type restoreBetweenStreamLeases struct {
	id                core.MemberIdentity
	afterFirstRelease func()
	leases            int
}

type callbackValidationOnly struct {
	core.FederationMembershipValidator
}

func TestAuthenticatedStreamRequiresRetainedMembershipGuard(t *testing.T) {
	for _, mode := range []string{"missing", "validator-only"} {
		t.Run(mode, func(t *testing.T) {
			members := &restoreBetweenStreamLeases{id: core.MemberIdentity{Federation: "f", Generation: 2, Name: "alice", Handle: 1, Incarnation: 1}}
			sessions, err := session.New(session.Options{Membership: members, Grace: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			defer sessions.Close()
			token, err := session.NewToken()
			if err != nil {
				t.Fatal(err)
			}
			sessions.Register(members.id, token)
			out := newFakeSubscribableOutbox()
			out.closeChan()
			svc := newStreamService(out)
			if mode == "validator-only" {
				svc.membership = callbackValidationOnly{members}
			}
			svc.sessions = sessions
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(token)))
			err = svc.Events(&rtiv1.EventsRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1}, &fakeStream{ctx: ctx})
			if status.Code(err) != codes.FailedPrecondition || out.subscribed != 0 {
				t.Fatalf("authenticated subscription without retained lease: err=%v subscriptions=%d", err, out.subscribed)
			}
		})
	}
}

func (m *restoreBetweenStreamLeases) MemberIdentity(core.FederationName, core.FederateHandle) (core.MemberIdentity, error) {
	return m.id, nil
}
func (m *restoreBetweenStreamLeases) AcquireMember(core.FederationName, core.FederateHandle) (func(), error) {
	m.leases++
	first := m.leases == 1
	return func() {
		if first && m.afterFirstRelease != nil {
			m.afterFirstRelease()
		}
	}, nil
}
func (*restoreBetweenStreamLeases) AutomaticResignFederation(context.Context, core.MemberIdentity, func() bool) error {
	return nil
}
func (*restoreBetweenStreamLeases) ValidateMember(core.FederationName, core.FederateHandle) error {
	return nil
}
func (m *restoreBetweenStreamLeases) GenerationFor(core.FederationName) (uint64, bool) {
	return m.id.Generation, true
}

func TestAuthenticatedStreamCannotSubscribeAfterMembershipIncarnationChanges(t *testing.T) {
	for _, batched := range []bool{false, true} {
		t.Run(map[bool]string{false: "events", true: "batches"}[batched], func(t *testing.T) {
			members := &restoreBetweenStreamLeases{id: core.MemberIdentity{Federation: "f", Generation: 2, Name: "alice", Handle: 1, Incarnation: 1}}
			sessions, err := session.New(session.Options{Membership: members, Grace: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			defer sessions.Close()
			oldToken, err := session.NewToken()
			if err != nil {
				t.Fatal(err)
			}
			sessions.Register(members.id, oldToken)
			members.afterFirstRelease = func() {
				// Resign/rejoin rotates the incarnation; a subsequent restore can
				// put that replacement session back at the earlier numeric handle.
				members.id.Incarnation = 3
				newToken, e := session.NewToken()
				if e != nil {
					t.Fatal(e)
				}
				sessions.Register(members.id, newToken)
			}
			out := newFakeSubscribableOutbox()
			out.closeChan()
			svc := newStreamService(out, members)
			svc.sessions = sessions
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(oldToken)))
			req := &rtiv1.EventsRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1}
			if batched {
				err = svc.EventBatches(req, &fakeBatchStream{ctx: ctx})
			} else {
				err = svc.Events(req, &fakeStream{ctx: ctx})
			}
			if err == nil || out.subscribed != 0 {
				t.Fatalf("stale authenticated startup subscribed to replacement membership: err=%v subscriptions=%d", err, out.subscribed)
			}
		})
	}
}
