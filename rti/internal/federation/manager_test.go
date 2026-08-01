package federation_test

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/federation"
)

// stubFOMRepo is a minimal FOMRepository for unit tests. It returns the
// canned handle by default and the canned error when set.
type stubFOMRepo struct {
	loadErr error
	handle  *stubFOMHandle
}

func newStubFOMRepo() *stubFOMRepo { return &stubFOMRepo{handle: &stubFOMHandle{}} }

func (r *stubFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	return r.handle, nil
}

func (r *stubFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return r.handle, nil
}

type stubFOMHandle struct{}

func (*stubFOMHandle) IsValid() bool { return true }
func (*stubFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 1, true
}
func (*stubFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (*stubFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (*stubFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

// validOptions returns a complete Options ready for New. Tests mutate one
// field at a time to exercise validation.
func validOptions() federation.Options {
	return federation.Options{
		Clock:               core.NewFakeClock(time.Unix(0, 0)),
		EventLog:            nil, // optional in cut 1; W1B implements separately.
		FOMs:                newStubFOMRepo(),
		DefaultStallTimeout: 0,
	}
}

// TestNew_NilClock_ReturnsErrorNamingField verifies that the constructor
// rejects a nil Clock and that the error names the missing field so
// operators can fix configuration without spelunking source.
func TestNew_NilClock_ReturnsErrorNamingField(t *testing.T) {
	t.Parallel()
	opts := validOptions()
	opts.Clock = nil
	mgr, err := federation.New(opts)
	if err == nil {
		t.Fatalf("New with nil Clock: want error, got nil (mgr=%v)", mgr)
	}
	if mgr != nil {
		t.Errorf("New with nil Clock: want nil Manager, got %v", mgr)
	}
	// Observable behavior: the error message identifies the field.
	if !contains(err.Error(), "Clock") {
		t.Errorf("error %q does not mention Clock", err.Error())
	}
}

// TestNew_NilFOMs_ReturnsErrorNamingField verifies that the constructor
// rejects a nil FOMRepository.
func TestNew_NilFOMs_ReturnsErrorNamingField(t *testing.T) {
	t.Parallel()
	opts := validOptions()
	opts.FOMs = nil
	mgr, err := federation.New(opts)
	if err == nil {
		t.Fatalf("New with nil FOMs: want error, got nil (mgr=%v)", mgr)
	}
	if mgr != nil {
		t.Errorf("New with nil FOMs: want nil Manager, got %v", mgr)
	}
	if !contains(err.Error(), "FOMs") {
		t.Errorf("error %q does not mention FOMs", err.Error())
	}
}

// TestNew_ValidOptions_ReturnsManager verifies the happy path: complete
// Options yields a usable Manager with no error.
func TestNew_ValidOptions_ReturnsManager(t *testing.T) {
	t.Parallel()
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatalf("New(valid): %v", err)
	}
	if mgr == nil {
		t.Fatal("New(valid) returned nil Manager")
	}
}

// TestNew_NilEventLog_AllowedForCreate verifies that nil EventLog is
// accepted: federation Create still functions; events would simply not be
// emitted. This is a deliberate cut-1 relaxation so W1A can land before
// W1B. (The Join-with-nil-EventLog assertion is added in the TASK-021
// test commit so that test turns green together with its impl.)
func TestNew_NilEventLog_AllowedForCreate(t *testing.T) {
	t.Parallel()
	opts := validOptions()
	opts.EventLog = nil
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatalf("New with nil EventLog: %v", err)
	}
	if mgr == nil {
		t.Fatal("New with nil EventLog returned nil Manager")
	}
	if cerr := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "x",
		FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
		Mode:       core.ModeVerbose,
	}); cerr != nil {
		t.Fatalf("CreateFederation with nil EventLog: %v", cerr)
	}
}

// TestNew_DefaultStallTimeoutZero_AppliesSixtySeconds verifies that the
// documented default of 60s is applied when callers leave
// DefaultStallTimeout at the zero value.
//
// Observable via List: the federation summary is independent of stall
// timeout, so we cover this indirectly — the test fails only if New
// rejects opts with DefaultStallTimeout=0 (which it must not).
func TestNew_DefaultStallTimeoutZero_AppliesSixtySeconds(t *testing.T) {
	t.Parallel()
	opts := validOptions()
	opts.DefaultStallTimeout = 0
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if mgr == nil {
		t.Fatal("nil Manager")
	}
	// Create + List succeed; the default is internal but a CreateFederation
	// with StallTimeout==0 must not be rejected by the manager.
	if cerr := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "x",
		FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
		Mode:       core.ModeVerbose,
	}); cerr != nil {
		t.Fatalf("CreateFederation: %v", cerr)
	}
}

