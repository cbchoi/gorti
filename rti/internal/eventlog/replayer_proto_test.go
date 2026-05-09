package eventlog

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// stubFederation is a minimal core.FederationStore the replayer
// dispatches FederateJoined / FederateResigned through. It records
// every call and returns a configurable next-handle so tests can
// exercise both the equal-handle (success) and divergence paths.
type stubFederation struct {
	mu          sync.Mutex
	joinCalls   []core.JoinFederationRequest
	resignCalls []resignCall
	// nextHandle is what JoinFederation returns. Tests bump or set
	// directly to cause a handle mismatch.
	nextHandle core.FederateHandle
}

type resignCall struct {
	Federation core.FederationName
	Handle     core.FederateHandle
	Action     core.ResignAction
}

func (s *stubFederation) CreateFederation(_ context.Context, _ core.CreateFederationRequest) error {
	return errors.New("stub: CreateFederation not used")
}
func (s *stubFederation) DestroyFederation(_ context.Context, _ core.FederationName) error {
	return errors.New("stub: DestroyFederation not used")
}
func (s *stubFederation) JoinFederation(_ context.Context, req core.JoinFederationRequest) (core.FederateHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.joinCalls = append(s.joinCalls, req)
	s.nextHandle++
	return s.nextHandle, nil
}
func (s *stubFederation) ResignFederation(_ context.Context, fed core.FederationName, h core.FederateHandle, action core.ResignAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resignCalls = append(s.resignCalls, resignCall{fed, h, action})
	return nil
}
func (s *stubFederation) List(_ context.Context) ([]core.FederationSummary, error) {
	return nil, nil
}
func (s *stubFederation) Snapshot() []core.FederationRoster { return nil }

// stubObjects is a minimal core.ObjectRegistry. Records every call;
// returns a configurable next ObjectHandle.
type stubObjects struct {
	mu               sync.Mutex
	registerCalls    []registerCall
	updateCalls      []updateCall
	interactCalls    []interactCall
	nextObjectHandle core.ObjectHandle
}

type registerCall struct {
	Federation core.FederationName
	Producer   core.FederateHandle
	Class      core.ObjectClassHandle
	Name       string
}
type updateCall struct {
	Federation core.FederationName
	Producer   core.FederateHandle
	Object     core.ObjectHandle
	Attrs      map[core.AttributeHandle][]byte
	TS         *core.LogicalTime
}
type interactCall struct {
	Federation core.FederationName
	Producer   core.FederateHandle
	Class      core.InteractionClassHandle
	Params     map[core.ParameterHandle][]byte
	TS         *core.LogicalTime
}

func (s *stubObjects) Register(_ context.Context, fed core.FederationName, p core.FederateHandle, c core.ObjectClassHandle, n string) (core.ObjectHandle, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerCalls = append(s.registerCalls, registerCall{fed, p, c, n})
	s.nextObjectHandle++
	if n == "" {
		n = "obj"
	}
	return s.nextObjectHandle, n, nil
}
func (s *stubObjects) UpdateAttributes(_ context.Context, fed core.FederationName, p core.FederateHandle, o core.ObjectHandle, a map[core.AttributeHandle][]byte, ts *core.LogicalTime) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateCalls = append(s.updateCalls, updateCall{fed, p, o, a, ts})
	return nil
}
func (s *stubObjects) SendInteraction(_ context.Context, fed core.FederationName, p core.FederateHandle, c core.InteractionClassHandle, params map[core.ParameterHandle][]byte, ts *core.LogicalTime) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interactCalls = append(s.interactCalls, interactCall{fed, p, c, params, ts})
	return nil
}
func (s *stubObjects) Delete(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ *core.LogicalTime, _ []byte) error {
	return nil
}
func (s *stubObjects) Snapshot(_ core.FederationName) core.ObjectSnapshot {
	return core.ObjectSnapshot{}
}

