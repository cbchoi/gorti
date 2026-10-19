package eventlog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/protobuf/proto"
)

type ddmJournalSink struct {
	bytes.Buffer
	writes int
}

func (s *ddmJournalSink) Write(p []byte) (int, error) {
	s.writes++
	return s.Buffer.Write(p)
}

func ordinaryObjectJournalRecord() *core.ObjectJournalRecord {
	return &core.ObjectJournalRecord{
		Kind: core.ObjectJournalRegistered, WallNS: 99,
		Object: 1<<40 + 3, ObjectClass: 1<<40 + 7, Federate: 1<<40 + 11,
		ObjectName: "vehicle",
	}
}

func TestDDMJournalWriterRejectsScopedRegistrationWithoutMutation(t *testing.T) {
	for _, warm := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			t.Run(fmt.Sprintf("warm=%t/partial=%t", warm, partial), func(t *testing.T) {
				var sink ddmJournalSink
				w, err := NewWriter(WriterOptions{
					Sink: &sink, Federation: "ddm-journal", Mode: core.ModeVerbose,
					Clock: core.NewManualClock(time.Unix(0, 0)), AllowPartialProto: partial,
				})
				if err != nil {
					t.Fatal(err)
				}
				defer w.Close()
				if warm {
					if err := w.Append(context.Background(), "ddm-journal", ordinaryObjectJournalRecord()); err != nil {
						t.Fatal(err)
					}
				}
				beforeBytes := bytes.Clone(sink.Bytes())
				beforeWrites, beforeSeq := sink.writes, w.nextSeq
				beforeScratch := bytes.Clone(w.frameScratch[:cap(w.frameScratch)])
				beforeScratchLen, beforeScratchCap := len(w.frameScratch), cap(w.frameScratch)
				for _, sequence := range []uint64{0, 999} {
					record := ordinaryObjectJournalRecord()
					record.Sequence = sequence
					record.RegionScoped = true
					err := w.Append(context.Background(), "ddm-journal", record)
					if !errors.Is(err, core.ErrDDMJournalUnsupported) {
						t.Errorf("scoped Append = %v, want ErrDDMJournalUnsupported", err)
					}
					if record.Sequence != sequence {
						t.Errorf("rejected record sequence = %d, want unchanged %d", record.Sequence, sequence)
					}
					if w.nextSeq != beforeSeq {
						t.Errorf("writer sequence = %d, want unchanged %d", w.nextSeq, beforeSeq)
					}
					if sink.writes != beforeWrites || !bytes.Equal(sink.Bytes(), beforeBytes) {
						t.Error("rejected scoped record wrote an event frame")
					}
					if len(w.frameScratch) != beforeScratchLen || cap(w.frameScratch) != beforeScratchCap || !bytes.Equal(w.frameScratch[:cap(w.frameScratch)], beforeScratch) {
						t.Error("rejected scoped record mutated the frame scratch buffer")
					}
				}
				next := ordinaryObjectJournalRecord()
				if err := w.Append(context.Background(), "ddm-journal", next); err != nil {
					t.Fatal(err)
				}
				if next.Sequence != beforeSeq+1 {
					t.Errorf("next ordinary sequence = %d, want %d", next.Sequence, beforeSeq+1)
				}
				r, err := NewReader(bytes.NewReader(sink.Bytes()))
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				for seq := uint64(1); seq <= beforeSeq+1; seq++ {
					record, err := r.Next(context.Background())
					if err != nil || record.Seq() != seq {
						t.Fatalf("readback sequence %d = %v, %v", seq, record, err)
					}
				}
				if _, err := r.Next(context.Background()); !errors.Is(err, io.EOF) {
					t.Errorf("unexpected trailing record after rejection: %v", err)
				}
			})
		}
	}
}

func TestDDMJournalCodecRejectsScopedRegistration(t *testing.T) {
	record := ordinaryObjectJournalRecord()
	record.Sequence = 999
	record.RegionScoped = true
	message, err := wireMessageForRecord(record)
	if !errors.Is(err, core.ErrDDMJournalUnsupported) {
		t.Fatalf("codec error = %v, want ErrDDMJournalUnsupported", err)
	}
	if message != nil || record.Sequence != 999 {
		t.Fatalf("rejected codec result = %v, sequence = %d", message, record.Sequence)
	}
}

func TestDDMJournalOrdinaryRegistrationPreservesFrozenBytes(t *testing.T) {
	var current, legacy bytes.Buffer
	currentWriter := newWriterForTest(t, "ordinary-journal", &current)
	legacyWriter := newWriterForTest(t, "ordinary-journal", &legacy)
	defer currentWriter.Close()
	defer legacyWriter.Close()
	for i := 0; i < 2; i++ {
		record := ordinaryObjectJournalRecord()
		record.Sequence = 999
		record.Object += core.ObjectHandle(i)
		wire := &rtiv1.Event{WallNs: record.WallNS, Body: &rtiv1.Event_ObjRegistered{ObjRegistered: &rtiv1.ObjectRegistered{
			ObjectHandle: uint64(record.Object), ObjectClassHandle: uint64(record.ObjectClass),
			OwnerFederateHandle: uint64(record.Federate), ObjectName: record.ObjectName,
		}}}
		if err := currentWriter.Append(context.Background(), "ordinary-journal", record); err != nil {
			t.Fatal(err)
		}
		if err := legacyWriter.Append(context.Background(), "ordinary-journal", &productionEvent{Event: wire}); err != nil {
			t.Fatal(err)
		}
		if record.Sequence != uint64(i+1) {
			t.Fatalf("ordinary sequence = %d, want %d", record.Sequence, i+1)
		}
	}
	if !bytes.Equal(current.Bytes(), legacy.Bytes()) {
		t.Fatalf("ordinary registration bytes changed:\ncurrent=%x\nlegacy=%x", current.Bytes(), legacy.Bytes())
	}
}