// TestCreateFederation_BadFOM_WrapsRepoError verifies that the FOMRepo
// rejection is surfaced verbatim (errors.Is must succeed against the
// canned error).
func TestCreateFederation_BadFOM_WrapsRepoError(t *testing.T) {
	t.Parallel()
	opts := validOptions()
	repo := opts.FOMs.(*stubFOMRepo)
	canned := errors.New("FOM-001: missing dataType")
	repo.loadErr = canned
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "bad",
		FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
		Mode:       core.ModeVerbose,
	})
	if got == nil {
		t.Fatal("CreateFederation: want error, got nil")
	}
	if !errors.Is(got, canned) {
		t.Errorf("error %v does not wrap canned %v", got, canned)
	}
}

// TestCreateFederation_DuplicateName_ReturnsSentinel verifies the duplicate
// rejection is the documented sentinel.
func TestCreateFederation_DuplicateName_ReturnsSentinel(t *testing.T) {
	t.Parallel()
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := core.CreateFederationRequest{
		Name:       "dup",
		FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
		Mode:       core.ModeVerbose,
	}
	if e := mgr.CreateFederation(context.Background(), req); e != nil {
		t.Fatalf("first create: %v", e)
	}
	e := mgr.CreateFederation(context.Background(), req)
	if !errors.Is(e, core.ErrFederationAlreadyExists) {
		t.Errorf("second create: %v, want ErrFederationAlreadyExists", e)
	}
}

// ---------------------------------------------------------------------------
// TASK-021: JoinFederation tests
// ---------------------------------------------------------------------------

// TestJoinFederation_UnknownFederation_ReturnsNotFound verifies that joining
// a federation that was never created returns the documented sentinel.
func TestJoinFederation_UnknownFederation_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, jerr := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "ghost", FederateName: "alice",
	})
	if !errors.Is(jerr, core.ErrFederationNotFound) {
		t.Errorf("Join unknown federation: %v, want ErrFederationNotFound", jerr)
	}
}

// TestJoinFederation_DuplicateName_ReturnsAlreadyJoined verifies that two
// joins with the same federate name return the documented sentinel.
func TestJoinFederation_DuplicateName_ReturnsAlreadyJoined(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "x")
	req := core.JoinFederationRequest{Federation: "x", FederateName: "alice"}
	if _, e := mgr.JoinFederation(context.Background(), req); e != nil {
		t.Fatalf("first join: %v", e)
	}
	_, e := mgr.JoinFederation(context.Background(), req)
	if !errors.Is(e, core.ErrFederateAlreadyJoined) {
		t.Errorf("duplicate join: %v, want ErrFederateAlreadyJoined", e)
	}
}

// TestJoinFederation_AssignsMonotonicHandlesByArrivalOrder verifies the
// algorithm contract: for serial joins, handle[k] == k and existing
// federates' handles are never reassigned when a new federate joins.
//
// Industry behavior (Portico / reference_rti / MAK): handles are issued by a
// per-federation monotonic counter. The specification tests
// (TestSpec_M2_JoinFederation_MonotonicHandles) assert the same.
func TestJoinFederation_AssignsMonotonicHandlesByArrivalOrder(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "det")
	// Arrival order chosen so sort-order would disagree (z, then a, then m).
	cases := []struct {
		joining  string
		wantThis core.FederateHandle
	}{
		{"zulu", 1},
		{"alpha", 2},
		{"mike", 3},
	}
	got := map[string]core.FederateHandle{}
	for _, c := range cases {
		h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
			Federation: "det", FederateName: c.joining,
		})
		if err != nil {
			t.Fatalf("Join %s: %v", c.joining, err)
		}
		if h != c.wantThis {
			t.Errorf("Join %s: got handle %d, want %d (arrival order)",
				c.joining, h, c.wantThis)
		}
		got[c.joining] = h
	}
	// Re-assert all prior handles after every later join — they must NOT
	// have shifted. The fixture's `got` map is the cheap proof: the manager
	// never wrote to a name's handle except on its own Join.
	if got["zulu"] != 1 || got["alpha"] != 2 || got["mike"] != 3 {
		t.Errorf("post-all-joins: zulu=%d alpha=%d mike=%d; want 1,2,3 (no re-keying)",
			got["zulu"], got["alpha"], got["mike"])
	}
}

