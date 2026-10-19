package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	hlatime "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type queueLogBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *queueLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}

func (b *queueLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

func TestRuntimeOutboxDiagnosticsFollowStartupLogLevel(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelDebug} {
		t.Run(level.String(), func(t *testing.T) {
			var log queueLogBuffer
			logger := slog.New(slog.NewJSONHandler(&log, &slog.HandlerOptions{Level: level}))
			srv, err := newRTID(rtidConfig{Logger: logger, SaveDir: t.TempDir(), OutboxBatchSize: 1})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { srv.grpcS.Stop(); srv.sessions.Close(); _ = srv.plugins.Close() })
			const fed core.FederationName = "queue-diagnostics"
			ctx := context.Background()
			if err := srv.fedMgr.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
				t.Fatal(err)
			}
			h, err := srv.fedMgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "member"})
			if err != nil {
				t.Fatal(err)
			}
			observation, err := srv.outbox.ObserveQueue(fed, h)
			if level == slog.LevelInfo {
				if !errors.Is(err, delivery.ErrQueueObservationDisabled) {
					t.Fatalf("default accounting enabled: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err := srv.outbox.Send(ctx, fed, h, &hlatime.TimeAdvanceGrant{}); err != nil {
				t.Fatal(err)
			}
			if err := srv.fedMgr.ResignFederation(ctx, fed, h, core.ResignActionNoAction); err != nil {
				t.Fatal(err)
			}
			if level == slog.LevelInfo {
				if strings.Contains(log.String(), "outbox-only") {
					t.Fatal("default execution produced queue diagnostics")
				}
				return
			}
			counts := observation.Inspect()
			if !counts.Closed || counts.Admitted != 1 || counts.Invalidated != 1 || counts.Drained != 0 || counts.Scratch != 0 || counts.Queued != 0 {
				t.Fatalf("unexpected retired snapshot: %+v", counts)
			}
			if !strings.Contains(log.String(), `"scope":"outbox-only"`) || !strings.Contains(log.String(), `"channel_drained":0`) {
				t.Fatal("missing explicitly scoped diagnostic", log.String())
			}
		})
	}
}

func TestRetiredOutboxDiagnosticsRequireClosedObservation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		admitted uint64
		closed   bool
	}{{"open-empty", 0, false}, {"open-queued", 1, false}, {"closed", 1, true}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			outbox := delivery.NewObservedWithBatch(1, 1, 0)
			outbox.Bind("retirement", 1)
			t.Cleanup(func() { outbox.Unbind("retirement", 1) })
			observation, err := outbox.ObserveQueue("retirement", 1)
			if err != nil {
				t.Fatal(err)
			}
			if tc.admitted != 0 {
				if err := outbox.Send(ctx, "retirement", 1, &hlatime.TimeAdvanceGrant{}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.closed {
				outbox.Unbind("retirement", 1)
			}
			var log queueLogBuffer
			logger := slog.New(slog.NewJSONHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))
			logRetiredOutbox(ctx, logger, observation)
			if !tc.closed {
				if got := log.String(); got != "" {
					t.Fatalf("open observation published as retired: %s", got)
				}
				return
			}
			var record struct {
				Message     string `json:"msg"`
				Scope       string
				Federation  string
				Federate    uint64
				Admitted    uint64
				Invalidated uint64 `json:"observation_invalidated"`
				Scratch     uint64
				Queued      uint64
				Closed      bool
			}
			if err := json.Unmarshal([]byte(log.String()), &record); err != nil {
				t.Fatal(err)
			}
			if record.Message != "retired delivery queue" || record.Scope != "outbox-only" || record.Federation != "retirement" || record.Federate != 1 || !record.Closed || record.Admitted != tc.admitted || record.Invalidated != tc.admitted || record.Scratch != 0 || record.Queued != 0 {
				t.Fatalf("closed snapshot changed: %+v", record)
			}
		})
	}
}
