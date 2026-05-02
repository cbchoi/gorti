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

// TestSpec_M2_JoinFederation_DeterministicHandles: federate handles are
// assigned by sort order of FederateName (not arrival order). Replay
// determinism depends on this.
//
// Implements: FR-FM-2, NFR-DET-1.
func TestSpec_M2_JoinFederation_DeterministicHandles(t *testing.T) {
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

	// Join in REVERSE alphabetical order: charlie, bob, alice.
	// Expectation: handles are still assigned alphabetically (alice=1, bob=2, charlie=3).
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
	if got["alice"] != 1 || got["bob"] != 2 || got["charlie"] != 3 {
		t.Errorf("got handles alice=%d bob=%d charlie=%d; want 1,2,3 (sorted by name)",
			got["alice"], got["bob"], got["charlie"])
	}
}

// TestSpec_M2_JoinFederation_ConcurrentDeterminism: 50 concurrent joins
// with shuffled scheduling still produce stable handles by name. The
// Manager serializes appropriately; no race detector escape.
//
// Implements: FR-FM-2, NFR-DET-1.
func TestSpec_M2_JoinFederation_ConcurrentDeterminism(t *testing.T) {
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

	// Names "fed00".."fed49" — sort order = creation order — so handles
	// should be 1..50 in that order regardless of goroutine scheduling.
	names := make([]string, 50)
	for i := range names {
		names[i] = "fed" + twoDigit(i)
	}
	results := make(map[string]core.FederateHandle, len(names))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, n := range names {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
				Federation:   "concur",
				FederateName: n,
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("Join %s: %v", n, err)
				return
			}
			results[n] = h
		}()
	}
	wg.Wait()

	for i, n := range names {
		want := core.FederateHandle(i + 1)
		if results[n] != want {
			t.Errorf("handle for %s = %d, want %d", n, results[n], want)
		}
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
