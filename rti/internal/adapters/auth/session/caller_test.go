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

func TestCallerIdentityRejectsInvalidAndRetiredTokens(t *testing.T) {
	mgr, members, ctx, token := setup(t)
	id, err := mgr.CallerIdentity(ctx)
	if err != nil || id != members.id {
		t.Fatalf("identity=%+v, err=%v", id, err)
	}
	if _, err := mgr.CallerIdentity(context.Background()); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing token: %v", err)
	}
	duplicate := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(token), session.MetadataKey, string(token)))
	if _, err := mgr.CallerIdentity(duplicate); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("duplicate token: %v", err)
	}
	wrong := append([]byte(nil), token...)
	wrong[0] ^= 1
	bad := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(wrong)))
	if _, err := mgr.CallerIdentity(bad); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("wrong token: %v", err)
	}
	mgr.ClearIdentity(id)
	if _, err := mgr.CallerIdentity(ctx); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("retired token: %v", err)
	}
}

func TestCallerIdentityRejectsAmbiguousOrClosedSessions(t *testing.T) {
	mgr, _, ctx, token := setup(t)
	mgr.Register(core.MemberIdentity{Federation: "other", Generation: 7, Name: "second", Handle: 1, Incarnation: 1}, token)
	if _, err := mgr.CallerIdentity(ctx); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ambiguous token: %v", err)
	}
	mgr.Close()
	if _, err := mgr.CallerIdentity(ctx); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("closed session: %v", err)
	}
}