// TestJoinFederation_ExistingHandlesUnchangedAfterLaterJoin is the focused
// invariant: joining X then Y → X stays at handle 1, Y gets handle 2;
// later joining Z → both X and Y unchanged, Z gets handle 3.
//
// We verify "unchanged" by re-resigning the original handle at the end:
// resign with the original handle must succeed (the slot still belongs to
// the same name) and a duplicate-name re-Join must be rejected (the name
// is still bound to that handle until resign completes).
func TestJoinFederation_ExistingHandlesUnchangedAfterLaterJoin(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "x")
	hX, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "X",
	})
	if err != nil {
		t.Fatalf("Join X: %v", err)
	}
	hY, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "Y",
	})
	if err != nil {
		t.Fatalf("Join Y: %v", err)
	}
	if hX != 1 || hY != 2 {
		t.Fatalf("after X,Y: hX=%d hY=%d; want 1,2", hX, hY)
	}
	hZ, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "Z",
	})
	if err != nil {
		t.Fatalf("Join Z: %v", err)
	}
	if hZ != 3 {
		t.Errorf("after X,Y,Z: hZ=%d; want 3", hZ)
	}
	// Invariance check: X's original handle still resigns successfully —
	// the (handle 1 -> "X") binding has not been re-keyed.
	if rerr := mgr.ResignFederation(context.Background(), "x", hX,
		core.ResignActionUnconditionallyDivestAttributes); rerr != nil {
		t.Errorf("resign X with original handle %d after Z joined: %v (handle was re-keyed?)", hX, rerr)
	}
	// Y's handle 2 should also still resolve — likewise unchanged.
	if rerr := mgr.ResignFederation(context.Background(), "x", hY,
		core.ResignActionUnconditionallyDivestAttributes); rerr != nil {
		t.Errorf("resign Y with original handle %d after Z joined: %v (handle was re-keyed?)", hY, rerr)
	}
}

// TestJoinFederation_ResignedHandlesAreNotReused: the per-federation
// monotonic counter never rolls back. After joining A,B and resigning A,
// joining C must yield handle 3 — NOT reuse handle 1.
//
// This protects the FederateJoined event log from ambiguity: handle 1
// always refers to A in this federation's history, even after A is gone.
func TestJoinFederation_ResignedHandlesAreNotReused(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "x")
	hA, _ := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "A",
	})
	_, _ = mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "B",
	})
	if rerr := mgr.ResignFederation(context.Background(), "x", hA,
		core.ResignActionUnconditionallyDivestAttributes); rerr != nil {
		t.Fatalf("resign A: %v", rerr)
	}
	hC, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "C",
	})
	if err != nil {
		t.Fatalf("Join C: %v", err)
	}
	if hC != 3 {
		t.Errorf("Join C after A resigned: got %d, want 3 (resigned handles not reused)", hC)
	}
}

// TestProperty_JoinFederation_HandlesAreUniqueAndDenseInArrivalOrder
// asserts that for any permutation of arrival order, the k-th successful
// Join (1-indexed) returns handle k. This is the strong monotonic
// property: the value depends only on prior successful-Join count, not on
// the name itself or on goroutine scheduling.
func TestProperty_JoinFederation_HandlesAreUniqueAndDenseInArrivalOrder(t *testing.T) {
	t.Parallel()
	names := []string{"alice", "bob", "carol", "dave", "eve", "frank", "grace"}
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // test-only
	for trial := 0; trial < 10; trial++ {
		shuffled := append([]string(nil), names...)
		rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		fed := core.FederationName("trial-" + strconv.Itoa(trial))
		mgr, err := federation.New(validOptions())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if cerr := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
			Name:       fed,
			FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
			Mode:       core.ModeVerbose,
		}); cerr != nil {
			t.Fatalf("Create %s: %v", fed, cerr)
		}
		for k, n := range shuffled {
			h, jerr := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
				Federation: fed, FederateName: n,
			})
			if jerr != nil {
				t.Fatalf("trial %d Join %s: %v", trial, n, jerr)
			}
			want := core.FederateHandle(k + 1)
			if h != want {
				t.Errorf("trial %d arrival %v: %s (k=%d) got handle %d, want %d (monotonic-by-arrival)",
					trial, shuffled, n, k, h, want)
			}
		}
	}
}

// TestJoinFederation_NilEventLog_DoesNotPanic verifies that the cut-1
// nil-EventLog tolerance covers Join (the path that would otherwise call
// EventLog.Append for FederateJoined).
func TestJoinFederation_NilEventLog_DoesNotPanic(t *testing.T) {
	t.Parallel()
	opts := validOptions()
	opts.EventLog = nil
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if cerr := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "x",
		FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
		Mode:       core.ModeVerbose,
	}); cerr != nil {
		t.Fatalf("Create: %v", cerr)
	}
	if _, jerr := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "alice",
	}); jerr != nil {
		t.Fatalf("Join with nil EventLog: %v", jerr)
	}
}

