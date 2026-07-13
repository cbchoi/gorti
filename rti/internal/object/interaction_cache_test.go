package object

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
)

type countingInteractionFOM struct {
	mu        sync.Mutex
	names     map[core.InteractionClassHandle]string
	nameCalls int
}

func (*countingInteractionFOM) IsValid() bool { return true }
func (*countingInteractionFOM) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 0, false
}
func (*countingInteractionFOM) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 0, false
}
func (*countingInteractionFOM) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 0, false
}
func (*countingInteractionFOM) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 0, false
}
func (*countingInteractionFOM) ObjectClassName(core.ObjectClassHandle) (string, bool) {
	return "", false
}
func (f *countingInteractionFOM) InteractionClassName(cls core.InteractionClassHandle) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nameCalls++
	name, ok := f.names[cls]
	return name, ok
}
func (*countingInteractionFOM) AttributeName(core.ObjectClassHandle, core.AttributeHandle) (string, bool) {
	return "", false
}
func (*countingInteractionFOM) ParameterName(_ core.InteractionClassHandle, parameter core.ParameterHandle) (string, bool) {
	return "Payload", parameter == 1
}
func (*countingInteractionFOM) LookupDimension(string) (core.DimensionHandle, bool) {
	return 0, false
}
func (*countingInteractionFOM) DimensionName(core.DimensionHandle) (string, bool) {
	return "", false
}
func (*countingInteractionFOM) DimensionUpperBound(core.DimensionHandle) (uint64, bool) {
	return 0, false
}

func (f *countingInteractionFOM) NameCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nameCalls
}

type countingInteractionFOMRepo struct {
	mu     sync.Mutex
	handle core.FOMHandle
	gets   int
}

func (r *countingInteractionFOMRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return r.handle, nil
}
func (r *countingInteractionFOMRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gets++
	return r.handle, nil
}
func (r *countingInteractionFOMRepo) Gets() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gets
}

type managementDispatchCall struct {
	fed    core.FederationName
	name   string
	sender core.FederateHandle
	fom    core.FOMHandle
	names  core.FOMHandleNameLookup
}

type recordingManagementDispatcher struct {
	mu            sync.Mutex
	managerNames  map[string]bool
	classifyCalls int
	dispatches    []managementDispatchCall
}

func (d *recordingManagementDispatcher) IsManagerClass(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.classifyCalls++
	return d.managerNames[name]
}

func (d *recordingManagementDispatcher) Dispatch(
	_ context.Context,
	fed core.FederationName,
	name string,
	sender core.FederateHandle,
	_ map[core.ParameterHandle][]byte,
	fom core.FOMHandle,
	names core.FOMHandleNameLookup,
) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dispatches = append(d.dispatches, managementDispatchCall{
		fed: fed, name: name, sender: sender, fom: fom, names: names,
	})
	return nil
}

func (d *recordingManagementDispatcher) Counts() (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.classifyCalls, len(d.dispatches)
}

func (d *recordingManagementDispatcher) Dispatches() []managementDispatchCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]managementDispatchCall(nil), d.dispatches...)
}

func newInteractionCacheRegistry(
	t *testing.T,
	fom *countingInteractionFOM,
	dispatch *recordingManagementDispatcher,
) (*Registry, *declaration.Manager, *recordingOutbox, *recordingEventLog, *countingInteractionFOMRepo) {
	t.Helper()
	decl := declaration.New()
	outbox := &recordingOutbox{}
	log := &recordingEventLog{}
	repo := &countingInteractionFOMRepo{handle: fom}
	reg, err := New(Options{
		EventLog:           log,
		Declarations:       decl,
		Outbox:             outbox,
		FOMs:               repo,
		Clock:              core.NewFakeClock(time.Unix(0, 0)),
		ManagementDispatch: dispatch,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return reg, decl, outbox, log, repo
}

func TestSendInteraction_CachesNormalClassClassificationPerFederation(t *testing.T) {
	t.Parallel()
	const cls = core.InteractionClassHandle(11)
	fom := &countingInteractionFOM{names: map[core.InteractionClassHandle]string{
		cls: "HLAinteractionRoot.ChatMessage",
	}}
	dispatch := &recordingManagementDispatcher{managerNames: map[string]bool{}}
	reg, decl, outbox, _, repo := newInteractionCacheRegistry(t, fom, dispatch)
	ctx := context.Background()

	for _, fed := range []core.FederationName{"alpha", "beta"} {
		if err := decl.PublishInteractionClass(ctx, fed, 1, cls); err != nil {
			t.Fatalf("PublishInteractionClass(%q): %v", fed, err)
		}
		if err := decl.SubscribeInteractionClass(ctx, fed, 2, cls); err != nil {
			t.Fatalf("SubscribeInteractionClass(%q): %v", fed, err)
		}
		for i := 0; i < 2; i++ {
			if err := reg.SendInteraction(ctx, fed, 1, cls, nil, nil); err != nil {
				t.Fatalf("SendInteraction(%q, %d): %v", fed, i, err)
			}
		}
	}

	if got := fom.NameCalls(); got != 2 {
		t.Fatalf("InteractionClassName calls = %d, want 2 (once per federation)", got)
	}
	if got := repo.Gets(); got != 2 {
		t.Fatalf("FOM repository Get calls = %d, want 2 (once per federation)", got)
	}
	classifyCalls, dispatchCalls := dispatch.Counts()
	if classifyCalls != 2 || dispatchCalls != 0 {
		t.Fatalf("management calls = classify %d, dispatch %d; want 2, 0", classifyCalls, dispatchCalls)
	}
	if got := len(outbox.Records()); got != 4 {
		t.Fatalf("normal interaction fanout count = %d, want 4", got)
	}
}

func TestSendInteraction_CachedManagerClassStillDispatches(t *testing.T) {
	t.Parallel()
	const (
		fed     = core.FederationName("fed")
		cls     = core.InteractionClassHandle(99)
		manager = "HLAinteractionRoot.HLAmanager.HLArequest.HLArequestPublications"
	)
	fom := &countingInteractionFOM{names: map[core.InteractionClassHandle]string{cls: manager}}
	dispatch := &recordingManagementDispatcher{managerNames: map[string]bool{manager: true}}
	reg, _, outbox, log, repo := newInteractionCacheRegistry(t, fom, dispatch)

	for i := 0; i < 2; i++ {
		if err := reg.SendInteraction(context.Background(), fed, 7, cls, map[core.ParameterHandle][]byte{1: {byte(i)}}, nil); err != nil {
			t.Fatalf("SendInteraction(%d): %v", i, err)
		}
	}

	if got := fom.NameCalls(); got != 1 {
		t.Fatalf("InteractionClassName calls = %d, want 1", got)
	}
	if got := repo.Gets(); got != 1 {
		t.Fatalf("FOM repository Get calls = %d, want 1", got)
	}
	classifyCalls, dispatchCalls := dispatch.Counts()
	if classifyCalls != 1 || dispatchCalls != 2 {
		t.Fatalf("management calls = classify %d, dispatch %d; want 1, 2", classifyCalls, dispatchCalls)
	}
	for i, call := range dispatch.Dispatches() {
		if call.fed != fed || call.name != manager || call.sender != 7 || call.fom != fom || call.names != fom {
			t.Errorf("dispatch[%d] = %+v, want original federation, class, sender, and FOM handles", i, call)
		}
	}
	if got := len(log.Records()); got != 0 {
		t.Fatalf("manager interaction eventlog appends = %d, want 0", got)
	}
	if got := len(outbox.Records()); got != 0 {
		t.Fatalf("manager interaction fanout count = %d, want 0", got)
	}
}
