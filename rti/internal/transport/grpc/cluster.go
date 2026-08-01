// ClusterService gRPC handler (M15 cut-2 demo).
//
// Thin adapter over rti/internal/cluster.Manager — translates wire
// types to/from Manager methods. Multi-node behavior comes from
// the Manager's state which is mutated by:
//   - rtid bootstrap (RegisterPeer for each --cluster-peers entry)
//   - federation creation hook (AssignFederation)
//   - incoming NotifyAssignment RPCs (RecordAssignment)
//
// No persistence, no health checks, no replication. M15 cut-3 will
// swap the Manager for a Raft-backed implementation.

package grpc

import (
	"context"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/cluster"
	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// PeerDialer dials a peer rtid by address for NotifyAssignment fan-out.
// Default uses grpc.NewClient with insecure transport (M15 cut-2
// demo); production builds wrap with TLS via the existing transport
// credentials plumbing.
type PeerDialer func(address string) (*grpc.ClientConn, error)

func defaultPeerDialer(address string) (*grpc.ClientConn, error) {
	return grpc.NewClient(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

// ClusterService is the gRPC handler exposing rti/internal/cluster.Manager
// over the wire. M15 cut-2 demo. Construct via NewClusterService.
type ClusterService struct {
	rtiv1.UnimplementedClusterServiceServer
	mgr           *cluster.Manager
	dialer        PeerDialer
	generationFor func(core.FederationName) (uint64, bool)

	// Lazy peer-connection cache. Connections stay open across
	// requests; the rtid lifecycle owns teardown via Close().
	connMu sync.Mutex
	conns  map[string]*grpc.ClientConn
}

func (s *ClusterService) SetGenerationResolver(resolver func(core.FederationName) (uint64, bool)) {
	s.generationFor = resolver
}

func (s *ClusterService) AssignLocalFederation(fed core.FederationName) {
	var generation uint64
	if s.generationFor != nil {
		generation, _ = s.generationFor(fed)
	}
	s.mgr.AssignFederationGeneration(fed, generation)
}

// NewClusterService constructs the gRPC adapter. “dialer“ may be
// nil to use the default insecure dialer.
func NewClusterService(mgr *cluster.Manager, dialer PeerDialer) *ClusterService {
	if dialer == nil {
		dialer = defaultPeerDialer
	}
	return &ClusterService{
		mgr:    mgr,
		dialer: dialer,
		conns:  map[string]*grpc.ClientConn{},
	}
}

// RegisterWith attaches the handler to a gRPC server. Mirrors the
// existing service-registration pattern in this package.
func (s *ClusterService) RegisterWith(srv *grpc.Server) {
	rtiv1.RegisterClusterServiceServer(srv, s)
}

// Close releases any cached peer connections.
func (s *ClusterService) Close() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	for _, c := range s.conns {
		_ = c.Close()
	}
	s.conns = nil
	return nil
}

func (s *ClusterService) ListClusterNodes(
	_ context.Context, req *rtiv1.ListClusterNodesRequest,
) (*rtiv1.ListClusterNodesResponse, error) {
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	nodes := s.mgr.Nodes()
	resp := &rtiv1.ListClusterNodesResponse{
		Nodes: make([]*rtiv1.ClusterNode, 0, len(nodes)),
	}
	for _, n := range nodes {
		resp.Nodes = append(resp.Nodes, &rtiv1.ClusterNode{
			NodeId:  n.NodeID,
			Address: n.Address,
			IsSelf:  n.IsSelf,
		})
	}
	return resp, nil
}

func (s *ClusterService) LookupFederationHost(
	_ context.Context, req *rtiv1.LookupFederationHostRequest,
) (*rtiv1.LookupFederationHostResponse, error) {
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	r := s.mgr.Lookup(core.FederationName(req.GetFederationName()))
	switch r.Status {
	case cluster.StatusCurrent:
		return &rtiv1.LookupFederationHostResponse{
			Status:     rtiv1.LookupFederationHostResponse_CURRENT,
			HostNodeId: r.HostNodeID,
		}, nil
	case cluster.StatusRedirect:
		return &rtiv1.LookupFederationHostResponse{
			Status:      rtiv1.LookupFederationHostResponse_REDIRECT,
			HostAddress: r.HostAddress,
			HostNodeId:  r.HostNodeID,
		}, nil
	default:
		return &rtiv1.LookupFederationHostResponse{
			Status: rtiv1.LookupFederationHostResponse_NOT_FOUND,
		}, nil
	}
}

func (s *ClusterService) ReportNodeHealth(
	_ context.Context, req *rtiv1.ReportNodeHealthRequest,
) (*rtiv1.Empty, error) {
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	// Demo cut: accept the heartbeat but don't track liveness.
	// Production cut wires a per-peer last-seen timestamp and a
	// liveness check that flips assignments when peers die.
	return &rtiv1.Empty{}, nil
}

func (s *ClusterService) NotifyAssignment(
	ctx context.Context, req *rtiv1.NotifyAssignmentRequest,
) (*rtiv1.Empty, error) {
	if err := validateWireVersion(req.GetWireVersion()); err != nil {
		return nil, err
	}
	if req.GetFederationName() == "" || req.GetHostNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument,
			"federation_name + host_node_id are required")
	}
	fed := core.FederationName(req.GetFederationName())
	if s.generationFor != nil && req.ExpectedFederationGeneration == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"expected_federation_generation is required")
	}
	generation := req.GetExpectedFederationGeneration()
	if s.generationFor != nil {
		if local, ok := s.generationFor(fed); ok && local != generation {
			return nil, errToStatus(ctx, core.ErrFederationGenerationMismatch)
		}
	}
	if current, ok := s.mgr.AssignmentGeneration(fed); ok && generation < current {
		return nil, errToStatus(ctx, core.ErrFederationGenerationMismatch)
	}
	// Update peer membership first so subsequent Lookup returns the
	// REDIRECT with a non-empty address.
	if req.GetHostAddress() != "" {
		s.mgr.RegisterPeer(req.GetHostNodeId(), req.GetHostAddress())
	}
	if !s.mgr.RecordAssignmentGeneration(fed, req.GetHostNodeId(), generation) {
		return nil, errToStatus(ctx, core.ErrFederationGenerationMismatch)
	}
	return &rtiv1.Empty{}, nil
}