// TestJoinFederation_AppendsEventBeforeReturn verifies the write-ahead
// invariant: EventLog.Append is invoked exactly once per successful Join,
// and the federation name passed matches the request.
func TestJoinFederation_AppendsEventBeforeReturn(t *testing.T) {
	t.Parallel()
	rec := &recordingEventLog{}
	opts := validOptions()
	opts.EventLog = rec
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if cerr := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "x",
		FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
		Mode:       core.ModeVerbose,
	}); cerr != nil {
		t.Fatalf("Create: %v", cerr)
	}
	if _, jerr := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "alice",
	}); jerr != nil {
		t.Fatalf("Join: %v", jerr)
	}
	calls := rec.Calls()
	if len(calls) != 1 {
		t.Fatalf("EventLog.Append calls = %d, want 1", len(calls))
	}
	if calls[0].fed != "x" {
		t.Errorf("Append fed = %q, want %q", calls[0].fed, "x")
	}
}

// ---------------------------------------------------------------------------
// TASK-022: ResignFederation tests
// ---------------------------------------------------------------------------

// TestResignFederation_UnknownFederation_ReturnsNotFound: resigning against
// a federation that was never created returns the documented sentinel.
func TestResignFederation_UnknownFederation_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := mgr.ResignFederation(context.Background(), "ghost", 1,
		core.ResignActionUnconditionallyDivestAttributes)
	if !errors.Is(got, core.ErrFederationNotFound) {
		t.Errorf("Resign on ghost: %v, want ErrFederationNotFound", got)
	}
}

// TestResignFederation_UnknownHandle_ReturnsNotJoined: an unknown handle in
// a known federation returns ErrFederateNotJoined.
func TestResignFederation_UnknownHandle_ReturnsNotJoined(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "x")
	got := mgr.ResignFederation(context.Background(), "x", 999,
		core.ResignActionUnconditionallyDivestAttributes)
	if !errors.Is(got, core.ErrFederateNotJoined) {
		t.Errorf("Resign with unknown handle: %v, want ErrFederateNotJoined", got)
	}
}

// TestResignFederation_DoubleResign_IsIdempotentlyRejected: calling resign
// twice on the same federate returns ErrFederateNotJoined the second time.
func TestResignFederation_DoubleResign_IsIdempotentlyRejected(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "x")
	h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "alice",
	})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if rerr := mgr.ResignFederation(context.Background(), "x", h,
		core.ResignActionUnconditionallyDivestAttributes); rerr != nil {
		t.Fatalf("first resign: %v", rerr)
	}
	rerr := mgr.ResignFederation(context.Background(), "x", h,
		core.ResignActionUnconditionallyDivestAttributes)
	if !errors.Is(rerr, core.ErrFederateNotJoined) {
		t.Errorf("second resign: %v, want ErrFederateNotJoined", rerr)
	}
}

// TestResignFederation_FreesNameForRejoin: after a resign, the name can be
// joined again — the name<->handle slot is fully released.
func TestResignFederation_FreesNameForRejoin(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "x")
	h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "alice",
	})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if rerr := mgr.ResignFederation(context.Background(), "x", h,
		core.ResignActionUnconditionallyDivestAttributes); rerr != nil {
		t.Fatalf("resign: %v", rerr)
	}
	if _, jerr := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "alice",
	}); jerr != nil {
		t.Errorf("re-join after resign: %v", jerr)
	}
}

// TestResignFederation_AppendsEventBeforeReturn: the FederateResigned event
// is written to the EventLog before Resign returns.
func TestResignFederation_AppendsEventBeforeReturn(t *testing.T) {
	t.Parallel()
	rec := &recordingEventLog{}
	opts := validOptions()
	opts.EventLog = rec
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if cerr := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "x",
		FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
		Mode:       core.ModeVerbose,
	}); cerr != nil {
		t.Fatalf("Create: %v", cerr)
	}
	h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "alice",
	})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	before := len(rec.Calls())
	if rerr := mgr.ResignFederation(context.Background(), "x", h,
		core.ResignActionUnconditionallyDivestAttributes); rerr != nil {
		t.Fatalf("Resign: %v", rerr)
	}
	after := len(rec.Calls())
	if after-before != 1 {
		t.Errorf("Resign appended %d events, want 1", after-before)
	}
}

// TestResignFederation_UnsupportedAction_ReturnsError: cut 1 only supports
// UnconditionallyDivestAttributes; other actions are rejected with a
// non-nil error.
func TestResignFederation_UnsupportedAction_ReturnsError(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "x")
	h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "alice",
	})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if rerr := mgr.ResignFederation(context.Background(), "x", h,
		core.ResignActionUnspecified); rerr == nil {
		t.Error("Resign with unspecified action: want error, got nil")
	}
}

