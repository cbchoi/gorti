package object

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type knownOwnershipFOM struct{ roFOM }

func (*knownOwnershipFOM) AttributeHandles(cls core.ObjectClassHandle) []core.AttributeHandle {
	if cls == 9 {
		return []core.AttributeHandle{101}
	}
	if cls == 1 {
		return []core.AttributeHandle{1, 2}
	}
	return nil
}

type knownOwnershipFOMs struct{ stubFOMRepo }

func (*knownOwnershipFOMs) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return &knownOwnershipFOM{}, nil
}

type knownOwnershipPreparation func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, func(), error)

func knownOwnershipPreparationFixture(t *testing.T, configure ...func(*ownership.Options)) (*Registry, *declaration.Manager, *ownership.Manager, core.ObjectHandle) {
	t.Helper()
	ctx := context.Background()
	r, decl, out := newTestRegistry(t, nil)
	r.opts.FOMs = &knownOwnershipFOMs{}
	decl.SetFOMRepository(r.opts.FOMs)
	decl.SetOnSubscribeObjectClass(r.ObjectClassSubscribed)
	opts := ownership.Options{
		Outbox: out, PrepareAcquisition: r.PrepareOwnershipAcquisition,
		PrepareDivestiture:      r.PrepareOwnershipReleaseDenied,
		PrepareKnownAcquisition: r.PrepareKnownOwnershipAcquisition,
		PrepareKnownOperation:   r.PrepareKnownOwnershipOperation,
	}
	for _, change := range configure {
		change(&opts)
	}
	owners, err := ownership.New(opts)
	transportationOK(t, err)
	r.opts.Ownership = owners
	r.opts.OnRegister = func(fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
		owners.RegisterInitialOwnership(fed, caller, obj, attrs)
	}
	transportationOK(t, decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1, 2}))
	transportationOK(t, decl.PublishObjectClassAttributes(ctx, "fed", 2, 9, []core.AttributeHandle{101}))
	transportationOK(t, decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 9, []core.AttributeHandle{101}))
	reserveFixtureName(t, r, "fed", 1, "known-ownership-pin")
	obj, _, err := r.Register(ctx, "fed", 1, 1, "known-ownership-pin")
	transportationOK(t, err)
	if _, cls, known := r.LookupKnownObjectInstance("fed", 2, obj); !known || cls != 9 {
		t.Fatalf("fixture known class=%d known=%t", cls, known)
	}
	// Rediscovery would select the child, but current knowledge remains parent.
	transportationOK(t, decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{2}))
	return r, decl, owners, obj
}

func awaitKnownLocalDelete(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("local deletion remained blocked after ownership release")
		return nil
	}
}

func TestKnownOwnershipPreparationPinsMappingThroughMutation(t *testing.T) {
	for _, acquisition := range []bool{false, true} {
		name := "operation"
		if acquisition {
			name = "acquisition"
		}
		t.Run(name, func(t *testing.T) {
			r, _, owners, obj := knownOwnershipPreparationFixture(t)
			prepare := knownOwnershipPreparation(r.PrepareKnownOwnershipOperation)
			if acquisition {
				prepare = r.PrepareKnownOwnershipAcquisition
			}
			input := []core.AttributeHandle{101}
			actual, release, err := prepare(context.Background(), "fed", 2, obj, input)
			transportationOK(t, err)
			defer release()
			if !slices.Equal(actual, []core.AttributeHandle{1}) || !slices.Equal(input, []core.AttributeHandle{101}) {
				t.Fatalf("actual=%v caller input=%v", actual, input)
			}
			st := r.stateFor("fed")
			if st.mu.TryLock() {
				st.mu.Unlock()
				t.Fatal("mapping returned without pinning object knowledge")
			}
			started, done := make(chan struct{}), make(chan error, 1)
			go func() {
				close(started)
				done <- r.LocalDelete(context.Background(), "fed", 2, obj)
			}()
			<-started
			// The direct manager write models the canonical mutation executed by
			// its prepared internal path, without recursively preparing the object.
			owners.RegisterInitialOwnership("fed", 2, obj, actual)
			if !owners.IsOwnedBy("fed", 2, obj, 1) || !owners.IsOwnedBy("fed", 1, obj, 2) {
				t.Fatal("canonical mutation changed the wrong attribute")
			}
			select {
			case err := <-done:
				t.Fatalf("local deletion crossed the ownership pin: %v", err)
			default:
			}
			release()
			release()
			if err := awaitKnownLocalDelete(t, done); !errors.Is(err, core.ErrFederateOwnsAttributes) {
				t.Fatalf("local deletion did not see the committed owner: %v", err)
			}
			if _, cls, known := r.LookupKnownObjectInstance("fed", 2, obj); !known || cls != 9 {
				t.Fatalf("mutation lost pinned knowledge: class=%d known=%t", cls, known)
			}
		})
	}
}

