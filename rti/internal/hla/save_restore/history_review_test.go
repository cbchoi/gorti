package savepoint

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type reviewHistoryBundleStore struct {
	*memStore
	onWriter func()
	onClose  func()
}

func (s *reviewHistoryBundleStore) Writer(fed core.FederationName, label string) (io.WriteCloser, error) {
	if s.onWriter != nil {
		s.onWriter()
	}
	w, err := s.memStore.Writer(fed, label)
	if err != nil {
		return nil, err
	}
	return &reviewHistoryWriter{BundleWriter: w.(BundleWriter), onClose: s.onClose}, nil
}

type reviewHistoryWriter struct {
	BundleWriter
	onClose func()
}

func (w *reviewHistoryWriter) Close() error {
	if w.onClose != nil {
		w.onClose()
	}
	return w.BundleWriter.Close()
}

func TestHistoryReviewReservePersistDecisionCommitOrder(t *testing.T) {
	var trace []string
	bundle := &reviewHistoryBundleStore{memStore: newMemStore()}
	history := &historyMemoryStore{}
	out := &historyOutbox{}
	m, err := New(Options{Outbox: out, BundleStore: bundle, HistoryStore: history, Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.InitializeSaveHistory("fed"); err != nil {
		t.Fatal(err)
	}
	out.onReserve = func() {
		trace = append(trace, "reserve")
		if bundle.Exists("fed", testCandidate) {
			t.Error("bundle existed before reservation")
		}
	}
	bundle.onWriter = func() { trace = append(trace, "persist-open") }
	bundle.onClose = func() { trace = append(trace, "persist-close") }
	history.before = func(_ SaveHistory) {
		trace = append(trace, "decision")
		if !bundle.Exists("fed", testCandidate) {
			t.Error("decision preceded bundle publication")
		}
		if got := m.QueryMOMMetrics("fed", 1); got.LastSaveExists {
			t.Error("MOM decision preceded durable decision")
		}
		if m.QuerySaveState("fed", testCandidate) != StateInitiated {
			t.Error("save finalized before durable decision")
		}
	}
	out.onCommit = func() {
		trace = append(trace, "commit")
		stored, err := history.LoadSaveHistory(SaveHistoryKey{Federation: "fed"})
		if err != nil || !stored.HasSave || stored.Name != testCandidate {
			t.Error("callback commit preceded durable history", stored, err)
		}
		if got := m.QueryMOMMetrics("fed", 1); got.LastSaveName != testCandidate {
			t.Error("callback commit preceded memory decision")
		}
		if m.QuerySaveState("fed", testCandidate) != StateSaved {
			t.Error("callback commit preceded finalized state")
		}
	}
	completeSave(t, m, testCandidate)
	if !reflect.DeepEqual(trace, []string{"reserve", "persist-open", "persist-close", "decision", "commit"}) {
		t.Fatalf("boundary order: %v", trace)
	}
	if out.commits != 1 || out.releases != 0 {
		t.Fatalf("reservation lifecycle: commits=%d releases=%d", out.commits, out.releases)
	}
}

func TestHistoryReviewAbortCannotInvalidateInFlightDecision(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	bundle := &reviewHistoryBundleStore{memStore: newMemStore(), onClose: func() { close(entered); <-release }}
	history, out := &historyMemoryStore{}, &historyOutbox{}
	m, err := New(Options{Outbox: out, BundleStore: bundle, HistoryStore: history, Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.RequestFederationSave(ctx, "fed", testCandidate, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateSaveBegun(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	completed, aborted := make(chan error, 1), make(chan error, 1)
	go func() { completed <- m.FederateSaveComplete(ctx, "fed", 1) }()
	<-entered
	go func() { aborted <- m.AbortSave(ctx, "fed") }()
	close(release)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if err := <-aborted; !errors.Is(err, core.ErrSaveNotInProgress) {
		t.Fatalf("abort invalidated a successful decision: %v", err)
	}
	if m.QuerySaveState("fed", testCandidate) != StateSaved {
		t.Fatal("committed state lost")
	}
	if got := m.QueryMOMMetrics("fed", 1); !got.LastSaveExists || got.LastSaveName != testCandidate {
		t.Fatal("committed history lost", got)
	}
}

func TestHistoryReviewCancellationAfterDecisionStillCommitsReservedSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	history, out, bundle := &historyMemoryStore{}, &historyOutbox{}, newMemStore()
	m := newHistoryManager(t, history, out, bundle)
	if err := m.RequestFederationSave(ctx, "fed", testCandidate, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateSaveBegun(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	history.after = func(SaveHistory) { cancel() }
	if err := m.FederateSaveComplete(ctx, "fed", 1); err != nil {
		t.Fatal("cancellation invalidated committed decision", err)
	}
	if out.commits != 1 || out.releases != 0 {
		t.Fatalf("decision lost its reserved callback: commits=%d releases=%d", out.commits, out.releases)
	}
	if m.QuerySaveState("fed", testCandidate) != StateSaved {
		t.Fatal("committed save became failed")
	}
	if got := m.QueryMOMMetrics("fed", 1); !got.LastSaveExists || got.LastSaveName != testCandidate {
		t.Fatal("committed history lost", got)
	}
}

const testCandidate = "candidate"
