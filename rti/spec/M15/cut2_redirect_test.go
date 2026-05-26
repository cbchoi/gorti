// TASK-M15.2.4 — 2-node bufconn redirect test.
//
// Validates the M15 cut-2 demo end-to-end at the wire level:
//   - 2 cluster.Manager instances, each registered as the other's peer
//   - 2 ClusterService gRPC handlers exposed via bufconn
//   - node-B records federation "fed-1" and broadcasts
//   - node-A's LookupFederationHost returns REDIRECT with node-B's address

package m15spec

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/cluster"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	grpcsvc "github.com/cbchoi/gorti/rti/internal/transport/grpc"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type bufNode struct {
	id      string
	addr    string
	mgr     *cluster.Manager
	svc     *grpcsvc.ClusterService
	lis     *bufconn.Listener
	grpcSrv *grpc.Server
}

// newBufNode spins up an in-process rtid-shaped cluster node. The
// listener is a bufconn instance keyed by ``addr`` (the in-process
// dial target); the dialer the cluster service uses dispatches by
// the same key, simulating "host:port" routing.
func newBufNode(t *testing.T, id, addr string, peerListeners map[string]*bufconn.Listener) *bufNode {
	t.Helper()
	mgr := cluster.New(id, addr)

	// Custom dialer that routes by the cluster's advertised address.
	// peerListeners maps address → bufconn listener so a
	// BroadcastAssignment(target=addr) dials the matching listener.
	// Caller-supplied addresses MUST already be passthrough://
	// prefixed (so the federate SDK can redial them directly via
	// grpc.NewClient without re-prefixing).
	dialer := func(target string) (*grpc.ClientConn, error) {
		l, ok := peerListeners[target]
		if !ok {
			return nil, net.ErrClosed
		}
		return grpc.NewClient(
			target,
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
				return l.Dial()
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}
	svc := grpcsvc.NewClusterService(mgr, dialer)

	lis := bufconn.Listen(1 << 16)
	srv := grpc.NewServer()
	svc.RegisterWith(srv)
	go func() { _ = srv.Serve(lis) }()
	return &bufNode{
		id:      id,
		addr:    addr,
		mgr:     mgr,
		svc:     svc,
		lis:     lis,
		grpcSrv: srv,
	}
}

func (b *bufNode) close() {
	b.grpcSrv.GracefulStop()
	_ = b.svc.Close()
}

// dialClient returns a ClusterServiceClient for the federate-side
// caller perspective — used by the test to interrogate a node's
// LookupFederationHost view.
func (b *bufNode) dialClient(t *testing.T) rtiv1.ClusterServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(
		b.addr,
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return b.lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial %s: %v", b.addr, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return rtiv1.NewClusterServiceClient(conn)
}

// TestSpec_M15_2_BroadcastAssignmentRedirectsRemoteNode is the
// headline acceptance check for the M15 cut-2 demo: a federation
// created on node-B becomes REDIRECT-routable from node-A's
// LookupFederationHost.
func TestSpec_M15_2_BroadcastAssignmentRedirectsRemoteNode(t *testing.T) {
	const (
		nodeAID   = "node-a"
		nodeBID   = "node-b"
		nodeAAddr = "passthrough:///bufnet-a"
		nodeBAddr = "passthrough:///bufnet-b"
	)

	// Listener table is shared so each node's dialer can reach the
	// other. Populated AFTER bufNode construction (the Listen() call
	// returns the listener we then thread into the dialer).
	listeners := map[string]*bufconn.Listener{}
	nodeA := newBufNode(t, nodeAID, nodeAAddr, listeners)
	nodeB := newBufNode(t, nodeBID, nodeBAddr, listeners)
	listeners[nodeAAddr] = nodeA.lis
	listeners[nodeBAddr] = nodeB.lis
	t.Cleanup(nodeA.close)
	t.Cleanup(nodeB.close)

	// Register peers on both sides.
	nodeA.mgr.RegisterPeer(nodeBID, nodeBAddr)
	nodeB.mgr.RegisterPeer(nodeAID, nodeAAddr)

	// Simulate "CreateFederation on node-B": record the assignment
	// locally + broadcast to peers.
	const fedName = "demo-fed"
	nodeB.mgr.AssignFederation(fedName)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	nodeB.svc.BroadcastAssignment(ctx, fedName, nodeBID, nodeBAddr)

	// Node-A should now redirect to node-B.
	clientA := nodeA.dialClient(t)
	resp, err := clientA.LookupFederationHost(ctx,
		&rtiv1.LookupFederationHostRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: fedName,
		})
	if err != nil {
		t.Fatalf("LookupFederationHost on node-a: %v", err)
	}
	if resp.GetStatus() != rtiv1.LookupFederationHostResponse_REDIRECT {
		t.Errorf("Status = %v, want REDIRECT", resp.GetStatus())
	}
	if resp.GetHostNodeId() != nodeBID {
		t.Errorf("HostNodeId = %q, want %q", resp.GetHostNodeId(), nodeBID)
	}
	if resp.GetHostAddress() != nodeBAddr {
		t.Errorf("HostAddress = %q, want %q", resp.GetHostAddress(), nodeBAddr)
	}

	// Sanity: node-B sees itself as CURRENT for the same federation.
	clientB := nodeB.dialClient(t)
	respB, err := clientB.LookupFederationHost(ctx,
		&rtiv1.LookupFederationHostRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: fedName,
		})
	if err != nil {
		t.Fatalf("LookupFederationHost on node-b: %v", err)
	}
	if respB.GetStatus() != rtiv1.LookupFederationHostResponse_CURRENT {
		t.Errorf("node-b status = %v, want CURRENT", respB.GetStatus())
	}
}

