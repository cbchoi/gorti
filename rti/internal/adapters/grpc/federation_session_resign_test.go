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

type sessionResignStore struct {
	core.FederationStore
	*restoreBetweenStreamLeases
	identities []core.MemberIdentity
	handles    []core.FederateHandle
}

func (s *sessionResignStore) ResignFederation(_ context.Context, _ core.FederationName, h core.FederateHandle, _ core.ResignAction) error {
	s.handles = append(s.handles, h)
	return nil
}

func (s *sessionResignStore) ResignFederationIdentity(_ context.Context, id core.MemberIdentity, _ core.ResignAction) error {
	s.identities = append(s.identities, id)
	return nil
}

func TestAuthenticatedResignUsesMembershipIdentity(t *testing.T) {
	for _, mode := range []string{resignModeAuthenticated, "wrong-token", "malformed-token", resignModeLegacy, "missing-session-adapter", "missing-identity-port"} {
		t.Run(mode, func(t *testing.T) {
			original := core.MemberIdentity{Federation: "f", Generation: 2, Name: "alice", Handle: 1, Incarnation: 10}
			store := &sessionResignStore{restoreBetweenStreamLeases: &restoreBetweenStreamLeases{id: original}}
			sessions, err := session.New(session.Options{Membership: store, Grace: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			defer sessions.Close()
			token, err := session.NewToken()
			if err != nil {
				t.Fatal(err)
			}
			sessions.Register(original, token)
			svc := newFederationService(store)
			svc.sessions = sessions
			ctx := context.Background()
			if mode != resignModeLegacy {
				provided := append([]byte(nil), token...)
				if mode == "wrong-token" {
					provided[0] ^= 1
				}
				if mode == "malformed-token" {
					provided = provided[:1]
				}
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(session.MetadataKey, string(provided)))
			}
			if mode == "missing-session-adapter" {
				svc.sessions = nil
			}
			if mode == "missing-identity-port" {
				svc.fed = struct{ core.FederationStore }{store}
			}
			store.afterFirstRelease = func() {
				// A restore can rebind this wire handle after authentication.
				store.id = core.MemberIdentity{Federation: "f", Generation: 2, Name: "bob", Handle: 1, Incarnation: 20}
			}
			_, err = svc.ResignFederation(ctx, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION})
			switch mode {
			case resignModeAuthenticated:
				if err != nil || len(store.handles) != 0 || len(store.identities) != 1 || store.identities[0] != original {
					t.Fatalf("signed resign lost authenticated identity: err=%v handles=%v identities=%v", err, store.handles, store.identities)
				}
			case resignModeLegacy:
				if err != nil || len(store.handles) != 1 || len(store.identities) != 0 {
					t.Fatalf("legacy resign changed: err=%v handles=%v identities=%v", err, store.handles, store.identities)
				}
			default:
				want := map[string]codes.Code{"wrong-token": codes.PermissionDenied, "malformed-token": codes.Unauthenticated, "missing-session-adapter": codes.FailedPrecondition, "missing-identity-port": codes.FailedPrecondition}[mode]
				if status.Code(err) != want || len(store.handles) != 0 || len(store.identities) != 0 {
					t.Fatalf("invalid signed resign mutated membership: err=%v handles=%v identities=%v", err, store.handles, store.identities)
				}
			}
		})
	}
}

func TestAuthenticatedStreamPreservesSessionError(t *testing.T) {
	for _, batched := range []bool{false, true} {
		for _, malformed := range []bool{false, true} {
			members := &restoreBetweenStreamLeases{id: core.MemberIdentity{Federation: "f", Generation: 2, Name: "alice", Handle: 1, Incarnation: 10}}
			sessions, err := session.New(session.Options{Membership: members, Grace: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(sessions.Close)
			token, err := session.NewToken()
			if err != nil {
				t.Fatal(err)
			}
			sessions.Register(members.id, token)
			token[0] ^= 1
			want := codes.PermissionDenied
			if malformed {
				token = token[:1]
				want = codes.Unauthenticated
			}
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(token)))
			out := newFakeSubscribableOutbox()
			svc := newStreamService(out, members)
			svc.sessions = sessions
			req := &rtiv1.EventsRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1}
			if batched {
				err = svc.EventBatches(req, &fakeBatchStream{ctx: ctx})
			} else {
				err = svc.Events(req, &fakeStream{ctx: ctx})
			}
			if status.Code(err) != want || out.subscribed != 0 {
				t.Fatalf("batched=%v malformed=%v: err=%v subscriptions=%d", batched, malformed, err, out.subscribed)
			}
		}
	}
}

const (
	resignModeLegacy        = "legacy"
	resignModeAuthenticated = "authenticated"
)
