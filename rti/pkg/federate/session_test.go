package federate

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestConfirmedUnaryContextPreservesSessionAndCallerMetadata(t *testing.T) {
	f := &Federate{sessionToken: []byte{0, 1, 2, 255}}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-fixture", "kept"))
	unary, finish := f.confirmedUnaryContext(ctx)
	defer finish()
	md, _ := metadata.FromOutgoingContext(unary)
	if got := md.Get("gorti-session-token-bin"); len(got) != 1 || got[0] != string(f.sessionToken) {
		t.Fatalf("confirmed unary lost authenticated session: %v", md)
	}
	if got := md.Get("x-fixture"); len(got) != 1 || got[0] != "kept" {
		t.Fatalf("confirmed unary lost caller metadata: %v", md)
	}
}

func TestGoResignUsesSessionMetadataAndPreservesLegacy(t *testing.T) {
	for _, signed := range []bool{false, true} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("signed=%t/cancelled=%t", signed, cancelled), func(t *testing.T) {
				var calls atomic.Int32
				token := []byte{0, 1, 2, 255}
				f := remainingServicesFederate(t, func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
					calls.Add(1)
					r := req.(*rtiv1.ResignFederationRequest)
					md, _ := metadata.FromIncomingContext(ctx)
					got := md.Get("gorti-session-token-bin")
					if signed && (len(got) != 1 || got[0] != string(token)) {
						t.Errorf("signed resign token=%q, want exact session token", got)
					}
					if !signed && len(got) != 0 {
						t.Errorf("legacy resign fabricated token: %q", got)
					}
					if values := md.Get("x-resign-test"); len(values) != 1 || values[0] != testRetainedMetadata {
						t.Errorf("caller metadata lost: %v", md)
					}
					if r.FederationName != testFederationName || r.FederateHandle != 7 || r.Action != rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION {
						t.Errorf("resign identity/action changed: %v", r)
					}
					return &rtiv1.Empty{}, nil
				})
				if signed {
					f.sessionToken = token
				}
				closed := false
				f.streamCancel = func() { closed = true }
				f.drainDone = make(chan struct{})
				close(f.drainDone)
				ctx, cancel := context.WithCancel(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-resign-test", testRetainedMetadata)))
				defer cancel()
				if cancelled {
					cancel()
				}
				err := f.ResignWithAction(ctx, ResignActionNoAction)
				if err != nil && (!cancelled || status.Code(err) != codes.Canceled) {
					t.Fatalf("resign: %v", err)
				}
				if calls.Load() != 1 || !closed {
					t.Fatalf("resign RPCs=%d, stream closed=%t", calls.Load(), closed)
				}
			})
		}
	}
}