func TestDDMJournalFlagDoesNotChangeOtherObjectRecords(t *testing.T) {
	for _, kind := range []core.ObjectJournalKind{core.ObjectJournalAttributeUpdated, core.ObjectJournalInteractionSent, core.ObjectJournalDeleted} {
		record := &core.ObjectJournalRecord{Kind: kind, Sequence: 7, Object: 11, Federate: 3, InteractionClass: 9}
		ordinary, err := wireMessageForRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		record.RegionScoped = true
		flagged, err := wireMessageForRecord(record)
		if err != nil || !proto.Equal(flagged, ordinary) {
			t.Fatalf("kind %d changed with registration-only flag: %v", kind, err)
		}
	}
}

func TestDDMJournalMultiplexRejectsBeforeOpeningWriter(t *testing.T) {
	metadataCalls, factoryCalls := 0, 0
	mux, err := NewMultiplexWriter(MultiplexOptions{
		Clock: core.NewManualClock(time.Unix(0, 0)),
		Metadata: func(core.FederationName) (uint64, core.Mode, uint64, bool) {
			metadataCalls++
			return 1, core.ModeVerbose, 1, true
		},
		Factory: func(WriterOptions) (*Writer, error) {
			factoryCalls++
			return nil, errors.New("factory must not be called")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mux.Close()
	record := ordinaryObjectJournalRecord()
	record.Sequence = 999
	record.RegionScoped = true
	if err := mux.Append(context.Background(), "ddm-journal", record); !errors.Is(err, core.ErrDDMJournalUnsupported) {
		t.Errorf("multiplex rejection = %v, want ErrDDMJournalUnsupported", err)
	}
	if metadataCalls != 0 || factoryCalls != 0 || len(mux.writers) != 0 || record.Sequence != 999 {
		t.Fatalf("rejection side effects: metadata=%d factory=%d writers=%d sequence=%d", metadataCalls, factoryCalls, len(mux.writers), record.Sequence)
	}
}

func TestDDMJournalMultiplexRejectsWithoutCreatingFileOrHeader(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "journal")
	mux, err := NewMultiplexWriter(MultiplexOptions{
		Clock: core.NewManualClock(time.Unix(0, 0)), Mode: core.ModeVerbose, Dir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mux.Close()
	ctx := context.Background()
	const fed core.FederationName = "ddm-journal"
	for _, sequence := range []uint64{0, 999} {
		record := ordinaryObjectJournalRecord()
		record.Sequence, record.RegionScoped = sequence, true
		if err := mux.Append(ctx, fed, record); !errors.Is(err, core.ErrDDMJournalUnsupported) {
			t.Errorf("cold multiplex rejection = %v", err)
		}
		if record.Sequence != sequence || len(mux.writers) != 0 {
			t.Errorf("cold rejection changed sequence or writer table: sequence=%d writers=%d", record.Sequence, len(mux.writers))
		}
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("cold rejection created journal directory or file: stat error=%v", err)
		}
	}
	ordinary := ordinaryObjectJournalRecord()
	if err := mux.Append(ctx, fed, ordinary); err != nil {
		t.Fatal(err)
	}
	if ordinary.Sequence != 1 {
		t.Fatalf("first ordinary sequence = %d, want 1", ordinary.Sequence)
	}
	path := GenerationLogPath(dir, fed, 0)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	scoped := ordinaryObjectJournalRecord()
	scoped.Sequence, scoped.RegionScoped = 999, true
	if err := mux.Append(ctx, fed, scoped); !errors.Is(err, core.ErrDDMJournalUnsupported) {
		t.Errorf("warm multiplex rejection = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || scoped.Sequence != 999 || mux.writers[fed].nextSeq != 1 {
		t.Error("warm rejection mutated file or sequences")
	}
	r, err := mux.OpenReader(ctx, string(fed))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if event, err := r.Next(ctx); err != nil || event.Seq() != 1 {
		t.Fatalf("ordinary readback = %v, %v", event, err)
	}
	if _, err := r.Next(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected frame after rejected registrations: %v", err)
	}
}

func TestDDMJournalClosedWritersRemainClosed(t *testing.T) {
	var sink bytes.Buffer
	w := newWriterForTest(t, "ddm-journal", &sink)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	mux, _ := newMultiplexForTest(t)
	if err := mux.Close(); err != nil {
		t.Fatal(err)
	}
	record := ordinaryObjectJournalRecord()
	record.RegionScoped = true
	if err := w.Append(context.Background(), "ddm-journal", record); !errors.Is(err, errWriterClosed) {
		t.Errorf("closed writer rejection = %v", err)
	}
	if err := mux.Append(context.Background(), "ddm-journal", record); !errors.Is(err, errMultiplexClosed) {
		t.Errorf("closed multiplex rejection = %v", err)
	}
}
