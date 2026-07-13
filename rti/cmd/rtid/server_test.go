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

func TestNewRTID_OutboxConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("zero values preserve defaults", func(t *testing.T) {
		srv, err := newRTID(rtidConfig{Logger: logger})
		if err != nil {
			t.Fatalf("newRTID: %v", err)
		}
		if srv.outbox.batchSize != defaultMultiBatchSize || srv.outbox.flushInterval != defaultMultiFlushInterval {
			t.Errorf("outbox config = (%d, %s), want (%d, %s)", srv.outbox.batchSize, srv.outbox.flushInterval, defaultMultiBatchSize, defaultMultiFlushInterval)
		}
	})

	t.Run("custom values are wired", func(t *testing.T) {
		const batchSize = 128
		const flushInterval = 5 * time.Millisecond
		srv, err := newRTID(rtidConfig{
			Logger:              logger,
			OutboxBatchSize:     batchSize,
			OutboxFlushInterval: flushInterval,
		})
		if err != nil {
			t.Fatalf("newRTID: %v", err)
		}
		if srv.outbox.batchSize != batchSize || srv.outbox.flushInterval != flushInterval {
			t.Errorf("outbox config = (%d, %s), want (%d, %s)", srv.outbox.batchSize, srv.outbox.flushInterval, batchSize, flushInterval)
		}
	})
}

func TestNewRTID_RejectsInvalidOutboxConfig(t *testing.T) {
	for _, cfg := range []rtidConfig{
		{OutboxBatchSize: maxMultiBatchSize + 1},
		{OutboxFlushInterval: -time.Nanosecond},
	} {
		if _, err := newRTID(cfg); err == nil {
			t.Errorf("newRTID(%+v) returned nil error", cfg)
		}
	}
}

func TestValidateOutboxCLIConfig(t *testing.T) {
	for _, tc := range []struct {
		name          string
		batchSize     int
		flushInterval time.Duration
		wantErr       bool
	}{
		{name: "minimum batch size", batchSize: 1, flushInterval: time.Nanosecond},
		{name: "maximum batch size", batchSize: maxMultiBatchSize, flushInterval: time.Second},
		{name: "zero batch size", batchSize: 0, flushInterval: time.Millisecond, wantErr: true},
		{name: "batch size above maximum", batchSize: maxMultiBatchSize + 1, flushInterval: time.Millisecond, wantErr: true},
		{name: "zero flush interval", batchSize: 1, flushInterval: 0, wantErr: true},
		{name: "negative flush interval", batchSize: 1, flushInterval: -time.Nanosecond, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOutboxCLIConfig(tc.batchSize, tc.flushInterval)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateOutboxCLIConfig() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
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