// BroadcastAssignment fans NotifyAssignment to every known peer
// (except self). Called by the federation-creation hook in M15.2.3.
// Best-effort: errors are swallowed; cut-3 will surface them via
// a retry loop tied to Raft commit semantics.
func (s *ClusterService) BroadcastAssignment(
	ctx context.Context,
	federation core.FederationName,
	hostNodeID string,
	hostAddress string,
) {
	req := &rtiv1.NotifyAssignmentRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FromNodeId:     s.mgr.SelfID(),
		FederationName: string(federation),
		HostNodeId:     hostNodeID,
		HostAddress:    hostAddress,
	}
	if s.generationFor != nil {
		if generation, ok := s.generationFor(federation); ok {
			req.ExpectedFederationGeneration = &generation
		}
	}
	selfID := s.mgr.SelfID()
	for _, n := range s.mgr.Nodes() {
		if n.NodeID == selfID || n.Address == "" {
			continue
		}
		conn, err := s.connTo(n.Address)
		if err != nil {
			continue
		}
		client := rtiv1.NewClusterServiceClient(conn)
		_, _ = client.NotifyAssignment(ctx, req)
	}
}

func (s *ClusterService) connTo(address string) (*grpc.ClientConn, error) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if c, ok := s.conns[address]; ok {
		return c, nil
	}
	c, err := s.dialer(address)
	if err != nil {
		return nil, err
	}
	s.conns[address] = c
	return c, nil
}