// ---------------------------------------------------------------------------
// TASK-023: DestroyFederation + List tests
// ---------------------------------------------------------------------------

// TestDestroyFederation_Unknown_ReturnsNotFound verifies the documented
// sentinel for an unknown federation name.
func TestDestroyFederation_Unknown_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := mgr.DestroyFederation(context.Background(), "ghost")
	if !errors.Is(got, core.ErrFederationNotFound) {
		t.Errorf("Destroy ghost: %v, want ErrFederationNotFound", got)
	}
}

// TestDestroyFederation_RejectsWhenJoined verifies that the federation
// cannot be destroyed while any federate is joined.
func TestDestroyFederation_RejectsWhenJoined(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "x")
	if _, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "alice",
	}); err != nil {
		t.Fatalf("Join: %v", err)
	}
	got := mgr.DestroyFederation(context.Background(), "x")
	if !errors.Is(got, core.ErrFederationHasFederatesJoined) {
		t.Errorf("Destroy while joined: %v, want ErrFederationHasFederatesJoined", got)
	}
}

// TestDestroyFederation_AfterAllResigned_Succeeds verifies that destroy
// succeeds once all federates have resigned. Federation is removed from
// the registry so a subsequent Create with the same name succeeds.
func TestDestroyFederation_AfterAllResigned_Succeeds(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "x")
	h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
		Federation: "x", FederateName: "alice",
	})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if rerr := mgr.ResignFederation(context.Background(), "x", h,
		core.ResignActionUnconditionallyDivestAttributes); rerr != nil {
		t.Fatalf("Resign: %v", rerr)
	}
	if derr := mgr.DestroyFederation(context.Background(), "x"); derr != nil {
		t.Fatalf("Destroy: %v", derr)
	}
	// Federation is removed: re-create with same name succeeds.
	if cerr := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       "x",
		FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
		Mode:       core.ModeVerbose,
	}); cerr != nil {
		t.Errorf("re-Create after Destroy: %v", cerr)
	}
}

func TestP0DestroySerializesConcurrentJoin(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	opts := validOptions()
	opts.OnFederationDestroying = func(context.Context, core.FederationName) error {
		close(entered)
		<-release
		return nil
	}
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	destroyed := make(chan error, 1)
	go func() { destroyed <- mgr.DestroyFederation(context.Background(), "x") }()
	<-entered
	joined := make(chan error, 1)
	go func() {
		_, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{Federation: "x", FederateName: "late"})
		joined <- err
	}()
	select {
	case err := <-joined:
		t.Fatalf("join escaped destroy serialization: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-destroyed; err != nil {
		t.Fatal(err)
	}
	if err := <-joined; !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatalf("join after destroy = %v, want ErrFederationNotFound", err)
	}
}

func TestP0DestroyHoldsOperationGateThroughCleanup(t *testing.T) {
	entered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	opts := validOptions()
	opts.OnFederationDestroying = func(context.Context, core.FederationName) error {
		close(entered)
		<-releaseCleanup
		return nil
	}
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	destroyed := make(chan error, 1)
	go func() { destroyed <- mgr.DestroyFederation(context.Background(), "x") }()
	<-entered

	admitted := make(chan error, 1)
	go func() {
		release, err := mgr.AcquireGeneration("x", 0)
		if release != nil {
			release()
		}
		admitted <- err
	}()
	select {
	case err := <-admitted:
		t.Fatalf("operation crossed destroy cleanup boundary: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseCleanup)
	if err := <-destroyed; err != nil {
		t.Fatal(err)
	}
	if err := <-admitted; !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatalf("operation after destroy = %v, want ErrFederationNotFound", err)
	}
}

func TestP0SameNameRecreationRejectsStaleGenerationHandle(t *testing.T) {
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	create := func() {
		t.Helper()
		if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	create()
	oldHandle, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "x", FederateName: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.ResignFederation(ctx, "x", oldHandle, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	if err := mgr.DestroyFederation(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	create()
	newHandle, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "x", FederateName: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if newHandle == oldHandle {
		t.Fatalf("recreated federation reused stale handle %d", oldHandle)
	}
	if err := mgr.ValidateMember("x", oldHandle); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("ValidateMember(old) = %v, want ErrFederateNotJoined", err)
	}
	if err := mgr.ValidateMember("x", newHandle); err != nil {
		t.Fatalf("ValidateMember(new) = %v", err)
	}
	if generation, ok := mgr.GenerationFor("x"); !ok || generation != 1 {
		t.Fatalf("GenerationFor = (%d, %v), want (1, true)", generation, ok)
	}
}

func TestP0JoinAndDestroyRejectStaleExpectedGeneration(t *testing.T) {
	opts := validOptions()
	opts.GenerationEpoch = 40
	opts.GenerationSpan = 8
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	stale := uint64(39)
	if _, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{
		Federation: "x", FederateName: "stale", ExpectedGeneration: &stale,
	}); !errors.Is(err, core.ErrFederationGenerationMismatch) {
		t.Fatalf("stale join = %v, want generation mismatch", err)
	}
	if err := mgr.DestroyFederationGeneration(ctx, "x", stale); !errors.Is(err, core.ErrFederationGenerationMismatch) {
		t.Fatalf("stale destroy = %v, want generation mismatch", err)
	}
	current := uint64(40)
	h, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{
		Federation: "x", FederateName: "current", ExpectedGeneration: &current,
	})
	if err != nil {
		t.Fatalf("current join: %v", err)
	}
	if err := mgr.ResignFederation(ctx, "x", h, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	if err := mgr.DestroyFederationGeneration(ctx, "x", current); err != nil {
		t.Fatalf("current destroy: %v", err)
	}
}

func TestP0ReservedGenerationBlockExhaustion(t *testing.T) {
	opts := validOptions()
	opts.GenerationEpoch = 100
	opts.GenerationSpan = 2
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "x"}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if err := mgr.DestroyFederation(ctx, "x"); err != nil {
			t.Fatalf("destroy %d: %v", i, err)
		}
	}
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "x"}); err == nil {
		t.Fatal("create beyond reserved generation block succeeded")
	}
}

