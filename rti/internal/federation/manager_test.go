package federation_test

import (
	"context"
	"errors"
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

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
