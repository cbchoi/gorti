package federate

import (
	"context"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery/callbackprofile"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Reporting uses the raw channel. Other SDK calls wait for preceding report
// ACKs, preventing a query or reentrant service from overtaking callback state.
type callbackOrderedClient struct {
	*grpc.ClientConn
	owner *Connection
}

func (c *Connection) rpcConnection() grpc.ClientConnInterface {
	if !callbackprofile.Async {
		return c.cc
	}
	return &callbackOrderedClient{ClientConn: c.cc, owner: c}
}

func (c *callbackOrderedClient) Invoke(ctx context.Context, method string, request, response any, options ...grpc.CallOption) error {
	if err := c.owner.awaitCallbackReports(ctx, request); err != nil {
		return err
	}
	return c.ClientConn.Invoke(ctx, method, request, response, options...)
}

func (c *callbackOrderedClient) NewStream(ctx context.Context, descriptor *grpc.StreamDesc, method string, options ...grpc.CallOption) (grpc.ClientStream, error) {
	stream, err := c.ClientConn.NewStream(ctx, descriptor, method, options...)
	if err != nil {
		return nil, err
	}
	return &callbackOrderedStream{ClientStream: stream, owner: c.owner}, nil
}

type callbackOrderedStream struct {
	grpc.ClientStream
	owner *Connection
}

func (s *callbackOrderedStream) SendMsg(message any) error {
	if err := s.owner.awaitCallbackReports(s.Context(), message); err != nil {
		return err
	}
	return s.ClientStream.SendMsg(message)
}

func (c *Connection) reportTransports() []*callbackReportTransport {
	c.callbackOrderMu.Lock()
	defer c.callbackOrderMu.Unlock()
	var result []*callbackReportTransport
	for q := range c.callbackOrder {
		if q.ctx.Err() != nil {
			delete(c.callbackOrder, q)
			continue
		}
		result = append(result, q)
	}
	return result
}

func callbackReportCaller(ctx context.Context, request any, q *callbackReportTransport) bool {
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		if tokens := md.Get("gorti-session-token-bin"); len(tokens) == 1 {
			return tokens[0] == string(q.f.sessionToken)
		}
	}
	if scope, ok := request.(interface{ GetFederationName() string }); ok {
		if name := scope.GetFederationName(); name != "" && name != q.f.federationName {
			return false
		}
	}
	if caller, ok := request.(interface{ GetFederateHandle() uint64 }); ok {
		if h := caller.GetFederateHandle(); h != 0 {
			return h == q.f.CurrentHandle()
		}
	}
	return true
}

func (c *Connection) awaitCallbackReports(ctx context.Context, request any) error {
	for _, q := range c.reportTransports() {
		if !callbackReportCaller(ctx, request, q) {
			continue
		}
		q.mu.Lock()
		if q.asyncError != nil {
			err := q.asyncError
			q.mu.Unlock()
			return err
		}
		pending := append([]*callbackReportTask(nil), q.queue...)
		q.mu.Unlock()
		for _, task := range pending {
			select {
			case <-task.ack.wait():
				if task.err != nil {
					return task.err
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

func (c *Connection) registerCallbackReports(q *callbackReportTransport) {
	c.callbackOrderMu.Lock()
	defer c.callbackOrderMu.Unlock()
	if c.callbackOrder == nil {
		c.callbackOrder = make(map[*callbackReportTransport]struct{})
	}
	c.callbackOrder[q] = struct{}{}
}

func (c *Connection) installCallbackOrdering() {
	if !callbackprofile.Async {
		return
	}
	rpc := c.rpcConnection()
	c.fed = rtiv1.NewFederationServiceClient(rpc)
	c.decl = rtiv1.NewDeclarationServiceClient(rpc)
	c.obj = rtiv1.NewObjectServiceClient(rpc)
	c.confirmedObject = rtiv1.NewConfirmedObjectServiceClient(rpc)
	c.localLRC = rtiv1.NewLocalLRCServiceClient(rpc)
	c.stream = rtiv1.NewStreamServiceClient(rpc)
	c.sync = rtiv1.NewSyncServiceClient(rpc)
	c.savepoint = rtiv1.NewSavepointServiceClient(rpc)
	c.tm = rtiv1.NewTimeServiceClient(rpc)
	c.ddm = rtiv1.NewDDMServiceClient(rpc)
	c.cluster = rtiv1.NewClusterServiceClient(rpc)
}
