package eventlog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/protobuf/proto"
)

const (
	journalFederation core.FederationName = "lifecycle-journal"
	journalWallNS     uint64              = 1750000000123456789
	journalHandle     core.FederateHandle = 1<<40 + 17
)

var journalResignActions = []struct {
	name   string
	action core.ResignAction
	wire   rtiv1.ResignAction
}{
	{"unconditionally-divest", core.ResignActionUnconditionallyDivestAttributes, rtiv1.ResignAction_RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES},
	{"delete-then-divest", core.ResignActionDeleteThenDivest, rtiv1.ResignAction_RESIGN_ACTION_DELETE_THEN_DIVEST},
	{"cancel-then-delete", core.ResignActionCancelThenDelete, rtiv1.ResignAction_RESIGN_ACTION_CANCEL_THEN_DELETE},
	{"cancel-pending-ownership", core.ResignActionCancelPendingOwnership, rtiv1.ResignAction_RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP},
	{"no-action", core.ResignActionNoAction, rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION},
	{"delete-objects", core.ResignActionDeleteObjects, rtiv1.ResignAction_RESIGN_ACTION_DELETE_OBJECTS},
}

func appendJournalRecord(t *testing.T, w *Writer, record core.EventRecord) {
	t.Helper()
	if err := w.Append(context.Background(), journalFederation, record); err != nil {
		t.Fatal(err)
	}
}

func journalReader(t *testing.T, buf *bytes.Buffer) *Reader {
	t.Helper()
	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func readJournalEvent(t *testing.T, r *Reader) *rtiv1.Event {
	t.Helper()
	record, err := r.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wrapped, ok := record.(protoEventRecord)
	if !ok {
		t.Fatalf("read record has type %T, want proto-backed record", record)
	}
	return wrapped.ProtoEvent()
}

func TestFederationJournalWriterReadback(t *testing.T) {
	w, buf := newCapturingSink(t, journalFederation)
	defer w.Close()
	joined := &core.FederationJoinedJournalRecord{
		Sequence: 999, WallNS: journalWallNS, FederateName: testAlphaName, FederateHandle: journalHandle,
	}
	appendJournalRecord(t, w, joined)
	if joined.Sequence != 1 || joined.Seq() != 1 {
		t.Fatalf("joined sequence = %d, want 1", joined.Seq())
	}
	for i, tc := range journalResignActions {
		record := &core.FederationResignedJournalRecord{
			Sequence: 999, WallNS: journalWallNS + uint64(i+1),
			FederateHandle: journalHandle, ResignAction: tc.action,
		}
		appendJournalRecord(t, w, record)
		if record.Sequence != uint64(i+2) || record.Seq() != uint64(i+2) {
			t.Fatalf("%s sequence = %d, want %d", tc.name, record.Seq(), i+2)
		}
	}
	r := journalReader(t, buf)
	want := &rtiv1.Event{Seq: 1, WallNs: journalWallNS, Body: &rtiv1.Event_FedJoined{
		FedJoined: &rtiv1.FederateJoined{FederateHandle: uint64(journalHandle), FederateName: testAlphaName},
	}}
	if got := readJournalEvent(t, r); !proto.Equal(got, want) {
		t.Fatalf("joined readback = %v, want %v", got, want)
	}
	for i, tc := range journalResignActions {
		want := &rtiv1.Event{Seq: uint64(i + 2), WallNs: journalWallNS + uint64(i+1), Body: &rtiv1.Event_FedResigned{
			FedResigned: &rtiv1.FederateResigned{FederateHandle: uint64(journalHandle), Action: tc.wire},
		}}
		if got := readJournalEvent(t, r); !proto.Equal(got, want) {
			t.Errorf("%s readback = %v, want %v", tc.name, got, want)
		}
	}
	if _, err := r.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("trailing record: %v, want EOF", err)
	}
}

