package auditreplay

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/runtimeplugin"
)

type testRecord struct {
	seq uint64
}

func (r *testRecord) Seq() uint64 { return r.seq }

func TestObserverFailureDoesNotChangeHLACallResult(t *testing.T) {
	plugin, err := New(Config{Dir: t.TempDir()}, runtimeplugin.Host{
		Clock:  core.NewRealClock(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metadata: func(core.FederationName) (uint64, core.Mode, uint64, bool) {
			return 1, core.ModeVerbose, 1516, true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.Close(); err != nil {
		t.Fatal(err)
	}

	err = plugin.EventLog().Append(context.Background(), "fed", &testRecord{})
	if err != nil {
		t.Fatalf("observer exposed plugin failure to HLA caller: %v", err)
	}
	status := plugin.Status()
	if status.ErrorCount != 1 || status.LastError == nil {
		t.Fatalf("status = %+v, want one recorded failure", status)
	}
}

func TestAdminSurfaceReportsStorageFailure(t *testing.T) {
	plugin, err := New(Config{Dir: t.TempDir()}, runtimeplugin.Host{
		Clock:  core.NewRealClock(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := plugin.AdminEventLog().Append(context.Background(), "fed", &testRecord{}); err == nil {
		t.Fatal("admin surface hid a storage failure")
	}
}