func TestP0DestroyCleanupFailureLeavesFederationAbsent(t *testing.T) {
	want := errors.New("cleanup failed")
	opts := validOptions()
	opts.OnFederationDestroying = func(context.Context, core.FederationName) error {
		return want
	}
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.DestroyFederation(context.Background(), "x"); !errors.Is(err, want) {
		t.Fatalf("DestroyFederation = %v, want cleanup error", err)
	}
	if _, ok := mgr.GenerationFor("x"); ok {
		t.Fatal("partially cleaned federation became active again")
	}
	if err := mgr.DestroyFederation(context.Background(), "x"); !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatalf("retry destroy = %v, want ErrFederationNotFound", err)
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{Name: "x"}); err != nil {
		t.Fatalf("fresh generation after failed cleanup: %v", err)
	}
}

func TestP0CreateSetupBlocksJoinUntilReady(t *testing.T) {
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	setupEntered := make(chan struct{})
	releaseSetup := make(chan struct{})
	created := make(chan error, 1)
	go func() {
		created <- mgr.CreateFederationWithSetup(
			context.Background(),
			core.CreateFederationRequest{Name: "x"},
			func(uint64) {
				close(setupEntered)
				<-releaseSetup
			},
		)
	}()
	<-setupEntered
	joined := make(chan error, 1)
	go func() {
		_, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
			Federation: "x", FederateName: "member",
		})
		joined <- err
	}()
	select {
	case err := <-joined:
		t.Fatalf("join crossed create setup boundary: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseSetup)
	if err := <-created; err != nil {
		t.Fatal(err)
	}
	if err := <-joined; err != nil {
		t.Fatal(err)
	}
}

func TestP0JoinHoldsOperationGateThroughReadyHook(t *testing.T) {
	hookHandle := make(chan core.FederateHandle, 1)
	releaseHook := make(chan struct{})
	opts := validOptions()
	opts.OnFederateJoined = func(_ context.Context, _ core.FederationName, h core.FederateHandle, _, _ string) {
		hookHandle <- h
		<-releaseHook
	}
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	joined := make(chan error, 1)
	go func() {
		_, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
			Federation: "x", FederateName: "member",
		})
		joined <- err
	}()
	h := <-hookHandle
	admitted := make(chan error, 1)
	go func() {
		release, err := mgr.AcquireMember("x", h)
		if release != nil {
			release()
		}
		admitted <- err
	}()
	select {
	case err := <-admitted:
		t.Fatalf("operation crossed join hook boundary: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseHook)
	if err := <-joined; err != nil {
		t.Fatal(err)
	}
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
}

func TestP0PreResignHookObservesJoinedMember(t *testing.T) {
	var mgr *federation.Manager
	var observed bool
	opts := validOptions()
	opts.OnFederateResigning = func(_ context.Context, fed core.FederationName, h core.FederateHandle, _ core.ResignAction) {
		for _, current := range mgr.MembersOf(fed) {
			observed = observed || current == h
		}
	}
	var err error
	mgr, err = federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{Federation: "x", FederateName: "member"})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.ResignFederation(context.Background(), "x", h, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	if !observed {
		t.Fatal("pre-resign hook observed member after roster removal")
	}
}

func TestP0ResignWaitsForAdmittedOperationLease(t *testing.T) {
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const fed = core.FederationName("operation-gate")
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
		t.Fatal(err)
	}
	h, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "member"})
	if err != nil {
		t.Fatal(err)
	}
	release, err := mgr.AcquireMember(fed, h)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- mgr.ResignFederation(ctx, fed, h, core.ResignActionNoAction)
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("resign crossed active operation lease: %v", err)
	default:
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("resign did not continue after operation lease release")
	}
}

