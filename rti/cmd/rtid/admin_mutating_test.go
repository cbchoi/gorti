// rtid-TUI Phase 5 — verifies the --admin-mutating composition-root
// gate: MutatingService is unregistered by default, registered when
// --admin-mutating=true, and isLoopbackBind correctly classifies the
// flag's safety state.

package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// TestIsLoopbackBind: the gate decision function classifies the
// expected hosts. DNS lookups are intentionally avoided so this stays
// byte-deterministic from the flag.
func TestIsLoopbackBind(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"localhost:8443", true},
		{"127.0.0.1:8443", true},
		{"[::1]:8443", true},
		{"0.0.0.0:8443", false},
		{":8443", false},
		{"10.0.0.5:8443", false},
		{"example.com:8443", false},
		{"not-an-addr", false},
		{"", false},
	}
	for _, c := range cases {
		got := isLoopbackBind(c.addr)
		if got != c.want {
			t.Errorf("isLoopbackBind(%q): got %v want %v", c.addr, got, c.want)
		}
	}
}

// TestAdminMutating_DefaultUnregistered: with --admin-mutating=false
// (the default), MutatingService.Probe returns Unimplemented. rti-top
// uses this to decide whether to render X / D keybindings.
func TestAdminMutating_DefaultUnregistered(t *testing.T) {
	const adminAddr = "127.0.0.1:18454"
	srv, err := newRTID(rtidConfig{
		ListenAddr:        "127.0.0.1:0",
		AdminListenAddr:   adminAddr,
		MetricsListenAddr: "127.0.0.1:0",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		// AdminMutating defaults to false — MutatingService should NOT
		// be registered.
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("Serve did not exit after cancel")
		}
	}()

	conn, ok := dialAdminWithRetry(t, adminAddr, 2*time.Second)
	if !ok {
		t.Skipf("admin port %s not reachable", adminAddr)
	}
	defer conn.Close()

	client := rtiv1.NewMutatingServiceClient(conn)
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer probeCancel()
	_, err = client.Probe(probeCtx, &rtiv1.MutatingProbeRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err == nil {
		t.Fatalf("MutatingService.Probe with --admin-mutating=false: want Unimplemented, got nil")
	}
	if got := status.Code(err); got != codes.Unimplemented {
		t.Errorf("Probe error code: got %v want Unimplemented", got)
	}
}

// TestAdminMutating_Registered: with AdminMutating=true the
// MutatingService is registered and Probe returns the version.
func TestAdminMutating_Registered(t *testing.T) {
	const adminAddr = "127.0.0.1:18455"
	srv, err := newRTID(rtidConfig{
		ListenAddr:        "127.0.0.1:0",
		AdminListenAddr:   adminAddr,
		MetricsListenAddr: "127.0.0.1:0",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		AdminMutating:     true,
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("Serve did not exit after cancel")
		}
	}()

	conn, ok := dialAdminWithRetry(t, adminAddr, 2*time.Second)
	if !ok {
		t.Skipf("admin port %s not reachable", adminAddr)
	}
	defer conn.Close()

	client := rtiv1.NewMutatingServiceClient(conn)
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer probeCancel()
	resp, err := client.Probe(probeCtx, &rtiv1.MutatingProbeRequest{
		WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1,
	})
	if err != nil {
		t.Fatalf("MutatingService.Probe: %v", err)
	}
	if !resp.GetMutatingEnabled() {
		t.Errorf("Probe response: mutating_enabled=false; want true")
	}
	if resp.GetRtidVersion() == "" {
		t.Errorf("Probe response: empty rtid_version")
	}
}

// dialAdminWithRetry mirrors the helper inlined in
// admin_listener_test.go so the new tests don't introduce a hidden
// dependency on TestAdminListener_StatusE2E running first.
func dialAdminWithRetry(t *testing.T, addr string, total time.Duration) (*grpc.ClientConn, bool) {
	t.Helper()
	deadline := time.Now().Add(total)
	for time.Now().Before(deadline) {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			return conn, true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, false
}
