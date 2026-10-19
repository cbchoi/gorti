package session_test

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type currentCallerRoster struct{ *members }

func (m currentCallerRoster) ListMembers(core.FederationName) []core.FederationMember {
	m.mu.Lock()
	defer m.mu.Unlock()
	return []core.FederationMember{{Name: m.id.Name, Handle: m.id.Handle}}
}

func TestCurrentCallerIdentityFollowsRestoreButNotReplacement(t *testing.T) {
	for _, variant := range []string{"restore", "incarnation", "generation", "name", "missing-token", "wrong-token"} {
		t.Run(variant, func(t *testing.T) {
			saved := core.MemberIdentity{Federation: "f", Generation: 2, Name: "a", Handle: 1, Incarnation: 3}
			roster := currentCallerRoster{&members{id: saved, gone: make(chan struct{})}}
			manager, err := session.New(session.Options{Membership: roster})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(manager.Close)
			token, err := session.NewToken()
			if err != nil {
				t.Fatal(err)
			}
			manager.Register(saved, token)
			roster.id.Handle = 7
			want := codes.OK
			switch variant {
			case "incarnation":
				roster.id.Incarnation++
				want = codes.FailedPrecondition
			case "generation":
				roster.id.Generation++
				want = codes.FailedPrecondition
			case "name":
				roster.id.Name = "b"
				want = codes.FailedPrecondition
			case "missing-token":
				want = codes.Unauthenticated
			case "wrong-token":
				token[0] ^= 1
				want = codes.PermissionDenied
			}
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(token)))
			if variant == "missing-token" {
				ctx = context.Background()
			}
			current, err := manager.CurrentCallerIdentity(ctx)
			if status.Code(err) != want {
				t.Fatalf("current identity=%+v error=%v want=%v", current, err, want)
			}
			if want == codes.OK && current != roster.id {
				t.Fatal("pre-restore handle returned", current)
			}
			if variant == "restore" {
				original, err := manager.CallerIdentity(ctx)
				if err != nil || original != saved {
					t.Fatal("stable session identity mutated", original, err)
				}
			}
		})
	}
}