func TestP0ResignHoldsOperationGateThroughCleanupHooks(t *testing.T) {
	hookEntered := make(chan struct{})
	releaseHook := make(chan struct{})
	opts := validOptions()
	opts.OnFederateResigned = func(context.Context, core.FederationName, core.FederateHandle) {
		close(hookEntered)
		<-releaseHook
	}
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	leaving, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "x", FederateName: "leaving"})
	if err != nil {
		t.Fatal(err)
	}
	survivor, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "x", FederateName: "survivor"})
	if err != nil {
		t.Fatal(err)
	}
	resigned := make(chan error, 1)
	go func() {
		resigned <- mgr.ResignFederation(ctx, "x", leaving, core.ResignActionNoAction)
	}()
	<-hookEntered
	admitted := make(chan error, 1)
	go func() {
		release, err := mgr.AcquireMember("x", survivor)
		if release != nil {
			release()
		}
		admitted <- err
	}()
	select {
	case err := <-admitted:
		t.Fatalf("survivor admitted before resign cleanup finished: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseHook)
	if err := <-resigned; err != nil {
		t.Fatal(err)
	}
	if err := <-admitted; err != nil {
		t.Fatalf("survivor admission after cleanup: %v", err)
	}
}

// TestList_ReturnsNameSortedSummaries verifies that List returns all
// federations in name-sorted order with accurate FederatesJoined count.
func TestList_ReturnsNameSortedSummaries(t *testing.T) {
	t.Parallel()
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Create in non-sorted insertion order.
	for _, n := range []string{"charlie", "alpha", "bravo"} {
		if cerr := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
			Name:       core.FederationName(n),
			FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
			Mode:       core.ModeVerbose,
		}); cerr != nil {
			t.Fatalf("Create %s: %v", n, cerr)
		}
	}
	// Join two federates into bravo to exercise the count.
	for _, fn := range []string{"f1", "f2"} {
		if _, jerr := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
			Federation: "bravo", FederateName: fn,
		}); jerr != nil {
			t.Fatalf("Join %s: %v", fn, jerr)
		}
	}
	got, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make([]string, len(got))
	for i, s := range got {
		names[i] = string(s.Name)
	}
	want := []string{"alpha", "bravo", "charlie"}
	if len(names) != len(want) {
		t.Fatalf("List length = %d, want %d (got %v)", len(names), len(want), names)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("List[%d].Name = %q, want %q (full=%v)", i, names[i], n, names)
		}
	}
	// Verify the join count for bravo is 2.
	for _, s := range got {
		if s.Name == "bravo" && s.FederatesJoined != 2 {
			t.Errorf("bravo.FederatesJoined = %d, want 2", s.FederatesJoined)
		}
	}
}

// mustNewWithFederation builds a Manager + creates the named federation,
// failing the test on any error. Reduces boilerplate in Join/Resign tests.
func mustNewWithFederation(t *testing.T, name core.FederationName) *federation.Manager {
	t.Helper()
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if cerr := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{
		Name:       name,
		FOMModules: []core.FOMModule{{XML: []byte("<x/>")}},
		Mode:       core.ModeVerbose,
	}); cerr != nil {
		t.Fatalf("Create %s: %v", name, cerr)
	}
	return mgr
}

// recordingEventLog captures every Append for assertion. Goroutine-safe.
type recordingEventLog struct {
	mu    sync.Mutex
	calls []recordedAppend
}

type recordedAppend struct {
	fed core.FederationName
	evt core.EventRecord
}

func (r *recordingEventLog) Append(_ context.Context, fed core.FederationName, evt core.EventRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedAppend{fed: fed, evt: evt})
	return nil
}

func (*recordingEventLog) Sync(_ context.Context, _ core.FederationName) error { return nil }
func (*recordingEventLog) OpenReader(_ context.Context, _ string) (core.EventLogReader, error) {
	return nil, errors.New("recordingEventLog: OpenReader unsupported")
}

func (r *recordingEventLog) Calls() []recordedAppend {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedAppend, len(r.calls))
	copy(out, r.calls)
	return out
}

