package m2spec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/eventlog"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

// TestSpec_M2_Replay_ByteIdentical is the M2 milestone gate test.
//
// Contract: replay real federation join/resign records through a fresh manager,
// compare exact bytes, and verify the restored roster. This is a bounded
// lifecycle determinism check, not complete object/time replay coverage.
//
// Implements: FR-EVT-3, NFR-DET-2; M2 exit criterion.
func TestSpec_M2_Replay_ByteIdentical(t *testing.T) {
	ctx := context.Background()
	const fed = core.FederationName("replay-src")
	var source, captured bytes.Buffer
	sourceWriter := newReplayWriter(t, fed, &source)
	original := newReplayManager(t, fed, sourceWriter)
	first, err := original.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "alpha", FederateType: "publisher"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := original.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "bravo", FederateType: "subscriber"}); err != nil {
		t.Fatal(err)
	}
	if err := original.ResignFederation(ctx, fed, first, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	if err := sourceWriter.Sync(ctx, fed); err != nil {
		t.Fatal(err)
	}
	w := newReplayWriter(t, fed, &captured)
	replayed := newReplayManager(t, fed, w)
	r, err := eventlog.NewReader(bytes.NewReader(source.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	rep, err := eventlog.NewReplayer(eventlog.ReplayerOptions{
		Source:         r,
		Federation:     replayed,
		CapturingSink:  w,
		CapturedBuffer: &captured,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	srcSum := sha256.Sum256(source.Bytes())
	capSum := sha256.Sum256(captured.Bytes())
	if srcSum != capSum {
		t.Errorf("replay sha256 mismatch:\n  source:   %x\n  captured: %x", srcSum, capSum)
	}
	if got, want := replayed.ListMembers(fed), original.ListMembers(fed); len(got) != 1 || !reflect.DeepEqual(got, want) {
		t.Fatalf("replayed roster=%+v, want %+v", got, want)
	}
}

func newReplayWriter(t *testing.T, fed core.FederationName, buf *bytes.Buffer) *eventlog.Writer {
	t.Helper()
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink:       buf,
		Federation: fed,
		Mode:       core.ModeVerbose,
		Seed:       1,
		Clock:      core.NewManualClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w
}

func newReplayManager(t *testing.T, fed core.FederationName, log core.EventLog) *federation.Manager {
	t.Helper()
	mgr, err := federation.New(federation.Options{Clock: core.NewManualClock(time.Unix(0, 0)), EventLog: log, FOMs: newFakeFOMRepo()})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{Name: fed, Mode: core.ModeVerbose,
		FOMModules: []core.FOMModule{{Path: "replay.fom", XML: []byte("<objectModel/>")}}}); err != nil {
		t.Fatal(err)
	}
	return mgr
}

// TestSpec_M2_Replay_DivergenceIsFatal: when the replayer detects the
// captured log diverging from the source (e.g. changed or missed
// event), Replay returns ErrReplayDivergence rather than silently
// continuing.
//
// Implements: NFR-DET-2.
func TestSpec_M2_Replay_DivergenceIsFatal(t *testing.T) {
	ctx := context.Background()
	const fed = core.FederationName("replay-divergence")
	var source bytes.Buffer
	srcWriter := newReplayWriter(t, fed, &source)
	srcManager := newReplayManager(t, fed, srcWriter)
	for _, name := range []string{"alpha", "bravo"} {
		if _, err := srcManager.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := srcWriter.Sync(ctx, fed); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"none", "drop", "alter"} {
		t.Run(fault, func(t *testing.T) {
			var captured bytes.Buffer
			writer := newReplayWriter(t, fed, &captured)
			mgr := newReplayManager(t, fed, &replayFaultLog{EventLog: writer, fault: fault})
			reader, err := eventlog.NewReader(bytes.NewReader(source.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			replayer, err := eventlog.NewReplayer(eventlog.ReplayerOptions{Source: reader, Federation: mgr,
				CapturingSink: writer, CapturedBuffer: &captured})
			if err != nil {
				t.Fatal(err)
			}
			err = replayer.Replay(ctx)
			equal := bytes.Equal(source.Bytes()[eventlog.HeaderSize:], captured.Bytes()[eventlog.HeaderSize:])
			if fault == "none" {
				if err != nil || !equal {
					t.Fatalf("unmodified replay: err=%v, equal bodies=%v", err, equal)
				}
			} else {
				if equal {
					t.Fatal("fault injection did not change the captured log")
				}
				if !errors.Is(err, eventlog.ErrReplayDivergence) {
					t.Fatalf("%s replay: err=%v, want ErrReplayDivergence (source body=%d bytes, captured body=%d bytes)",
						fault, err, source.Len()-eventlog.HeaderSize, captured.Len()-eventlog.HeaderSize)
				}
			}
		})
	}
}

func TestSpec_M2_Replay_TerminalModes(t *testing.T) {
	ctx := context.Background()
	const fed = core.FederationName("terminal")
	for _, tc := range []struct {
		name    string
		strict  bool
		trailer string
		want    error
	}{
		{"strict-clean", true, "", nil},
		{"strict-trailing-record", true, "record", eventlog.ErrReplayDivergence},
		{"strict-truncated-trailer", true, "truncated", io.ErrUnexpectedEOF},
		{"dispatch-clean", false, "", nil},
		{"dispatch-trailing-record", false, "record", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var source, captured bytes.Buffer
			writer := newReplayWriter(t, fed, &source)
			halt := &replayProtoRecord{Event: &rtiv1.Event{Body: &rtiv1.Event_Halted{Halted: &rtiv1.EventFederationHalted{}}}}
			if err := writer.Append(ctx, fed, halt); err != nil {
				t.Fatal(err)
			}
			terminalEnd := source.Len()
			if tc.trailer == "record" {
				if err := writer.Append(ctx, fed, &replayProtoRecord{Event: &rtiv1.Event{Body: &rtiv1.Event_FedJoined{
					FedJoined: &rtiv1.FederateJoined{FederateHandle: 1, FederateName: "must-not-join"},
				}}}); err != nil {
					t.Fatal(err)
				}
			} else if tc.trailer == "truncated" {
				_, _ = source.Write([]byte{1, 0})
			}
			reader, err := eventlog.NewReader(bytes.NewReader(source.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			captureWriter := newReplayWriter(t, fed, &captured)
			mgr := newReplayManager(t, fed, captureWriter)
			opts := eventlog.ReplayerOptions{Source: reader, Federation: mgr, CapturingSink: captureWriter}
			if tc.strict {
				opts.CapturedBuffer = &captured
			}
			replayer, err := eventlog.NewReplayer(opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := replayer.Replay(ctx); !errors.Is(err, tc.want) {
				t.Fatalf("Replay = %v, want %v", err, tc.want)
			}
			if !bytes.Equal(captured.Bytes(), source.Bytes()[:terminalEnd]) {
				t.Fatal("replay did not emit exactly the terminal frame")
			}
			if got := mgr.ListMembers(fed); len(got) != 0 {
				t.Fatalf("event after halt was dispatched: %+v", got)
			}
			if !tc.strict && tc.trailer == "record" {
				remaining, err := reader.Next(ctx)
				if err != nil || remaining.Seq() != 2 {
					t.Fatalf("dispatch-only mode consumed the trailing record: %v, %v", remaining, err)
				}
			}
		})
	}
}

type replayProtoRecord struct{ *rtiv1.Event }

func (r *replayProtoRecord) Seq() uint64 { return r.Event.GetSeq() }

// Inject faults only at the journal boundary; replay still drives the real
// federation manager, and the independent byte comparison verifies injection.
type replayFaultLog struct {
	core.EventLog
	fault string
}

func (l *replayFaultLog) Append(ctx context.Context, fed core.FederationName, event core.EventRecord) error {
	if joined, ok := event.(*core.FederationJoinedJournalRecord); ok {
		switch l.fault {
		case "drop":
			return nil
		case "alter":
			changed := *joined
			changed.FederateName = "wrong-name"
			event = &changed
		}
	}
	return l.EventLog.Append(ctx, fed, event)
}
