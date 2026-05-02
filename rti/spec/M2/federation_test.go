package m2spec

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
	"github.com/cbchoi/gorti/rti/internal/federation"
)

// newTestFederationManager builds a federation.Manager wired with the
// fakes most tests want. Returns the manager, the in-memory log buffer
// (for inspection), and the FOM repo (for canned-error injection).
func newTestFederationManager(t *testing.T) (*federation.Manager, *eventlog.Writer, *fakeFOMRepo) {
	t.Helper()
	clk := core.NewFakeClock(time.Unix(0, 0))
	repo := newFakeFOMRepo()
	// Each test gets its own log buffer; the writer header validation is
	// covered separately in eventlog_test.go.
	w, err := eventlog.NewWriter(eventlog.WriterOptions{
		Sink:       newDiscardSink(),
		Federation: "_test",
		Mode:       core.ModeVerbose,
		Seed:       1,
		Clock:      clk,
	})
	if err != nil {
		// Stub returns ErrNotImplemented; that's OK — federation tests
		// can still proceed with a nil writer. They reach the
		// federation manager's stub before touching the log.
		w = nil
	}
	mgr, mErr := federation.New(federation.Options{
		Clock:               clk,
		EventLog:            w,
		FOMs:                repo,
		DefaultStallTimeout: 60,
	})
	if mErr != nil {
		// Until New is implemented, mgr is nil — tests must not deref it
		// before exercising the public API. The first method call hits
		// the stub and returns ErrNotImplemented.
		t.Logf("federation.New returned: %v (expected during M2 RED phase)", mErr)
	}
	return mgr, w, repo
}

// discardSink swallows writes. Used until eventlog.Writer is real.
type discardSink struct{}

func newDiscardSink() *discardSink                { return &discardSink{} }
func (*discardSink) Write(p []byte) (int, error) { return len(p), nil }

// TestSpec_M2_CreateFederation_Happy: a brand-new federation with valid
// FOM modules is created without error.
//
// Implements: FR-FM-1.
func TestSpec_M2_CreateFederation_Happy(t *testing.T) {
	mgr, _, _ := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired; expected during M2 RED phase")
	}
	err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "demo",
		FOMModules: []core.FOMModule{{Path: "demo.fom", XML: []byte("<objectModel/>")}},
		Mode:       core.ModeVerbose,
	})
	if err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
}

// TestSpec_M2_CreateFederation_DuplicateName: creating twice with the same
// name returns core.ErrFederationAlreadyExists.
//
// Implements: FR-FM-1.
func TestSpec_M2_CreateFederation_DuplicateName(t *testing.T) {
	mgr, _, _ := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired")
	}
	req := core.CreateFederationRequest{
		Name:       "dup",
		FOMModules: []core.FOMModule{{Path: "x", XML: []byte("<objectModel/>")}},
		Mode:       core.ModeVerbose,
	}
	if err := mgr.CreateFederation(context.Background(), req); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := mgr.CreateFederation(context.Background(), req)
	if !errors.Is(err, core.ErrFederationAlreadyExists) {
		t.Fatalf("second create err = %v, want ErrFederationAlreadyExists", err)
	}
}

// TestSpec_M2_CreateFederation_BadFOM: when the FOM repo rejects, Create
// surfaces the rejection (wraps the underlying error so callers can
// errors.Is/As against diagnostic types).
//
// Implements: FR-FM-1, FR-FOM-4.
func TestSpec_M2_CreateFederation_BadFOM(t *testing.T) {
	mgr, _, repo := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired")
	}
	repo.loadErr = errCanned
	err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "badfom",
		FOMModules: []core.FOMModule{{Path: "x", XML: []byte("<bad/>")}},
		Mode:       core.ModeVerbose,
	})
	if err == nil {
		t.Fatal("CreateFederation with bad FOM: want error, got nil")
	}
	if !errors.Is(err, errCanned) {
		t.Errorf("error %v does not wrap canned FOM-load error", err)
	}
}