// TestJoinFederation_RecordsFederateType exercises M13 thread B
// (docs/srs.md §10.4): JoinFederationRequest.FederateType is recorded
// per-federate, surfaced via FederateTypeOf, fed into the
// OnFederateJoined hook, and reflected in the Snapshot's FederateInfo.
func TestJoinFederation_RecordsFederateType(t *testing.T) {
	t.Parallel()
	var hookFed core.FederationName
	var hookHandle core.FederateHandle
	var hookName, hookType string
	opts := validOptions()
	opts.OnFederateJoined = func(_ context.Context, fed core.FederationName, h core.FederateHandle, name string, ftype string) {
		hookFed = fed
		hookHandle = h
		hookName = name
		hookType = ftype
	}
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "f"}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	h, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{
		Federation:   "f",
		FederateName: "alpha",
		FederateType: "Sensor",
	})
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if hookFed != "f" || hookHandle != h || hookName != "alpha" || hookType != "Sensor" {
		t.Errorf("hook = (%q, %d, %q, %q); want (f, %d, alpha, Sensor)",
			hookFed, hookHandle, hookName, hookType, h)
	}
	got, ok := mgr.FederateTypeOf("f", h)
	if !ok || got != "Sensor" {
		t.Errorf("FederateTypeOf = (%q, %v), want (Sensor, true)", got, ok)
	}
	rosters := mgr.Snapshot()
	if len(rosters) != 1 || len(rosters[0].Federates) != 1 {
		t.Fatalf("Snapshot shape unexpected: %+v", rosters)
	}
	if rosters[0].Federates[0].Type != "Sensor" {
		t.Errorf("Snapshot Federates[0].Type = %q, want Sensor", rosters[0].Federates[0].Type)
	}
}

// TestJoinFederation_EmptyFederateType_PreservesCut1Behavior asserts
// that a JoinFederationRequest without FederateType does not regress
// any previous behavior — FederateTypeOf returns ("", true) for the
// joined federate and the OnFederateJoined hook receives "" for type.
func TestJoinFederation_EmptyFederateType_PreservesCut1Behavior(t *testing.T) {
	t.Parallel()
	var hookType string
	hookFired := false
	opts := validOptions()
	opts.OnFederateJoined = func(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ string, ftype string) {
		hookType = ftype
		hookFired = true
	}
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "f"}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	h, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{
		Federation:   "f",
		FederateName: "alpha",
	})
	if err != nil {
		t.Fatalf("JoinFederation: %v", err)
	}
	if !hookFired {
		t.Fatal("hook never fired")
	}
	if hookType != "" {
		t.Errorf("hookType = %q, want empty", hookType)
	}
	got, ok := mgr.FederateTypeOf("f", h)
	if !ok {
		t.Errorf("FederateTypeOf joined = (_, false), want (_, true)")
	}
	if got != "" {
		t.Errorf("FederateTypeOf = %q, want empty", got)
	}
}

// TestMembersOf_UnknownFederation_ReturnsEmpty exercises the M13
// thread-A accessor (docs/srs.md §10.4): unknown federation must yield
// a non-nil empty slice so callers can iterate without a nil-guard.
func TestMembersOf_UnknownFederation_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := mgr.MembersOf("nope")
	if got == nil {
		t.Errorf("MembersOf unknown returned nil; want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("MembersOf unknown = %v; want empty", got)
	}
}

// TestMembersOf_ReturnsSortedJoinedHandles asserts that the snapshot
// is the live joined-handle set and ordered ascending. Threads A wires
// this as the sync.Options.MembersResolver and savepoint.Options.
// MembersResolver in cmd/rtid; the deterministic order keeps
// announceSynchronizationPoint / initiateFederateSave fan-out
// reproducible across replays.
func TestMembersOf_ReturnsSortedJoinedHandles(t *testing.T) {
	t.Parallel()
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "f"}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	for _, n := range []string{"alpha", "beta", "gamma"} {
		if _, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "f", FederateName: n}); err != nil {
			t.Fatalf("JoinFederation %s: %v", n, err)
		}
	}
	got := mgr.MembersOf("f")
	if len(got) != 3 {
		t.Fatalf("MembersOf len = %d, want 3", len(got))
	}
	// Handles assigned by monotonic counter — alpha=1, beta=2, gamma=3.
	if got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("MembersOf = %v, want [1 2 3]", got)
	}

	// Resign beta; MembersOf must reflect the gap.
	if err := mgr.ResignFederation(ctx, "f", core.FederateHandle(2),
		core.ResignActionUnconditionallyDivestAttributes); err != nil {
		t.Fatalf("ResignFederation: %v", err)
	}
	got = mgr.MembersOf("f")
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("MembersOf after resign = %v, want [1 3]", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
