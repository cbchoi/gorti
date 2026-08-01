// Package client wraps the rti.v1.AdminService gRPC stub for the
// rti-top TUI. The wrapper centralises dial setup, deadline policy,
// and the version-pin (every request carries WIRE_VERSION_V1) so the
// TUI MVU code never touches gRPC directly.
//
// Read-only by default — Phase 1 of docs/rtid-tui.md is read-only
// (Snapshot / TailEvents / Status). Phase 5 adds an OPT-IN MutatingService
// probe + ForceResign + DestroyFederation; the daemon registers the
// service only when --admin-mutating=true (docs/rtid-tui.md §7.5),
// and rti-top's TUI hides the X / D keybindings until the probe
// succeeds.
package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// Client is the typed AdminService façade. Holds the underlying
// *grpc.ClientConn so callers can Close() once the program exits.
type Client struct {
	conn   *grpc.ClientConn
	stub   rtiv1.AdminServiceClient
	mutate rtiv1.MutatingServiceClient
	target string
}

// Dial establishes a connection to the AdminService listener at addr
// (typically `localhost:8443`). Plaintext only — Phase 1's admin
// listener is plaintext per docs/rtid-tui.md §2.5 (mTLS deferred to
// cut-3). The first Status() roundtrip should be used as the
// liveness probe before entering the TUI loop.
func Dial(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("rti-top: dial admin %q: %w", addr, err)
	}
	return &Client{
		conn:   conn,
		stub:   rtiv1.NewAdminServiceClient(conn),
		mutate: rtiv1.NewMutatingServiceClient(conn),
		target: addr,
	}, nil
}

// Target returns the dial address for log/diagnostic strings.
func (c *Client) Target() string { return c.target }

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Status calls AdminService.Status with a 3-second deadline. Used at
// startup as a liveness probe; the response also seeds the TUI's
// version + uptime header before the first Snapshot lands.
func (c *Client) Status(ctx context.Context) (*rtiv1.StatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp, err := c.stub.Status(ctx, &rtiv1.StatusRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err != nil {
		return nil, fmt.Errorf("Status: %w", err)
	}
	return resp, nil
}

// Snapshot calls AdminService.Snapshot with a deadline equal to the
// poll interval (so a stuck server cannot back up the TUI's polling
// goroutine). When federation is empty, every federation is returned;
// otherwise only the named one.
func (c *Client) Snapshot(ctx context.Context, federation string, deadline time.Duration) (*rtiv1.SnapshotResponse, error) {
	if deadline <= 0 {
		deadline = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	resp, err := c.stub.Snapshot(ctx, &rtiv1.SnapshotRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: federation,
	})
	if err != nil {
		return nil, fmt.Errorf("Snapshot: %w", err)
	}
	return resp, nil
}

// TailEvents opens a server-streaming subscription on the named
// federation. Caller cancels via the supplied context. The wrapper
// returns the typed gRPC stream so the consumer reads
// TailEventsResponse messages directly (no intermediate channel).
//
// Phase 4: the server-side handler now batches events and applies
// optional class / handle filters server-side. Use TailEventsFiltered
// when those filters are needed; this wrapper ships a no-filter
// request and accepts whatever batching defaults the server picks.
func (c *Client) TailEvents(ctx context.Context, federation string) (grpc.ServerStreamingClient[rtiv1.TailEventsResponse], error) {
	return c.TailEventsFiltered(ctx, federation, "", nil)
}

// TailEventsFiltered opens a server-streaming subscription with
// server-side filters (Phase 4 of docs/rtid-tui.md).
//
//   - classFilter: case-sensitive substring match against the event's
//     class name; empty → no class filter.
//   - handleFilter: whitelist of federate handles; nil/empty → no
//     handle filter. Federation-scope events bypass this filter.
//
// The batching knobs are left at server defaults (max 32 events, max
// 10 ms latency); callers that need other batching should construct
// the request themselves and call the underlying stub directly.
func (c *Client) TailEventsFiltered(
	ctx context.Context,
	federation, classFilter string,
	handleFilter []uint64,
) (grpc.ServerStreamingClient[rtiv1.TailEventsResponse], error) {
	stream, err := c.stub.TailEvents(ctx, &rtiv1.TailEventsRequest{
		WireVersion:          rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:       federation,
		EventClassFilter:     classFilter,
		FederateHandleFilter: handleFilter,
	})
	if err != nil {
		return nil, fmt.Errorf("TailEvents: %w", err)
	}
	return stream, nil
}

// --- MutatingService (Phase 5 of docs/rtid-tui.md) -------------------------

// ProbeMutating tries MutatingService.Probe and reports whether the
// daemon registered the service. Returns (true, nil) when the probe
// succeeds, (false, nil) when the service is Unimplemented (the
// expected outcome on a read-only daemon), and (false, err) on any
// other transport error so callers can surface a misconfig instead of
// silently hiding the keys.
func (c *Client) ProbeMutating(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp, err := c.mutate.Probe(ctx, &rtiv1.MutatingProbeRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err != nil {
		switch status.Code(err) {
		case codes.Unimplemented, codes.NotFound:
			return false, nil
		}
		return false, fmt.Errorf("ProbeMutating: %w", err)
	}
	return resp.GetMutatingEnabled(), nil
}

// ForceResign calls MutatingService.ForceResign with a 3-second
// deadline. Caller is expected to render a confirmation dialog
// before invoking — there's no second-guessing inside the wrapper.
func (c *Client) ForceResign(ctx context.Context, federation string, handle uint64) (*rtiv1.ForceResignResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp, err := c.mutate.ForceResign(ctx, &rtiv1.ForceResignRequest{
		WireVersion:    rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName: federation,
		FederateHandle: handle,
	})
	if err != nil {
		return nil, fmt.Errorf("ForceResign: %w", err)
	}
	return resp, nil
}

// DestroyFederation calls MutatingService.DestroyFederation with a
// 5-second deadline (longer than ForceResign because the handler may
// have to evict joined federates first). evictJoined controls
// whether to first force-resign every joined federate or refuse on
// the federate-facing FR-FM-5 contract.
func (c *Client) DestroyFederation(ctx context.Context, federation string, evictJoined bool) (*rtiv1.AdminDestroyFederationResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := c.mutate.DestroyFederation(ctx, &rtiv1.AdminDestroyFederationRequest{
		WireVersion:          rtiv1.WireVersion_WIRE_VERSION_V1,
		FederationName:       federation,
		EvictJoinedFederates: evictJoined,
	})
	if err != nil {
		return nil, fmt.Errorf("DestroyFederation: %w", err)
	}
	return resp, nil
}