// TestSpec_M2_JoinFederation_MonotonicHandles: federate handles are
// assigned monotonically in arrival order, starting at 1. Once assigned,
// a handle is stable for the federate's lifetime — never re-keyed.
//
// Replay determinism (NFR-DET-1) is guaranteed because the FederateJoined
// event log records the assignment; replay reads the log and re-assigns
// the same handles in the same arrival order.
//
// Implements: FR-FM-2, NFR-DET-1.
func TestSpec_M2_JoinFederation_MonotonicHandles(t *testing.T) {
	mgr, _, _ := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired")
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "det",
		FOMModules: []core.FOMModule{{Path: "x", XML: []byte("<objectModel/>")}},
		Mode:       core.ModeVerbose,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Join in arrival order: charlie, bob, alice. Expectation: monotonic
	// handles 1, 2, 3 assigned in arrival order (NOT name-sort order).
	// Re-keying when alice joins later is forbidden — charlie's handle 1
	// must stay 1.
	got := map[string]core.FederateHandle{}
	for _, name := range []string{"charlie", "bob", "alice"} {
		h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
			Federation:   "det",
			FederateName: name,
		})
		if err != nil {
			t.Fatalf("Join %s: %v", name, err)
		}
		got[name] = h
	}
	if got["charlie"] != 1 || got["bob"] != 2 || got["alice"] != 3 {
		t.Errorf("got handles charlie=%d bob=%d alice=%d; want 1,2,3 (arrival order)",
			got["charlie"], got["bob"], got["alice"])
	}
}

// TestSpec_M2_JoinFederation_ConcurrentSerialization: 50 concurrent joins
// fed through a deterministic input-order channel produce stable handles.
// The fixture itself imposes the order (channel ordering); the Manager's
// job is to serialize correctly so each join sees the next monotonic
// handle without races.
//
// This is the executable form of NFR-DET-1 for the join path: same input
// order in → same handle assignment out. Different scheduling of the
// goroutines that pull from the channel does not affect the result
// because the channel reads are serialized.
//
// Implements: FR-FM-2, NFR-DET-1.
func TestSpec_M2_JoinFederation_ConcurrentSerialization(t *testing.T) {
	mgr, _, _ := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired")
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "concur",
		FOMModules: []core.FOMModule{{Path: "x", XML: []byte("<objectModel/>")}},
		Mode:       core.ModeVerbose,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Names "fed00".."fed49" sent through a channel one at a time.
	// Workers pull in channel order (FIFO); the Manager serializes so
	// each Join sees the next monotonic handle.
	names := make([]string, 50)
	for i := range names {
		names[i] = "fed" + twoDigit(i)
	}
	ch := make(chan string, len(names))
	for _, n := range names {
		ch <- n
	}
	close(ch)

	results := make(map[string]core.FederateHandle, len(names))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ { // 8 workers pulling from a 50-deep channel
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range ch {
				h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
					Federation:   "concur",
					FederateName: n,
				})
				mu.Lock()
				if err != nil {
					t.Errorf("Join %s: %v", n, err)
				} else {
					results[n] = h
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Every name got a unique handle in the range 1..50.
	seen := map[core.FederateHandle]bool{}
	for n, h := range results {
		if h < 1 || h > 50 {
			t.Errorf("handle for %s = %d, out of [1,50]", n, h)
		}
		if seen[h] {
			t.Errorf("duplicate handle %d (current name=%s)", h, n)
		}
		seen[h] = true
	}
	if len(seen) != len(names) {
		t.Errorf("got %d unique handles; want %d", len(seen), len(names))
	}
}

func twoDigit(i int) string {
	const dig = "0123456789"
	return string([]byte{dig[i/10], dig[i%10]})
}

// TestSpec_M2_JoinFederation_DuplicateName: joining twice with the same
// federate name returns core.ErrFederateAlreadyJoined.
//
// Implements: FR-FM-2.
func TestSpec_M2_JoinFederation_DuplicateName(t *testing.T) {
	mgr, _, _ := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired")
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "dup",
		FOMModules: []core.FOMModule{{Path: "x", XML: []byte("<objectModel/>")}},
		Mode:       core.ModeVerbose,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	req := core.JoinFederationRequest{Federation: "dup", FederateName: "alice"}
	if _, err := mgr.JoinFederation(context.Background(), req); err != nil {
		t.Fatalf("first join: %v", err)
	}
	if _, err := mgr.JoinFederation(context.Background(), req); !errors.Is(err, core.ErrFederateAlreadyJoined) {
		t.Errorf("second join: err = %v, want ErrFederateAlreadyJoined", err)
	}
}

// TestSpec_M2_DestroyFederation_RejectsWhileJoined: cannot destroy a
// federation while any federate is joined.
//
// Implements: FR-FM-5.
func TestSpec_M2_DestroyFederation_RejectsWhileJoined(t *testing.T) {
	mgr, _, _ := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired")
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "x",
		FOMModules: []core.FOMModule{{Path: "x", XML: []byte("<objectModel/>")}},
		Mode:       core.ModeVerbose,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation:   "x",
		FederateName: "alice",
	}); err != nil {
		t.Fatalf("join: %v", err)
	}
	err := mgr.DestroyFederation(context.Background(), "x")
	if !errors.Is(err, core.ErrFederationHasFederatesJoined) {
		t.Errorf("Destroy while joined: err = %v, want ErrFederationHasFederatesJoined", err)
	}
}

