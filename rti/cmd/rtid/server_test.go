package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestParseAuditReplayPluginMode(t *testing.T) {
	for _, tc := range []struct {
		input   string
		want    auditReplayPluginMode
		wantErr bool
	}{
		{input: "", want: auditReplayPluginNone},
		{input: "none", want: auditReplayPluginNone},
		{input: "event-journal", want: auditReplayPluginEventJournal},
		{input: "buffered", wantErr: true},
	} {
		got, err := parseAuditReplayPluginMode(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseAuditReplayPluginMode(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("parseAuditReplayPluginMode(%q) = %s, want %s", tc.input, got, tc.want)
		}
	}
}

func TestValidateAuditReplayPluginConfig(t *testing.T) {
	for _, tc := range []struct {
		name      string
		runMode   string
		plugin    auditReplayPluginMode
		logDir    string
		wantError bool
	}{
		{name: "server core", runMode: "server", plugin: auditReplayPluginNone},
		{name: "default server core", runMode: "", plugin: auditReplayPluginNone},
		{name: "core rejects unused log directory", runMode: "server", plugin: auditReplayPluginNone, logDir: "logs", wantError: true},
		{name: "server audit", runMode: "server", plugin: auditReplayPluginEventJournal, logDir: "logs"},
		{name: "audit needs directory", runMode: "server", plugin: auditReplayPluginEventJournal, wantError: true},
		{name: "demo rejects runtime plugin", runMode: "timed-demo", plugin: auditReplayPluginEventJournal, logDir: "logs", wantError: true},
		{name: "unknown plugin", runMode: "server", plugin: auditReplayPluginMode(255), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAuditReplayPluginConfig(tc.runMode, tc.plugin, tc.logDir)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}

func TestNewRTID_HLACoreAndAuditReplayPlugin(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, tc := range []struct {
		name      string
		plugin    auditReplayPluginMode
		wantFiles int
	}{
		{name: "default HLA core creates no journal", plugin: auditReplayPluginNone, wantFiles: 0},
		{name: "audit plugin creates journal", plugin: auditReplayPluginEventJournal, wantFiles: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logDir := t.TempDir()
			srv, err := newRTID(rtidConfig{
				PluginFactories: pluginFactories(tc.plugin, logDir, false),
				Logger:          logger,
			})
			if err != nil {
				t.Fatalf("newRTID: %v", err)
			}
			t.Cleanup(func() { _ = srv.plugins.Close() })

			const fed = core.FederationName("journal-profile")
			ctx := context.Background()
			if err := srv.fedMgr.CreateFederation(ctx, core.CreateFederationRequest{
				Name: fed,
				Mode: core.ModeVerbose,
			}); err != nil {
				t.Fatalf("CreateFederation: %v", err)
			}
			if _, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{
				Federation:   fed,
				FederateName: "member",
			}); err != nil {
				t.Fatalf("JoinFederation: %v", err)
			}

			files, err := filepath.Glob(filepath.Join(logDir, "*", "*.log"))
			if err != nil {
				t.Fatalf("glob journal files: %v", err)
			}
			if len(files) != tc.wantFiles {
				t.Fatalf("journal files = %v, want %d", files, tc.wantFiles)
			}
		})
	}
}

// TestNewRTID_AllComponentsWired: with valid config, newRTID returns
// a runtime exposing fedMgr/declMgr/objReg/plugins/outbox/grpcS/metrics.
func TestNewRTID_AllComponentsWired(t *testing.T) {
	srv, err := newRTID(rtidConfig{
		ListenAddr:        ":0",
		MetricsListenAddr: ":0",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("newRTID: %v", err)
	}
	if srv.fedMgr == nil || srv.declMgr == nil || srv.objReg == nil ||
		srv.plugins == nil || srv.outbox == nil || srv.grpcS == nil ||
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
		if srv.outbox.bufferSize != defaultMultiEventCapacity/defaultMultiBatchSize ||
			srv.outbox.batchSize != defaultMultiBatchSize ||
			srv.outbox.flushInterval != defaultMultiFlushInterval {
			t.Errorf(
				"outbox config = (%d batches, %d events/batch, %s), want (%d, %d, %s)",
				srv.outbox.bufferSize, srv.outbox.batchSize, srv.outbox.flushInterval,
				defaultMultiEventCapacity/defaultMultiBatchSize,
				defaultMultiBatchSize, defaultMultiFlushInterval,
			)
		}
	})

	t.Run("custom values are wired", func(t *testing.T) {
		const eventCapacity = 4096
		const batchSize = 128
		const flushInterval = 5 * time.Millisecond
		srv, err := newRTID(rtidConfig{
			Logger:              logger,
			OutboxEventCapacity: eventCapacity,
			OutboxBatchSize:     batchSize,
			OutboxFlushInterval: flushInterval,
		})
		if err != nil {
			t.Fatalf("newRTID: %v", err)
		}
		if srv.outbox.bufferSize != eventCapacity/batchSize ||
			srv.outbox.batchSize != batchSize || srv.outbox.flushInterval != flushInterval {
			t.Errorf(
				"outbox config = (%d batches, %d events/batch, %s), want (%d, %d, %s)",
				srv.outbox.bufferSize, srv.outbox.batchSize, srv.outbox.flushInterval,
				eventCapacity/batchSize, batchSize, flushInterval,
			)
		}
	})
}

func TestNewRTID_RejectsInvalidOutboxConfig(t *testing.T) {
	for _, cfg := range []rtidConfig{
		{OutboxEventCapacity: maxMultiEventCapacity + 1},
		{OutboxBatchSize: maxMultiBatchSize + 1},
		{OutboxEventCapacity: 31, OutboxBatchSize: 32},
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
		eventCapacity int
		batchSize     int
		flushInterval time.Duration
		wantErr       bool
	}{
		{name: "minimum event capacity", eventCapacity: 1, batchSize: 1, flushInterval: time.Nanosecond},
		{name: "maximum event capacity", eventCapacity: maxMultiEventCapacity, batchSize: 1, flushInterval: time.Second},
		{name: "zero event capacity", eventCapacity: 0, batchSize: 1, flushInterval: time.Millisecond, wantErr: true},
		{name: "event capacity above maximum", eventCapacity: maxMultiEventCapacity + 1, batchSize: 1, flushInterval: time.Millisecond, wantErr: true},
		{name: "minimum batch size", eventCapacity: 1, batchSize: 1, flushInterval: time.Nanosecond},
		{name: "maximum batch size", eventCapacity: maxMultiBatchSize, batchSize: maxMultiBatchSize, flushInterval: time.Second},
		{name: "zero batch size", eventCapacity: 1, batchSize: 0, flushInterval: time.Millisecond, wantErr: true},
		{name: "batch size above maximum", eventCapacity: 1, batchSize: maxMultiBatchSize + 1, flushInterval: time.Millisecond, wantErr: true},
		{name: "capacity below batch size", eventCapacity: 31, batchSize: 32, flushInterval: time.Millisecond, wantErr: true},
		{name: "zero flush interval", eventCapacity: 1, batchSize: 1, flushInterval: 0, wantErr: true},
		{name: "negative flush interval", eventCapacity: 1, batchSize: 1, flushInterval: -time.Nanosecond, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOutboxCLIConfig(tc.eventCapacity, tc.batchSize, tc.flushInterval)
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
