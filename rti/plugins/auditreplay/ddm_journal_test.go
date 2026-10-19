package auditreplay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/eventlog"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/runtimeplugin"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestScopedRegistrationAuditFailureDoesNotBlockCore(t *testing.T) {
	for _, warm := range []bool{false, true} {
		name := "unopened_journal"
		if warm {
			name = "existing_journal"
		}
		t.Run(name, func(t *testing.T) {
			const fed core.FederationName = "scoped-audit"
			const generation = 1
			dir := t.TempDir()
			var warnings bytes.Buffer
			plugin, err := New(Config{Dir: dir}, runtimeplugin.Host{
				Clock:  core.NewManualClock(time.Unix(0, 0)),
				Logger: slog.New(slog.NewJSONHandler(&warnings, nil)),
				Metadata: func(core.FederationName) (uint64, core.Mode, uint64, bool) {
					return generation, core.ModeVerbose, 1516, true
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer plugin.Close()
			ctx := context.Background()
			observer := plugin.EventLog()
			path := eventlog.GenerationLogPath(dir, fed, generation)
			var before []byte
			var wantObjects []core.ObjectHandle
			if warm {
				if err := observer.Append(ctx, fed, &core.ObjectJournalRecord{Kind: core.ObjectJournalRegistered, Object: 11}); err != nil {
					t.Fatal(err)
				}
				before, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				wantObjects = append(wantObjects, 11)
			}
			scoped := &core.ObjectJournalRecord{
				Sequence: 999, Kind: core.ObjectJournalRegistered, Object: 42,
				ObjectClass: 7, Federate: 3, ObjectName: "scoped", RegionScoped: true,
			}
			if err := observer.Append(ctx, fed, scoped); err != nil {
				t.Fatalf("observer exposed unsupported audit encoding to core: %v", err)
			}
			status := plugin.Status()
			if status.ErrorCount != 1 || !errors.Is(status.LastError, core.ErrDDMJournalUnsupported) {
				t.Fatalf("status = %+v, want one ErrDDMJournalUnsupported failure", status)
			}
			if scoped.Sequence != 999 {
				t.Fatalf("unsupported record sequence = %d, want unchanged 999", scoped.Sequence)
			}
			var warning struct {
				Level      string `json:"level"`
				Operation  string `json:"operation"`
				Federation string `json:"federation"`
				Error      string `json:"err"`
			}
			if err := json.Unmarshal(warnings.Bytes(), &warning); err != nil {
				t.Fatalf("missing or invalid audit warning: %v", err)
			}
			if warning.Level != "WARN" || warning.Operation != "append" || warning.Federation != string(fed) || warning.Error != core.ErrDDMJournalUnsupported.Error() {
				t.Fatalf("unexpected audit warning: %+v", warning)
			}
			if warm {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("scoped registration changed existing audit bytes: %v", err)
				}
			} else {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("scoped registration created an audit file or directory: entries=%v err=%v", entries, err)
				}
			}
			ordinary := &core.ObjectJournalRecord{Kind: core.ObjectJournalRegistered, Object: 73}
			if err := observer.Append(ctx, fed, ordinary); err != nil {
				t.Fatal(err)
			}
			wantObjects = append(wantObjects, 73)
			if ordinary.Sequence != uint64(len(wantObjects)) {
				t.Fatalf("ordinary sequence after audit failure = %d, want %d", ordinary.Sequence, len(wantObjects))
			}
			reader, err := plugin.AdminEventLog().OpenReader(ctx, string(fed))
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			for i, object := range wantObjects {
				record, err := reader.Next(ctx)
				if err != nil {
					t.Fatal(err)
				}
				wire, ok := record.(interface{ ProtoEvent() *rtiv1.Event })
				if !ok || record.Seq() != uint64(i+1) || wire.ProtoEvent().GetObjRegistered().GetObjectHandle() != uint64(object) {
					t.Fatalf("readback %d = %v, want ordinary object %d", i, record, object)
				}
			}
			if _, err := reader.Next(ctx); !errors.Is(err, io.EOF) {
				t.Fatalf("unexpected audit frame after ordinary registrations: %v", err)
			}
			if status := plugin.Status(); status.ErrorCount != 1 || !errors.Is(status.LastError, core.ErrDDMJournalUnsupported) {
				t.Fatalf("ordinary append lost the recorded audit failure: %+v", status)
			}
		})
	}
}