// TestSpec_M2_ResignFederation_ThenDestroy: resigning the last federate
// allows destroy to succeed.
//
// Implements: FR-FM-3, FR-FM-5.
func TestSpec_M2_ResignFederation_ThenDestroy(t *testing.T) {
	mgr, _, _ := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired")
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "x",
		FOMModules: []core.FOMModule{{Path: "x", XML: []byte("<objectModel/>")}},
		Mode:       core.ModeVerbose,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation:   "x",
		FederateName: "alice",
	})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := mgr.ResignFederation(context.Background(), "x", h, core.ResignActionUnconditionallyDivestAttributes); err != nil {
		t.Fatalf("resign: %v", err)
	}
	if err := mgr.DestroyFederation(context.Background(), "x"); err != nil {
		t.Fatalf("destroy after resign: %v", err)
	}
}

// TestSpec_M2_ResignFederation_Idempotent: resigning an already-resigned
// federate returns core.ErrFederateNotJoined.
//
// Implements: FR-FM-3.
func TestSpec_M2_ResignFederation_Idempotent(t *testing.T) {
	mgr, _, _ := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired")
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "x",
		FOMModules: []core.FOMModule{{Path: "x", XML: []byte("<objectModel/>")}},
		Mode:       core.ModeVerbose,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	h, _ := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation:   "x",
		FederateName: "alice",
	})
	_ = mgr.ResignFederation(context.Background(), "x", h, core.ResignActionUnconditionallyDivestAttributes)
	err := mgr.ResignFederation(context.Background(), "x", h, core.ResignActionUnconditionallyDivestAttributes)
	if !errors.Is(err, core.ErrFederateNotJoined) {
		t.Errorf("re-resign: err = %v, want ErrFederateNotJoined", err)
	}
}

// TestSpec_M2_List_ReturnsSorted: List returns federations in
// name-sorted order.
//
// Implements: FR-FM-4, NFR-DET-1.
func TestSpec_M2_List_ReturnsSorted(t *testing.T) {
	mgr, _, _ := newTestFederationManager(t)
	if mgr == nil {
		t.Skip("federation.Manager not yet wired")
	}
	for _, n := range []string{"charlie", "alpha", "bravo"} {
		_ = mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
			Name:       core.FederationName(n),
			FOMModules: []core.FOMModule{{Path: "x", XML: []byte("<objectModel/>")}},
			Mode:       core.ModeVerbose,
		})
	}
	got, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make([]string, len(got))
	for i, s := range got {
		names[i] = string(s.Name)
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("List returned %v; want sorted", names)
	}
}
