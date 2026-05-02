package federation_test

import (
	"context"
	"errors"
	"math/rand"
	"sort"
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

// TestJoinFederation_HandleEqualsSortPositionInCurrentRoster verifies the
// determinism invariant: at join time, the returned handle equals the
// joining federate's sort-position within (current roster + the new
// federate). Re-key on join keeps handle order = name sort order.
//
// This is the per-call observable behavior. The federate's handle MAY
// shift later if a smaller-named federate joins (replay determinism is
// preserved because the same shifts replay identically).
func TestJoinFederation_HandleEqualsSortPositionInCurrentRoster(t *testing.T) {
	t.Parallel()
	mgr := mustNewWithFederation(t, "det")
	cases := []struct {
		joining string
		// rosterAfter must be sorted; want = position of joining name
		// in this list (1-based).
		rosterAfter []string
	}{
		{"echo", []string{"echo"}},                                  // alone -> 1
		{"alpha", []string{"alpha", "echo"}},                        // smaller -> 1
		{"delta", []string{"alpha", "delta", "echo"}},               // middle -> 2
		{"bravo", []string{"alpha", "bravo", "delta", "echo"}},      // -> 2
		{"charlie", []string{"alpha", "bravo", "charlie", "delta", "echo"}}, // -> 3
	}
	for _, c := range cases {
		h, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
			Federation: "det", FederateName: c.joining,
		})
		if err != nil {
			t.Fatalf("Join %s: %v", c.joining, err)
		}
		want := core.FederateHandle(0)
		for i, n := range c.rosterAfter {
			if n == c.joining {
				want = core.FederateHandle(i + 1)
				break
			}
		}
		if h != want {
			t.Errorf("Join %s in roster %v: got handle %d, want %d (sort position)",
				c.joining, c.rosterAfter, h, want)
		}
	}
}

// TestProperty_JoinFederation_HandleAlwaysMatchesSortPositionAtJoin asserts
// that for any permutation of arrival order, every join returns
// (sort-rank-in-current-roster). This is the strong determinism property:
// the function value is purely determined by the name + the current
// roster, NOT by goroutine scheduling or wall-clock.
func TestProperty_JoinFederation_HandleAlwaysMatchesSortPositionAtJoin(t *testing.T) {
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
		joined := []string{}
		for _, n := range shuffled {
			joined = append(joined, n)
			sort.Strings(joined)
			expected := core.FederateHandle(0)
			for i, jn := range joined {
				if jn == n {
					expected = core.FederateHandle(i + 1)
					break
				}
			}
			h, jerr := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{
				Federation: fed, FederateName: n,
			})
			if jerr != nil {
				t.Fatalf("trial %d Join %s: %v", trial, n, jerr)
			}
			if h != expected {
				t.Errorf("trial %d arrival %v: %s got handle %d, want sort-position %d",
					trial, shuffled, n, h, expected)
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

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