// buildProtoSourceLog appends one or more *rtiv1.Event records
// (production-shape) into a fresh writer and returns the bytes. Used to
// exercise the dispatch path.
func buildProtoSourceLog(t *testing.T, fed core.FederationName, events ...*rtiv1.Event) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := NewWriter(WriterOptions{
		Sink:       &buf,
		Federation: fed,
		Mode:       core.ModeVerbose,
		Seed:       1,
		Clock:      core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for i, e := range events {
		rec := &protoRecord{Event: e}
		if err := w.Append(context.Background(), fed, rec); err != nil {
			t.Fatalf("Append[%d]: %v", i, err)
		}
	}
	_ = w.Sync(context.Background(), fed)
	return buf.Bytes()
}

// TestReplayer_Dispatch_FederateJoined: a source with a FederateJoined
// event dispatches through Federation.JoinFederation; the recorded
// federate name equals the source's recorded name; the live-assigned
// handle equals the source's recorded handle (no divergence).
func TestReplayer_Dispatch_FederateJoined(t *testing.T) {
	src := buildProtoSourceLog(t, "fj",
		&rtiv1.Event{Body: &rtiv1.Event_FedJoined{
			FedJoined: &rtiv1.FederateJoined{
				FederateHandle: 1,
				FederateName:   "alpha",
			},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "fj")
	defer w.Close()

	fed := &stubFederation{}
	rep, err := NewReplayer(ReplayerOptions{
		Source:        r,
		Federation:    fed,
		CapturingSink: w,
	})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(fed.joinCalls) != 1 {
		t.Fatalf("Federation.JoinFederation called %d times, want 1", len(fed.joinCalls))
	}
	if got := fed.joinCalls[0].FederateName; got != "alpha" {
		t.Errorf("Join.FederateName = %q, want alpha", got)
	}
	if got := fed.joinCalls[0].Federation; got != "fj" {
		t.Errorf("Join.Federation = %q, want fj", got)
	}
}

// TestReplayer_Dispatch_FederateJoined_HandleDivergence: when the
// stubFederation's pre-set nextHandle would NOT match the source's
// recorded handle, Replay returns ErrReplayDivergence with a context
// message naming the divergence.
func TestReplayer_Dispatch_FederateJoined_HandleDivergence(t *testing.T) {
	src := buildProtoSourceLog(t, "div",
		&rtiv1.Event{Body: &rtiv1.Event_FedJoined{
			FedJoined: &rtiv1.FederateJoined{
				FederateHandle: 99, // source recorded 99
				FederateName:   "diverger",
			},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "div")
	defer w.Close()

	// Live federation will assign handle 1 (counter starts at 0).
	fed := &stubFederation{}
	rep, err := NewReplayer(ReplayerOptions{
		Source:        r,
		Federation:    fed,
		CapturingSink: w,
	})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	err = rep.Replay(context.Background())
	if !errors.Is(err, ErrReplayDivergence) {
		t.Errorf("Replay on handle mismatch: err = %v, want ErrReplayDivergence", err)
	}
}

// TestReplayer_Dispatch_FederateResigned: a Resigned event dispatches
// through Federation.ResignFederation with the recorded handle and
// the cut-1 action.
func TestReplayer_Dispatch_FederateResigned(t *testing.T) {
	src := buildProtoSourceLog(t, "fr",
		&rtiv1.Event{Body: &rtiv1.Event_FedResigned{
			FedResigned: &rtiv1.FederateResigned{
				FederateHandle: 7,
			},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "fr")
	defer w.Close()

	fed := &stubFederation{}
	rep, err := NewReplayer(ReplayerOptions{
		Source:        r,
		Federation:    fed,
		CapturingSink: w,
	})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(fed.resignCalls) != 1 {
		t.Fatalf("ResignFederation called %d times, want 1", len(fed.resignCalls))
	}
	if got := fed.resignCalls[0].Handle; got != 7 {
		t.Errorf("Resign.Handle = %d, want 7", got)
	}
	if got := fed.resignCalls[0].Action; got != core.ResignActionUnconditionallyDivestAttributes {
		t.Errorf("Resign.Action = %d, want UnconditionallyDivestAttributes", got)
	}
}

// TestReplayer_Dispatch_ObjectRegistered: an ObjectRegistered event
// dispatches through Objects.Register; the recorded class/owner/name
// are forwarded; the live-assigned handle equals the source's.
func TestReplayer_Dispatch_ObjectRegistered(t *testing.T) {
	src := buildProtoSourceLog(t, "or",
		&rtiv1.Event{Body: &rtiv1.Event_ObjRegistered{
			ObjRegistered: &rtiv1.ObjectRegistered{
				ObjectHandle:        1,
				ObjectClassHandle:   42,
				OwnerFederateHandle: 5,
				ObjectName:          "tank-1",
			},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "or")
	defer w.Close()

	objs := &stubObjects{}
	rep, err := NewReplayer(ReplayerOptions{
		Source:        r,
		Objects:       objs,
		CapturingSink: w,
	})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(objs.registerCalls) != 1 {
		t.Fatalf("Register called %d times, want 1", len(objs.registerCalls))
	}
	c := objs.registerCalls[0]
	if c.Class != 42 || c.Producer != 5 || c.Name != "tank-1" {
		t.Errorf("Register call = %+v, want class=42 producer=5 name=tank-1", c)
	}
}

// TestReplayer_Dispatch_AttributeUpdated: an AttributeUpdated event
// dispatches through Objects.UpdateAttributes; the recorded attrs map
// is preserved; ts is converted from *float64 to *core.LogicalTime
// (TSO when set).
func TestReplayer_Dispatch_AttributeUpdated(t *testing.T) {
	ts := 12.5
	src := buildProtoSourceLog(t, "au",
		&rtiv1.Event{Body: &rtiv1.Event_AttrUpdated{
			AttrUpdated: &rtiv1.AttributeUpdated{
				ObjectHandle:           3,
				ProducerFederateHandle: 1,
				Attributes: map[uint64][]byte{
					10: []byte("ten"),
					20: []byte("twenty"),
				},
				LogicalTime: &ts,
			},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "au")
	defer w.Close()

	objs := &stubObjects{}
	rep, err := NewReplayer(ReplayerOptions{
		Source:        r,
		Objects:       objs,
		CapturingSink: w,
	})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(objs.updateCalls) != 1 {
		t.Fatalf("UpdateAttributes called %d times, want 1", len(objs.updateCalls))
	}
	c := objs.updateCalls[0]
	if c.Object != 3 || c.Producer != 1 {
		t.Errorf("Update call object/producer = %d/%d, want 3/1", c.Object, c.Producer)
	}
	if len(c.Attrs) != 2 {
		t.Errorf("Update.Attrs len = %d, want 2", len(c.Attrs))
	}
	if c.TS == nil || float64(*c.TS) != 12.5 {
		t.Errorf("Update.TS = %v, want 12.5", c.TS)
	}
}

// TestReplayer_Dispatch_InteractionSent: an InteractionSent event
// dispatches through Objects.SendInteraction; ts nil means RO.
func TestReplayer_Dispatch_InteractionSent(t *testing.T) {
	src := buildProtoSourceLog(t, "is",
		&rtiv1.Event{Body: &rtiv1.Event_InterSent{
			InterSent: &rtiv1.InteractionSent{
				InteractionClassHandle: 9,
				ProducerFederateHandle: 2,
				Parameters: map[uint64][]byte{
					100: []byte("p100"),
				},
				// LogicalTime nil → RO
			},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "is")
	defer w.Close()

	objs := &stubObjects{}
	rep, err := NewReplayer(ReplayerOptions{
		Source:        r,
		Objects:       objs,
		CapturingSink: w,
	})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(objs.interactCalls) != 1 {
		t.Fatalf("SendInteraction called %d times, want 1", len(objs.interactCalls))
	}
	c := objs.interactCalls[0]
	if c.Class != 9 || c.Producer != 2 {
		t.Errorf("Interaction call class/producer = %d/%d, want 9/2", c.Class, c.Producer)
	}
	if c.TS != nil {
		t.Errorf("Interaction.TS = %v, want nil (RO)", c.TS)
	}
}

// TestReplayer_Dispatch_ObjectDeletedPassthrough: ObjectDeleted is
// cut-2 deferred — the replayer passes it through unchanged. Verifies
// no Objects calls are made and the captured sink received the event.
func TestReplayer_Dispatch_ObjectDeletedPassthrough(t *testing.T) {
	src := buildProtoSourceLog(t, "od",
		&rtiv1.Event{Body: &rtiv1.Event_ObjDeleted{
			ObjDeleted: &rtiv1.ObjectDeleted{
				ObjectHandle: 11,
			},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, capBuf := newCapturingSink(t, "od")
	defer w.Close()

	objs := &stubObjects{}
	rep, err := NewReplayer(ReplayerOptions{
		Source:        r,
		Objects:       objs,
		CapturingSink: w,
	})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(objs.registerCalls)+len(objs.updateCalls)+len(objs.interactCalls) != 0 {
		t.Errorf("ObjectDeleted produced unexpected Objects calls")
	}
	if !bytes.Equal(src[HeaderSize:], capBuf.Bytes()[HeaderSize:]) {
		t.Errorf("ObjectDeleted passthrough: bodies differ")
	}
}

// TestReplayer_Dispatch_FederationHaltedTerminates: a Halted record
// cleanly terminates the loop. Subsequent records in the source are
// ignored (replay returns nil).
func TestReplayer_Dispatch_FederationHaltedTerminates(t *testing.T) {
	src := buildProtoSourceLog(t, "ht",
		&rtiv1.Event{Body: &rtiv1.Event_Halted{
			Halted: &rtiv1.EventFederationHalted{},
		}},
		// This second event MUST NOT be dispatched.
		&rtiv1.Event{Body: &rtiv1.Event_FedJoined{
			FedJoined: &rtiv1.FederateJoined{FederateHandle: 1, FederateName: "ghost"},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "ht")
	defer w.Close()

	fed := &stubFederation{}
	rep, err := NewReplayer(ReplayerOptions{
		Source:        r,
		Federation:    fed,
		CapturingSink: w,
	})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(fed.joinCalls) != 0 {
		t.Errorf("Halt did not terminate replay: JoinFederation called %d times, want 0",
			len(fed.joinCalls))
	}
}

// TestReplayer_Dispatch_NilFederationFallsBackToPassthrough: when a
// proto event arrives but Federation is nil, replayer falls back to
// passthrough so the captured stream still matches the source bytes.
func TestReplayer_Dispatch_NilFederationFallsBackToPassthrough(t *testing.T) {
	src := buildProtoSourceLog(t, "nf",
		&rtiv1.Event{Body: &rtiv1.Event_FedJoined{
			FedJoined: &rtiv1.FederateJoined{FederateHandle: 1, FederateName: "loner"},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, capBuf := newCapturingSink(t, "nf")
	defer w.Close()

	rep, err := NewReplayer(ReplayerOptions{
		Source:        r,
		CapturingSink: w,
		// Federation/Objects deliberately nil.
	})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if !bytes.Equal(src[HeaderSize:], capBuf.Bytes()[HeaderSize:]) {
		t.Errorf("nil-Federation passthrough: bodies differ")
	}
}

// TestReplayer_Dispatch_TimeAdvanceRequestedPassthrough: M3 events
// pass through with no calls.
func TestReplayer_Dispatch_TimeAdvanceRequestedPassthrough(t *testing.T) {
	src := buildProtoSourceLog(t, "ta",
		&rtiv1.Event{Body: &rtiv1.Event_TimeRequested{
			TimeRequested: &rtiv1.TimeAdvanceRequested{},
		}},
		&rtiv1.Event{Body: &rtiv1.Event_TimeGranted{
			TimeGranted: &rtiv1.TimeAdvanceGranted{},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, capBuf := newCapturingSink(t, "ta")
	defer w.Close()

	rep, err := NewReplayer(ReplayerOptions{Source: r, CapturingSink: w})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if !bytes.Equal(src[HeaderSize:], capBuf.Bytes()[HeaderSize:]) {
		t.Errorf("TimeAdvance passthrough: bodies differ")
	}
}

// TestReplayer_Replay_CanceledContext: a canceled context causes
// Replay to return wrapping ctx.Err().
func TestReplayer_Replay_CanceledContext(t *testing.T) {
	src := buildSyntheticSourceLog(t, "cc", 3)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "cc")
	defer w.Close()

	rep, err := NewReplayer(ReplayerOptions{Source: r, CapturingSink: w})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = rep.Replay(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Replay on canceled ctx: err = %v, want context.Canceled", err)
	}
}

// readerErr is a one-shot reader that errors on the first Read after
// the header has been consumed by the Reader. Useful for exercising
// the "source read error" branch in Replayer.Replay.
type readerErr struct {
	header []byte
	off    int
}

func (r *readerErr) Read(p []byte) (int, error) {
	if r.off < len(r.header) {
		n := copy(p, r.header[r.off:])
		r.off += n
		return n, nil
	}
	return 0, errors.New("read failed")
}

// TestProtoRecord_SeqReflectsEmbeddedEvent: the protoRecord wrapper
// (used during passthrough) exposes Seq() that reads from the embedded
// *rtiv1.Event. This ensures our wrapper satisfies core.EventRecord
// when callers introspect via the interface.
func TestProtoRecord_SeqReflectsEmbeddedEvent(t *testing.T) {
	pb := &rtiv1.Event{Seq: 42}
	pr := &protoRecord{Event: pb}
	if got := pr.Seq(); got != 42 {
		t.Errorf("protoRecord.Seq() = %d, want 42", got)
	}
}

// TestReplayer_Dispatch_FederationReturnsError: when Federation returns
// an error during JoinFederation, replay surfaces that error wrapped
// (not as divergence).
func TestReplayer_Dispatch_FederationReturnsError(t *testing.T) {
	src := buildProtoSourceLog(t, "ferr",
		&rtiv1.Event{Body: &rtiv1.Event_FedJoined{
			FedJoined: &rtiv1.FederateJoined{FederateHandle: 1, FederateName: "x"},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "ferr")
	defer w.Close()

	fed := &errorFederation{err: errors.New("backend down")}
	rep, err := NewReplayer(ReplayerOptions{
		Source: r, Federation: fed, CapturingSink: w,
	})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	err = rep.Replay(context.Background())
	if err == nil || errors.Is(err, ErrReplayDivergence) {
		t.Errorf("Replay on Federation error: err = %v, want non-nil non-divergence", err)
	}
}

// TestReplayer_Dispatch_ResignReturnsError: ResignFederation error path.
func TestReplayer_Dispatch_ResignReturnsError(t *testing.T) {
	src := buildProtoSourceLog(t, "rerr",
		&rtiv1.Event{Body: &rtiv1.Event_FedResigned{
			FedResigned: &rtiv1.FederateResigned{FederateHandle: 1},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "rerr")
	defer w.Close()

	fed := &errorFederation{err: errors.New("not joined")}
	rep, err := NewReplayer(ReplayerOptions{Source: r, Federation: fed, CapturingSink: w})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err == nil {
		t.Errorf("Replay on Resign error: nil error")
	}
}

// TestReplayer_Dispatch_RegisterReturnsError: Objects.Register error path.
func TestReplayer_Dispatch_RegisterReturnsError(t *testing.T) {
	src := buildProtoSourceLog(t, "regerr",
		&rtiv1.Event{Body: &rtiv1.Event_ObjRegistered{
			ObjRegistered: &rtiv1.ObjectRegistered{ObjectHandle: 1, ObjectClassHandle: 1, OwnerFederateHandle: 1},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "regerr")
	defer w.Close()

	objs := &errorObjects{err: errors.New("not published")}
	rep, err := NewReplayer(ReplayerOptions{Source: r, Objects: objs, CapturingSink: w})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err == nil {
		t.Errorf("Replay on Register error: nil error")
	}
}

// TestReplayer_Dispatch_RegisterHandleDivergence: stubObjects assigns
// handle 1, but source recorded handle 99 — divergence.
func TestReplayer_Dispatch_RegisterHandleDivergence(t *testing.T) {
	src := buildProtoSourceLog(t, "divreg",
		&rtiv1.Event{Body: &rtiv1.Event_ObjRegistered{
			ObjRegistered: &rtiv1.ObjectRegistered{ObjectHandle: 99, ObjectClassHandle: 1, OwnerFederateHandle: 1, ObjectName: "x"},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "divreg")
	defer w.Close()

	objs := &stubObjects{}
	rep, err := NewReplayer(ReplayerOptions{Source: r, Objects: objs, CapturingSink: w})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); !errors.Is(err, ErrReplayDivergence) {
		t.Errorf("Register handle divergence: err = %v, want ErrReplayDivergence", err)
	}
}

// TestReplayer_Dispatch_UpdateAttributesError: UpdateAttributes error.
func TestReplayer_Dispatch_UpdateAttributesError(t *testing.T) {
	src := buildProtoSourceLog(t, "uaerr",
		&rtiv1.Event{Body: &rtiv1.Event_AttrUpdated{
			AttrUpdated: &rtiv1.AttributeUpdated{ObjectHandle: 1, ProducerFederateHandle: 1},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "uaerr")
	defer w.Close()

	objs := &errorObjects{err: errors.New("not owned")}
	rep, err := NewReplayer(ReplayerOptions{Source: r, Objects: objs, CapturingSink: w})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err == nil {
		t.Errorf("Replay on Update error: nil error")
	}
}

// TestReplayer_Dispatch_SendInteractionError: SendInteraction error.
func TestReplayer_Dispatch_SendInteractionError(t *testing.T) {
	src := buildProtoSourceLog(t, "sierr",
		&rtiv1.Event{Body: &rtiv1.Event_InterSent{
			InterSent: &rtiv1.InteractionSent{InteractionClassHandle: 1, ProducerFederateHandle: 1},
		}},
	)
	r, err := NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "sierr")
	defer w.Close()

	objs := &errorObjects{err: errors.New("class not declared")}
	rep, err := NewReplayer(ReplayerOptions{Source: r, Objects: objs, CapturingSink: w})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	if err := rep.Replay(context.Background()); err == nil {
		t.Errorf("Replay on SendInteraction error: nil error")
	}
}

// errorFederation always returns err on Join/Resign.
type errorFederation struct {
	err error
}

func (e *errorFederation) CreateFederation(_ context.Context, _ core.CreateFederationRequest) error {
	return e.err
}
func (e *errorFederation) DestroyFederation(_ context.Context, _ core.FederationName) error {
	return e.err
}
func (e *errorFederation) JoinFederation(_ context.Context, _ core.JoinFederationRequest) (core.FederateHandle, error) {
	return core.InvalidFederateHandle, e.err
}
func (e *errorFederation) ResignFederation(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ResignAction) error {
	return e.err
}
func (e *errorFederation) List(_ context.Context) ([]core.FederationSummary, error) {
	return nil, e.err
}
func (e *errorFederation) Snapshot() []core.FederationRoster { return nil }

// errorObjects always returns err.
type errorObjects struct {
	err error
}

func (e *errorObjects) Register(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ string) (core.ObjectHandle, string, error) {
	return core.InvalidObjectHandle, "", e.err
}
func (e *errorObjects) UpdateAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ map[core.AttributeHandle][]byte, _ *core.LogicalTime) error {
	return e.err
}
func (e *errorObjects) SendInteraction(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle, _ map[core.ParameterHandle][]byte, _ *core.LogicalTime) error {
	return e.err
}
func (e *errorObjects) Delete(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, _ *core.LogicalTime, _ []byte) error {
	return e.err
}
func (e *errorObjects) Snapshot(_ core.FederationName) core.ObjectSnapshot {
	return core.ObjectSnapshot{}
}

// TestReplayer_Replay_PropagatesSourceReadError: an I/O failure on
// reading the source body propagates as an error (not divergence).
func TestReplayer_Replay_PropagatesSourceReadError(t *testing.T) {
	hdr := make([]byte, HeaderSize)
	if err := EncodeHeader(hdr, core.EventLogHeader{
		Magic:      Magic,
		Version:    Version,
		Federation: "rerr",
		Mode:       core.ModeVerbose,
	}); err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	r, err := NewReader(&readerErr{header: hdr})
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	w, _ := newCapturingSink(t, "rerr")
	defer w.Close()

	rep, err := NewReplayer(ReplayerOptions{Source: r, CapturingSink: w})
	if err != nil {
		t.Fatalf("NewReplayer: %v", err)
	}
	err = rep.Replay(context.Background())
	if err == nil || errors.Is(err, ErrReplayDivergence) {
		t.Errorf("Replay on read failure: err = %v, want non-nil non-divergence", err)
	}
}