func TestKnownOwnershipPreparationReleaseAllowsRediscovery(t *testing.T) {
	r, _, _, obj := knownOwnershipPreparationFixture(t)
	actual, release, err := r.PrepareKnownOwnershipOperation(context.Background(), "fed", 2, obj, []core.AttributeHandle{101})
	transportationOK(t, err)
	defer release()
	if !slices.Equal(actual, []core.AttributeHandle{1}) {
		t.Fatalf("mapped=%v", actual)
	}
	st := r.stateFor("fed")
	if st.mu.TryLock() {
		st.mu.Unlock()
		t.Fatal("operation did not retain the object pin")
	}
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		done <- r.LocalDelete(context.Background(), "fed", 2, obj)
	}()
	<-started
	release()
	release()
	transportationOK(t, awaitKnownLocalDelete(t, done))
	if _, cls, known := r.LookupKnownObjectInstance("fed", 2, obj); !known || cls != 1 {
		t.Fatalf("post-release rediscovery class=%d known=%t", cls, known)
	}
	if _, release, err := r.PrepareKnownOwnershipOperation(context.Background(), "fed", 2, obj, []core.AttributeHandle{101}); !errors.Is(err, core.ErrAttributeNotFound) {
		if release != nil {
			release()
		}
		t.Fatalf("old parent coordinates survived child rediscovery: %v", err)
	}
}

