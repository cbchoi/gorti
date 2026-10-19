package grpc

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/experiments/cluster/manager"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type blockingAssignmentPeer struct {
	rtiv1.UnimplementedClusterServiceServer
	deadlines chan time.Time
	cancelled chan error
	calls     atomic.Int32
}

func (p *blockingAssignmentPeer) NotifyAssignment(ctx context.Context, _ *rtiv1.NotifyAssignmentRequest) (*rtiv1.Empty, error) {
	p.calls.Add(1)
	deadline, _ := ctx.Deadline()
	p.deadlines <- deadline
	<-ctx.Done()
	p.cancelled <- ctx.Err()
	return nil, status.FromContextError(ctx.Err()).Err()
}

func TestBroadcastAssignmentReviewHasTotalBoundAndPreservesShorterDeadline(t *testing.T) {
	for _, shorter := range []bool{false, true} {
		name := "detached-creation"
		if shorter {
			name = "shorter-caller"
		}
		t.Run(name, func(t *testing.T) {
			listener := bufconn.Listen(1 << 20)
			server := grpcgo.NewServer()
			peer := &blockingAssignmentPeer{deadlines: make(chan time.Time, 2), cancelled: make(chan error, 2)}
			rtiv1.RegisterClusterServiceServer(server, peer)
			serveDone := make(chan error, 1)
			go func() { serveDone <- server.Serve(listener) }()
			manager := cluster.New("self", "self-address")
			manager.RegisterPeer("peer-one", "first")
			manager.RegisterPeer("peer-two", "second")
			service := NewClusterService(manager, func(string) (*grpcgo.ClientConn, error) {
				return grpcgo.NewClient("passthrough:///blocked-peer", grpcgo.WithTransportCredentials(insecure.NewCredentials()), grpcgo.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
			})
			done := make(chan struct{})
			t.Cleanup(func() {
				server.Stop()
				_ = listener.Close()
				<-serveDone
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("broadcast remained blocked after peer teardown")
				}
				_ = service.Close()
			})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			ctx = context.WithoutCancel(ctx)
			var expectedDeadline time.Time
			if shorter {
				ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
				defer cancel()
				expectedDeadline, _ = ctx.Deadline()
			}
			started := time.Now() //nolint:forbidigo // Measures real gRPC deadline propagation and elapsed cancellation time.
			go func() { service.BroadcastAssignment(ctx, "f", "self", "self-address"); close(done) }()
			var receivedDeadline time.Time
			select {
			case receivedDeadline = <-peer.deadlines:
			case <-time.After(2 * time.Second):
				t.Fatal("peer did not receive assignment")
			}
			if receivedDeadline.IsZero() {
				t.Fatal("committed creation sent an unbounded peer notification")
			}
			if shorter {
				if receivedDeadline.After(expectedDeadline.Add(20 * time.Millisecond)) {
					t.Fatal("shorter caller deadline extended")
				}
			} else if remaining := receivedDeadline.Sub(started); remaining < 4*time.Second || remaining > 6*time.Second {
				t.Fatalf("expected five-second bound, got %v", remaining)
			}
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				t.Fatal("peer notification exceeded total batch timeout")
			}
			select {
			case err := <-peer.cancelled:
				if err == nil {
					t.Fatal("blocking peer was not cancelled")
				}
			case <-time.After(time.Second):
				t.Fatal("peer did not observe cancellation")
			}
			if peer.calls.Load() != 1 {
				t.Fatalf("expired batch started another blocking RPC: %d", peer.calls.Load())
			}
			if elapsed := time.Since(started); elapsed > 6*time.Second { //nolint:forbidigo // Verifies the real total RPC timeout, not simulated time.
				t.Fatalf("timeout applied per peer, not per batch: %v", elapsed)
			}
		})
	}
}

func TestBroadcastAssignmentExpiredBatchDoesNotDial(t *testing.T) {
	for _, stage := range []string{"before-batch", "during-dial"} {
		t.Run(stage, func(t *testing.T) {
			mgr := cluster.New("self", "self-address")
			mgr.RegisterPeer("peer-one", "first")
			mgr.RegisterPeer("peer-two", "second")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls int
			service := NewClusterService(mgr, func(string) (*grpcgo.ClientConn, error) {
				calls++
				cancel()
				return nil, errors.New("peer unavailable")
			})
			if stage == "before-batch" {
				cancel()
			}
			service.BroadcastAssignment(ctx, "f", "self", "self-address")
			want := 0
			if stage == "during-dial" {
				want = 1
			}
			if calls != want {
				t.Fatalf("expired batch dialed %d peers, want %d", calls, want)
			}
		})
	}
}