func TestFederationJournalRejectsInvalidRecordsWithoutWriting(t *testing.T) {
	cases := []struct {
		name   string
		record core.EventRecord
	}{
		{"nil-joined", (*core.FederationJoinedJournalRecord)(nil)},
		{"nil-resigned", (*core.FederationResignedJournalRecord)(nil)},
		{"unspecified", &core.FederationResignedJournalRecord{}},
		{"unknown", &core.FederationResignedJournalRecord{ResignAction: 7}},
		{"max-uint8", &core.FederationResignedJournalRecord{ResignAction: 255}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, buf := newCapturingSink(t, journalFederation)
			defer w.Close()
			before := bytes.Clone(buf.Bytes())
			err := w.Append(context.Background(), journalFederation, tc.record)
			if err == nil {
				t.Fatal("invalid record accepted")
			}
			if record, ok := tc.record.(*core.FederationResignedJournalRecord); ok && record != nil && !errors.Is(err, core.ErrResignActionUnsupported) {
				t.Errorf("error = %v, want ErrResignActionUnsupported", err)
			}
			if !bytes.Equal(buf.Bytes(), before) {
				t.Fatal("invalid record wrote journal bytes")
			}
			valid := &core.FederationJoinedJournalRecord{FederateHandle: journalHandle, FederateName: testAlphaName}
			appendJournalRecord(t, w, valid)
			if valid.Seq() != 1 {
				t.Fatalf("sequence after rejected append = %d, want 1", valid.Seq())
			}
			if got := readJournalEvent(t, journalReader(t, buf)); got.GetSeq() != 1 || got.GetFedJoined() == nil {
				t.Fatalf("readback after rejected append = %v", got)
			}
		})
	}
}

// Re-emit the same DTOs manager will append, without coupling these adapter
// tests to federation cleanup implementation or its transaction hooks.
type journalingFederation struct {
	stubFederation
	writer *Writer
}

func (f *journalingFederation) JoinFederation(ctx context.Context, req core.JoinFederationRequest) (core.FederateHandle, error) {
	handle, err := f.stubFederation.JoinFederation(ctx, req)
	if err != nil {
		return handle, err
	}
	err = f.writer.Append(ctx, req.Federation, &core.FederationJoinedJournalRecord{
		WallNS: journalWallNS, FederateName: req.FederateName, FederateHandle: handle,
	})
	return handle, err
}

func (f *journalingFederation) ResignFederation(ctx context.Context, fed core.FederationName, handle core.FederateHandle, action core.ResignAction) error {
	if err := f.stubFederation.ResignFederation(ctx, fed, handle, action); err != nil {
		return err
	}
	return f.writer.Append(ctx, fed, &core.FederationResignedJournalRecord{
		WallNS: journalWallNS, FederateHandle: handle, ResignAction: action,
	})
}

