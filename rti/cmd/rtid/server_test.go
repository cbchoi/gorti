package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

// TestNewRTID_AllComponentsWired: with valid config, newRTID returns
// a runtime exposing fedMgr/declMgr/objReg/multi/outbox/grpcS/metrics.
func TestNewRTID_AllComponentsWired(t *testing.T) {
	srv, err := newRTID(rtidConfig{
		ListenAddr:        ":0",
		MetricsListenAddr: ":0",
		LogDir:            "",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	if srv.fedMgr == nil || srv.declMgr == nil || srv.objReg == nil ||
		srv.multi == nil || srv.outbox == nil || srv.grpcS == nil ||
		srv.metrics == nil || srv.foms == nil {
		t.Errorf("newRTID returned an incompletely wired runtime: %+v", srv)
	}
}

// TestNewRTID_NilLoggerDefaults: a nil Logger is replaced by slog.Default().
func TestNewRTID_NilLoggerDefaults(t *testing.T) {
	srv, err := newRTID(rtidConfig{
		ListenAddr:        ":0",
		MetricsListenAddr: ":0",
		Logger:            nil,
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	if srv.logger == nil {
		t.Errorf("nil Logger was not defaulted")
	}
}

// TestRTIDServe_StartsAndShutsDownCleanly: Serve listens, then exits
// when ctx is canceled.
func TestRTIDServe_StartsAndShutsDownCleanly(t *testing.T) {
	srv, err := newRTID(rtidConfig{
		ListenAddr:        "127.0.0.1:0",
		MetricsListenAddr: "127.0.0.1:0",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	// Give Serve a moment to bind both listeners.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Serve returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not exit within 5s of cancel")
	}
}

// TestRTIDServe_RejectsBadListen: a malformed listen address returns
// an error from Serve before any goroutine starts.
func TestRTIDServe_RejectsBadListen(t *testing.T) {
	srv, err := newRTID(rtidConfig{
		ListenAddr:        "not-a-valid-address",
		MetricsListenAddr: "127.0.0.1:0",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	if err := srv.Serve(context.Background()); err == nil {
		t.Errorf("Serve with bad listen returned nil error")
	}
}