func TestKnownOwnershipPreparationRejectsWholeRequest(t *testing.T) {
	for _, acquisition := range []bool{false, true} {
		for _, failure := range []string{"later attribute", "unknown object", "unknown caller", "unpublished", "cancelled"} {
			name := "operation/" + failure
			if acquisition {
				name = "acquisition/" + failure
			}
			t.Run(name, func(t *testing.T) {
				r, decl, owners, obj := knownOwnershipPreparationFixture(t)
				prepare := knownOwnershipPreparation(r.PrepareKnownOwnershipOperation)
				if acquisition {
					prepare = r.PrepareKnownOwnershipAcquisition
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				attrs, caller, target := []core.AttributeHandle{101}, core.FederateHandle(2), obj
				var want error
				switch failure {
				case "later attribute":
					attrs = append(attrs, 999)
					want = core.ErrAttributeNotFound
				case "unknown object":
					target, want = obj+100, core.ErrObjectHandleInvalid
				case "unknown caller":
					caller, want = 4, core.ErrObjectHandleInvalid
				case "unpublished":
					transportationOK(t, decl.UnpublishObjectClassAttributes(ctx, "fed", 2, 9, []core.AttributeHandle{101}))
					if acquisition {
						want = core.ErrObjectClassNotPublished
					}
				case "cancelled":
					cancel()
					want = context.Canceled
				}
				before, ownerBefore := r.ExportState("fed"), owners.ExportState("fed")
				actual, release, err := prepare(ctx, "fed", caller, target, attrs)
				if release != nil {
					release()
					release()
				}
				if !errors.Is(err, want) || want != nil && (actual != nil || release != nil) {
					t.Errorf("actual=%v lease=%t error=%v, want %v", actual, release != nil, err, want)
				}
				if !reflect.DeepEqual(before, r.ExportState("fed")) || !reflect.DeepEqual(ownerBefore, owners.ExportState("fed")) {
					t.Fatal("preparation rejection changed object or ownership state")
				}
			})
		}
	}
}

type knownOwnershipBlockedFOMs struct {
	knownOwnershipFOMs
	once             sync.Once
	entered, proceed chan struct{}
}

func (f *knownOwnershipBlockedFOMs) Get(ctx context.Context, fed core.FederationName) (core.FOMHandle, error) {
	f.once.Do(func() {
		close(f.entered)
		<-f.proceed
	})
	return f.knownOwnershipFOMs.Get(ctx, fed)
}

func TestKnownOwnershipPreparationCancelledDuringMapping(t *testing.T) {
	for _, acquisition := range []bool{false, true} {
		name := "operation"
		if acquisition {
			name = "acquisition"
		}
		t.Run(name, func(t *testing.T) {
			r, _, owners, obj := knownOwnershipPreparationFixture(t)
			prepare := knownOwnershipPreparation(r.PrepareKnownOwnershipOperation)
			if acquisition {
				prepare = r.PrepareKnownOwnershipAcquisition
			}
			before := owners.ExportState("fed")
			gate := &knownOwnershipBlockedFOMs{entered: make(chan struct{}), proceed: make(chan struct{})}
			r.opts.FOMs = gate
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			unblock := sync.OnceFunc(func() { close(gate.proceed) })
			defer unblock()
			type result struct {
				actual  []core.AttributeHandle
				release func()
				err     error
			}
			done := make(chan result, 1)
			go func() {
				actual, release, err := prepare(ctx, "fed", 2, obj, []core.AttributeHandle{101})
				done <- result{actual, release, err}
			}()
			select {
			case <-gate.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("preparation did not reach FOM mapping barrier")
			}
			st := r.stateFor("fed")
			if st.mu.TryLock() {
				st.mu.Unlock()
				t.Fatal("mapping barrier did not retain object pin")
			}
			cancel()
			unblock()
			select {
			case got := <-done:
				if got.release != nil {
					got.release()
				}
				if !errors.Is(got.err, context.Canceled) || got.actual != nil || got.release != nil {
					t.Fatalf("cancelled mapping result=%v lease=%t error=%v", got.actual, got.release != nil, got.err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("cancelled preparation retained its lease")
			}
			if !st.mu.TryLock() {
				t.Fatal("cancelled preparation leaked object lock")
			}
			st.mu.Unlock()
			if !reflect.DeepEqual(before, owners.ExportState("fed")) {
				t.Fatal("cancelled mapping changed ownership")
			}
		})
	}
}

func TestKnownOwnershipPreparationEmptySetStillPinsKnowledge(t *testing.T) {
	r, _, owners, obj := knownOwnershipPreparationFixture(t)
	for _, prepare := range []knownOwnershipPreparation{r.PrepareKnownOwnershipAcquisition, r.PrepareKnownOwnershipOperation} {
		before := owners.ExportState("fed")
		actual, release, err := prepare(context.Background(), "fed", 2, obj, nil)
		transportationOK(t, err)
		st := r.stateFor("fed")
		if len(actual) != 0 || release == nil {
			t.Fatal("invalid empty-set preparation")
		}
		if st.mu.TryLock() {
			st.mu.Unlock()
			release()
			t.Fatal("empty-set preparation lost object pin")
		}
		release()
		release()
		if !reflect.DeepEqual(before, owners.ExportState("fed")) {
			t.Fatal("empty preparation changed ownership")
		}
	}
}

func TestKnownOwnershipCoordinatorAcquisitionPinsAgainstLocalDelete(t *testing.T) {
	entered, proceed := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(proceed) })
	defer unblock()
	r, _, owners, obj := knownOwnershipPreparationFixture(t, func(opts *ownership.Options) {
		prepare := opts.PrepareKnownAcquisition
		opts.PrepareKnownAcquisition = func(ctx context.Context, fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
			actual, release, err := prepare(ctx, fed, caller, obj, attrs)
			if err != nil {
				return nil, nil, err
			}
			close(entered)
			<-proceed
			return actual, release, nil
		}
	})
	ctx := context.Background()
	transportationOK(t, owners.UnconditionalDivest(ctx, "fed", 1, obj, []core.AttributeHandle{1}))
	commandDone := make(chan error, 1)
	go func() {
		_, err := owners.ExecuteKnownOwnership(ctx, core.OwnershipCommand{
			Operation: core.OwnershipAcquireIfAvailable, Federation: "fed", Federate: 2, Object: obj, Attributes: []core.AttributeHandle{101},
		})
		commandDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("command did not reach mapped preparation")
	}
	st := r.stateFor("fed")
	if st.mu.TryLock() {
		st.mu.Unlock()
		t.Fatal("manager released knowledge between mapping and mutation")
	}
	started, deleted := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		deleted <- r.LocalDelete(ctx, "fed", 2, obj)
	}()
	<-started
	unblock()
	select {
	case err := <-commandDone:
		transportationOK(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("known-coordinate command deadlocked")
	}
	if err := awaitKnownLocalDelete(t, deleted); !errors.Is(err, core.ErrFederateOwnsAttributes) {
		t.Fatalf("local deletion did not see atomic acquisition: %v", err)
	}
	if !owners.IsOwnedBy("fed", 2, obj, 1) || !owners.IsOwnedBy("fed", 1, obj, 2) {
		t.Fatal("command mutated wrong registered attribute")
	}
	if _, cls, known := r.LookupKnownObjectInstance("fed", 2, obj); !known || cls != 9 {
		t.Fatalf("contending deletion changed acquisition coordinates: class=%d known=%t", cls, known)
	}
}