// TestSpec_M15_2_LookupUnknownFederationNotFound verifies the
// negative path: a federation not in either node's table returns
// NOT_FOUND from both nodes.
func TestSpec_M15_2_LookupUnknownFederationNotFound(t *testing.T) {
	listeners := map[string]*bufconn.Listener{}
	nodeA := newBufNode(t, "node-a", "passthrough:///bufnet-a", listeners)
	nodeB := newBufNode(t, "node-b", "passthrough:///bufnet-b", listeners)
	listeners["passthrough:///bufnet-a"] = nodeA.lis
	listeners["passthrough:///bufnet-b"] = nodeB.lis
	t.Cleanup(nodeA.close)
	t.Cleanup(nodeB.close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := nodeA.dialClient(t)
	resp, err := client.LookupFederationHost(ctx,
		&rtiv1.LookupFederationHostRequest{
			WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
			FederationName: "never-created",
		})
	if err != nil {
		t.Fatalf("LookupFederationHost: %v", err)
	}
	if resp.GetStatus() != rtiv1.LookupFederationHostResponse_NOT_FOUND {
		t.Errorf("Status = %v, want NOT_FOUND", resp.GetStatus())
	}
}

// TestSpec_M15_2_ListClusterNodesReturnsBothNodes verifies the
// membership view after RegisterPeer.
func TestSpec_M15_2_ListClusterNodesReturnsBothNodes(t *testing.T) {
	listeners := map[string]*bufconn.Listener{}
	nodeA := newBufNode(t, "node-a", "passthrough:///bufnet-a", listeners)
	nodeB := newBufNode(t, "node-b", "passthrough:///bufnet-b", listeners)
	listeners["passthrough:///bufnet-a"] = nodeA.lis
	listeners["passthrough:///bufnet-b"] = nodeB.lis
	t.Cleanup(nodeA.close)
	t.Cleanup(nodeB.close)

	nodeA.mgr.RegisterPeer("node-b", "passthrough:///bufnet-b")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := nodeA.dialClient(t)
	resp, err := client.ListClusterNodes(ctx,
		&rtiv1.ListClusterNodesRequest{
			WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
		})
	if err != nil {
		t.Fatalf("ListClusterNodes: %v", err)
	}
	if len(resp.GetNodes()) != 2 {
		t.Fatalf("Nodes len = %d, want 2", len(resp.GetNodes()))
	}
	seen := map[string]bool{}
	for _, n := range resp.GetNodes() {
		seen[n.GetNodeId()] = true
	}
	if !seen["node-a"] || !seen["node-b"] {
		t.Errorf("seen = %v, want both node-a + node-b", seen)
	}
}
