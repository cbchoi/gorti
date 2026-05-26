// M16.3 — demo failover spec test.
//
// Stands up 2 nodes via bufconn, has a federate connect to node-A,
// promotes the federation to node-B, verifies the federate's
// ResolveFederationHost returns a new connection pointing at B.

package m15spec

import (
	"context"
	"net"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/federate"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// TestSpec_M16_3_PromoteThenResolveRedirectsFederate is the
// headline M16 demo AC. Demonstrates the operator-triggered
// failover path:
//   1. node-A hosts federation "fed".
//   2. Operator calls cluster.PromoteFederation("fed", "node-b") on
//      node-A. The assignment flips locally + broadcasts to node-B.
//   3. A federate calling node-A's ResolveFederationHost("fed")
//      now sees REDIRECT and follows it to node-B.
func TestSpec_M16_3_PromoteThenResolveRedirectsFederate(t *testing.T) {
	const (
		nodeAID, nodeAAddr = "node-a", "passthrough:///bufnet-a"
		nodeBID, nodeBAddr = "node-b", "passthrough:///bufnet-b"
		fed                = "fed"
	)

	// Build two in-process nodes. Each peers with the other for
	// NotifyAssignment gossip.
	listeners := map[string]*bufconn.Listener{}
	a := newBufNode(t, nodeAID, nodeAAddr, listeners)
	b := newBufNode(t, nodeBID, nodeBAddr, listeners)
	listeners[nodeAAddr] = a.lis
	listeners[nodeBAddr] = b.lis
	t.Cleanup(a.close)
	t.Cleanup(b.close)
	a.mgr.RegisterPeer(nodeBID, nodeBAddr)
	b.mgr.RegisterPeer(nodeAID, nodeAAddr)

	// Initial state: federation hosted on node-A.
	a.mgr.AssignFederation(fed)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.svc.BroadcastAssignment(ctx, fed, nodeAID, nodeAAddr)

	// Step 1 — federate connects to node-A and resolves the host;
	// should stay on A (CURRENT).
	connA := dialFederateTo(t, a.lis, nodeAAddr)
	t.Cleanup(func() { _ = connA.Close() })
	resolved, err := connA.ResolveFederationHost(ctx, fed, bufconnOptsFor(listeners))
	if err != nil {
		t.Fatalf("first ResolveFederationHost: %v", err)
	}
	if resolved != connA {
		t.Errorf("Resolve before promote returned a NEW conn; want same conn (CURRENT)")
	}

	// Step 2 — operator-driven promotion of "fed" to node-B.
	prior, err := a.mgr.PromoteFederation(fed, nodeBID)
	if err != nil {
		t.Fatalf("PromoteFederation: %v", err)
	}
	if prior != nodeAID {
		t.Errorf("PromoteFederation prior = %q, want %q", prior, nodeAID)
	}
	// Broadcast the new assignment so node-B also sees itself as host.
	a.svc.BroadcastAssignment(ctx, fed, nodeBID, nodeBAddr)

	// Step 3 — federate resolves again on connA; expects REDIRECT
	// to node-B. The returned Connection is a fresh dial.
	resolvedB, err := connA.ResolveFederationHost(ctx, fed, bufconnOptsFor(listeners))
	if err != nil {
		t.Fatalf("post-promote ResolveFederationHost: %v", err)
	}
	t.Cleanup(func() { _ = resolvedB.Close() })
	if resolvedB == connA {
		t.Fatalf("Resolve after promote returned original conn; want NEW conn")
	}

	// Sanity — the new connection actually points at node-B by
	// asking IT to LookupFederationHost; should report CURRENT.
	// (The federate SDK doesn't expose the underlying cluster
	// stub; reach in via the proto client we already built for
	// the bufconn fixture.)
	clientB := b.dialClient(t)
	resp, err := clientB.LookupFederationHost(ctx,
		&rtiv1.LookupFederationHostRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: fed,
		})
	if err != nil {
		t.Fatalf("LookupFederationHost on node-b: %v", err)
	}
	if resp.GetStatus() != rtiv1.LookupFederationHostResponse_CURRENT {
		t.Errorf("node-b reports %v, want CURRENT", resp.GetStatus())
	}
}

// TestSpec_M16_3_RedirectLoopGuard — feeding a circular redirect
// (a → b → a → ...) trips ErrTooManyRedirects.
func TestSpec_M16_3_RedirectLoopGuard(t *testing.T) {
	const (
		nodeAID, nodeAAddr = "node-a", "passthrough:///bufnet-a"
		nodeBID, nodeBAddr = "node-b", "passthrough:///bufnet-b"
		fed                = "loop-fed"
	)
	listeners := map[string]*bufconn.Listener{}
	a := newBufNode(t, nodeAID, nodeAAddr, listeners)
	b := newBufNode(t, nodeBID, nodeBAddr, listeners)
	listeners[nodeAAddr] = a.lis
	listeners[nodeBAddr] = b.lis
	t.Cleanup(a.close)
	t.Cleanup(b.close)

	// Each side claims the federation is hosted by the OTHER —
	// neither hosts itself.
	a.mgr.RegisterPeer(nodeBID, nodeBAddr)
	b.mgr.RegisterPeer(nodeAID, nodeAAddr)
	a.mgr.RecordAssignment(fed, nodeBID)
	b.mgr.RecordAssignment(fed, nodeAID)

	connA := dialFederateTo(t, a.lis, nodeAAddr)
	t.Cleanup(func() { _ = connA.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := connA.ResolveFederationHost(ctx, fed, bufconnOptsFor(listeners))
	if err == nil {
		t.Errorf("circular redirect: want error, got nil")
	}
	if err != federate.ErrTooManyRedirects && !errIsTooManyRedirects(err) {
		t.Errorf("got %v, want ErrTooManyRedirects", err)
	}
}

func errIsTooManyRedirects(err error) bool {
	// Direct comparison handles the case where the error returned
	// IS the sentinel; the wrapping happens only when a redial
	// itself fails.
	return err != nil && err.Error() == federate.ErrTooManyRedirects.Error()
}

// bufconnOptsFor builds ConnectOptions with a ContextDialer that
// routes by the host_address key in ``listeners``. The dialer
// reconstructs the "passthrough:///" prefix before lookup so the
// listeners map key matches what newBufNode's PeerDialer uses for
// the same listener.
func bufconnOptsFor(listeners map[string]*bufconn.Listener) federate.ConnectOptions {
	return federate.ConnectOptions{
		ExtraDialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(_ context.Context, target string) (net.Conn, error) {
				// The passthrough resolver strips "passthrough:///"
				// from the grpc.NewClient target before invoking the
				// dialer. Re-add it so the listener key matches the
				// canonical address shape.
				key := "passthrough:///" + target
				l, ok := listeners[key]
				if !ok {
					return nil, net.ErrClosed
				}
				return l.Dial()
			}),
		},
	}
}

// dialFederateTo opens a federate.Connection backed by the supplied
// bufconn listener. The address argument is passthrough-only; the
// dialer uses the listener directly.
func dialFederateTo(t *testing.T, lis *bufconn.Listener, addr string) *federate.Connection {
	t.Helper()
	cc, err := grpc.NewClient(
		"passthrough:///"+addr,
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial federate: %v", err)
	}
	// federate.Connect requires real host:port routing. For tests
	// we go through grpcsvc-compatible bufconn but the federate
	// SDK's exported surface uses Connect / ConnectWithOptions
	// which call grpc.NewClient internally. The simplest
	// fixture-friendly path: bypass Connect by constructing the
	// stubs ourselves.
	return federate.WrapGRPCClientConn(cc)
}