func replayJournal(t *testing.T, source *bytes.Buffer, writer *Writer, fed core.FederationStore) error {
	t.Helper()
	replayer, err := NewReplayer(ReplayerOptions{
		Source: journalReader(t, source), Federation: fed, CapturingSink: writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	return replayer.Replay(context.Background())
}

func TestFederationJournalReplayAllResignActions(t *testing.T) {
	for _, tc := range journalResignActions {
		t.Run(tc.name, func(t *testing.T) {
			sourceWriter, source := newCapturingSink(t, journalFederation)
			defer sourceWriter.Close()
			appendJournalRecord(t, sourceWriter, &core.FederationJoinedJournalRecord{
				WallNS: journalWallNS, FederateName: testAlphaName, FederateHandle: journalHandle,
			})
			appendJournalRecord(t, sourceWriter, &core.FederationResignedJournalRecord{
				WallNS: journalWallNS, FederateHandle: journalHandle, ResignAction: tc.action,
			})
			writer, captured := newCapturingSink(t, journalFederation)
			defer writer.Close()
			fed := &journalingFederation{writer: writer, stubFederation: stubFederation{nextHandle: journalHandle - 1}}
			if err := replayJournal(t, source, writer, fed); err != nil {
				t.Fatal(err)
			}
			if len(fed.joinCalls) != 1 || fed.joinCalls[0].Federation != journalFederation || fed.joinCalls[0].FederateName != testAlphaName {
				t.Fatalf("join calls = %+v", fed.joinCalls)
			}
			if len(fed.resignCalls) != 1 {
				t.Fatalf("resign calls = %d, want exactly one whole-action call", len(fed.resignCalls))
			}
			if call := fed.resignCalls[0]; call != (resignCall{journalFederation, journalHandle, tc.action}) {
				t.Fatalf("resign call = %+v, want federation %q, handle %d, action %d", call, journalFederation, journalHandle, tc.action)
			}
			if !bytes.Equal(source.Bytes()[HeaderSize:], captured.Bytes()[HeaderSize:]) {
				t.Fatal("replayed lifecycle journal differs from source body bytes")
			}
		})
	}
}

func legacyJournalResign(action rtiv1.ResignAction) *protoRecord {
	return &protoRecord{Event: &rtiv1.Event{WallNs: journalWallNS, Body: &rtiv1.Event_FedResigned{
		FedResigned: &rtiv1.FederateResigned{FederateHandle: uint64(journalHandle), Action: action},
	}}}
}

func TestFederationJournalReplayLegacyUnspecifiedDefaultsToDivest(t *testing.T) {
	sourceWriter, source := newCapturingSink(t, journalFederation)
	defer sourceWriter.Close()
	appendJournalRecord(t, sourceWriter, legacyJournalResign(rtiv1.ResignAction_RESIGN_ACTION_UNSPECIFIED))
	writer, _ := newCapturingSink(t, journalFederation)
	defer writer.Close()
	fed := &stubFederation{}
	if err := replayJournal(t, source, writer, fed); err != nil {
		t.Fatal(err)
	}
	if len(fed.resignCalls) != 1 || fed.resignCalls[0] != (resignCall{journalFederation, journalHandle, core.ResignActionUnconditionallyDivestAttributes}) {
		t.Fatalf("legacy resign calls = %+v, want unconditional divestiture", fed.resignCalls)
	}
}

func TestFederationJournalReplayRejectsUnknownActions(t *testing.T) {
	for _, action := range []rtiv1.ResignAction{-1, 7, 255, 256, 257, 1<<31 - 1} {
		for _, live := range []bool{false, true} {
			t.Run(fmt.Sprintf("action=%d/live=%t", action, live), func(t *testing.T) {
				sourceWriter, source := newCapturingSink(t, journalFederation)
				defer sourceWriter.Close()
				appendJournalRecord(t, sourceWriter, legacyJournalResign(action))
				appendJournalRecord(t, sourceWriter, &core.FederationJoinedJournalRecord{
					FederateName: "must-not-replay", FederateHandle: journalHandle,
				})
				writer, captured := newCapturingSink(t, journalFederation)
				defer writer.Close()
				before := bytes.Clone(captured.Bytes())
				spy := &stubFederation{}
				var fed core.FederationStore
				if live {
					fed = spy
				}
				err := replayJournal(t, source, writer, fed)
				if !errors.Is(err, core.ErrResignActionUnsupported) || !strings.Contains(err.Error(), fmt.Sprintf("unknown recorded resign action %d", action)) {
					t.Fatalf("replay error = %v, want unknown action %d", err, action)
				}
				if len(spy.joinCalls) != 0 || len(spy.resignCalls) != 0 || !bytes.Equal(before, captured.Bytes()) {
					t.Fatal("corrupt action dispatched or wrote events before rejection")
				}
			})
		}
	}
}

func TestFederationJournalReplayPassthroughPreservesMetadata(t *testing.T) {
	sourceWriter, source := newCapturingSink(t, journalFederation)
	defer sourceWriter.Close()
	joined := &protoRecord{Event: &rtiv1.Event{WallNs: journalWallNS, Body: &rtiv1.Event_FedJoined{
		FedJoined: &rtiv1.FederateJoined{FederateHandle: uint64(journalHandle), FederateName: testAlphaName},
	}}}
	// Preserve unknown envelope fields as well as the existing timestamp.
	joined.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x09})
	appendJournalRecord(t, sourceWriter, joined)
	for _, tc := range journalResignActions {
		record := legacyJournalResign(tc.wire)
		record.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x09})
		appendJournalRecord(t, sourceWriter, record)
	}
	appendJournalRecord(t, sourceWriter, legacyJournalResign(rtiv1.ResignAction_RESIGN_ACTION_UNSPECIFIED))
	writer, captured := newCapturingSink(t, journalFederation)
	defer writer.Close()
	if err := replayJournal(t, source, writer, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source.Bytes()[HeaderSize:], captured.Bytes()[HeaderSize:]) {
		t.Fatal("lifecycle passthrough changed source body bytes")
	}
}

const testAlphaName = "alpha"
