package ddm_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

const (
	testFed   core.FederationName    = "registration"
	testOwner core.FederateHandle    = 1
	testClass core.ObjectClassHandle = 1
)

type testOutbox struct{ t *testing.T }

func (o testOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	o.t.Error("DDM preparation must not deliver callbacks")
	return nil
}

type testRepository struct {
	handle core.FOMHandle
	err    error
}

func (r testRepository) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return r.handle, r.err
}

func (r testRepository) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return r.handle, r.err
}

type minimalFOM struct{}

func (minimalFOM) IsValid() bool                                                     { return true }
func (minimalFOM) LookupObjectClass(string) (core.ObjectClassHandle, bool)           { return testClass, true }
func (minimalFOM) LookupInteractionClass(string) (core.InteractionClassHandle, bool) { return 1, true }
func (minimalFOM) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (minimalFOM) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

type dimensionFOM struct {
	minimalFOM
	dimensions []model.Dimension
	attributes map[core.AttributeHandle][]core.DDMDimensionHandle
}

func (h dimensionFOM) Dimensions() []model.Dimension { return slices.Clone(h.dimensions) }
func (h dimensionFOM) ObjectClassName(cls core.ObjectClassHandle) (string, bool) {
	return "Vehicle", cls == testClass
}
func (h dimensionFOM) AttributeDimensions(cls core.ObjectClassHandle, attr core.AttributeHandle) ([]core.DDMDimensionHandle, bool) {
	dims, ok := h.attributes[attr]
	return slices.Clone(dims), cls == testClass && ok
}

var _ core.FOMAttributeDimensions = dimensionFOM{}

func registrationFOM() dimensionFOM {
	return dimensionFOM{
		dimensions: []model.Dimension{{Name: "X", UpperBound: 100}, {Name: "Y", UpperBound: 200}},
		attributes: map[core.AttributeHandle][]core.DDMDimensionHandle{1: {1}, 2: {2}, 3: {1, 2}, 4: {}},
	}
}

func newManager(t *testing.T, h core.FOMHandle) *ddm.Manager {
	t.Helper()
	m, err := ddm.New(ddm.Options{Outbox: testOutbox{t}, FOMs: testRepository{handle: h}})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func newRegion(t *testing.T, m *ddm.Manager, owner core.FederateHandle, dims ...core.DDMDimensionHandle) core.DDMRegionHandleCore {
	t.Helper()
	space, ok := m.LookupRoutingSpace(testFed, ddm.DefaultRoutingSpace)
	if !ok {
		t.Fatal("missing default routing space")
	}
	rh, err := m.CreateRegion(context.Background(), testFed, owner, space, dims)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range m.ExportState(testFed).Regions {
		if entry.Handle != rh {
			continue
		}
		for _, bounds := range entry.Committed {
			if err := m.SetRangeBounds(testFed, owner, rh, bounds.Dimension, core.DDMRange{Lower: bounds.Lower, Upper: bounds.Upper}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := m.CommitRegionModifications(context.Background(), testFed, owner, []core.DDMRegionHandleCore{rh}); err != nil {
		t.Fatal(err)
	}
	return rh
}

func registrar(t *testing.T, m *ddm.Manager) core.ObjectRegionRegistrar {
	t.Helper()
	r, ok := any(m).(core.ObjectRegionRegistrar)
	if !ok {
		t.Fatal("DDM manager does not implement core.ObjectRegionRegistrar")
	}
	return r
}

func prepare(t *testing.T, m *ddm.Manager, bindings map[core.AttributeHandle][]core.DDMRegionHandleCore) core.ObjectRegionRegistration {
	t.Helper()
	tx, err := registrar(t, m).PrepareObjectRegistration(context.Background(), testFed, testOwner, testClass, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if tx == nil {
		t.Fatal("successful preparation returned nil")
	}
	t.Cleanup(tx.Release)
	return tx
}

func TestPrepareObjectRegistrationCommitCopiesBindings(t *testing.T) {
	m := newManager(t, registrationFOM())
	x := newRegion(t, m, testOwner, 1)
	y := newRegion(t, m, testOwner, 2)
	bindings := map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {x}, 2: {y}}
	tx := prepare(t, m, bindings)
	bindings[1][0] = 999
	delete(bindings, 2)
	bindings[99] = []core.DDMRegionHandleCore{x}
	tx.Commit(42)
	tx.Release()
	if got := m.PublisherRegionsFor(testFed, 42, 1); !slices.Equal(got, []core.DDMRegionHandleCore{x}) {
		t.Fatalf("attribute 1 = %v", got)
	}
	if got := m.PublisherRegionsFor(testFed, 42, 2); !slices.Equal(got, []core.DDMRegionHandleCore{y}) {
		t.Fatalf("attribute 2 = %v", got)
	}
	if got := m.PublisherRegionsFor(testFed, 42, 99); len(got) != 0 {
		t.Fatalf("caller-added attribute leaked: %v", got)
	}
	if err := m.DeleteRegion(context.Background(), testFed, testOwner, x); !errors.Is(err, core.ErrRegionInUse) {
		t.Fatalf("delete committed region = %v", err)
	}
}

func TestPrepareObjectRegistrationReleaseHasNoMutations(t *testing.T) {
	m := newManager(t, registrationFOM())
	rh := newRegion(t, m, testOwner, 1)
	if err := m.SetRangeBounds(testFed, testOwner, rh, 1, core.DDMRange{Lower: 10, Upper: 20}); err != nil {
		t.Fatal(err)
	}
	if err := m.AssociateRegionsForUpdates(context.Background(), testFed, testOwner, 7, map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {rh}}); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState(testFed)
	tx := prepare(t, m, map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {rh}})
	tx.Release()
	tx.Release()
	if after := m.ExportState(testFed); !reflect.DeepEqual(before, after) {
		t.Fatalf("release mutated state: before=%+v after=%+v", before, after)
	}
}

func TestPrepareObjectRegistrationRejectsInvalidBindingsAtomically(t *testing.T) {
	for _, tc := range []struct {
		name        string
		class       core.ObjectClassHandle
		attr        core.AttributeHandle
		regionOwner core.FederateHandle
		dims        []core.DDMDimensionHandle
		missing     bool
		want        error
	}{
		{"missing_region", testClass, 1, testOwner, []core.DDMDimensionHandle{1}, true, core.ErrRegionNotFound},
		{"foreign_owner", testClass, 1, 2, []core.DDMDimensionHandle{1}, false, core.ErrRegionNotOwnedByFederate},
		{"unknown_class", 99, 1, testOwner, []core.DDMDimensionHandle{1}, false, core.ErrObjectClassNotFound},
		{"unknown_attribute", testClass, 99, testOwner, []core.DDMDimensionHandle{1}, false, core.ErrAttributeNotFound},
		{"different_dimension", testClass, 1, testOwner, []core.DDMDimensionHandle{2}, false, core.ErrInvalidRegionContext},
		{"extra_dimension", testClass, 1, testOwner, []core.DDMDimensionHandle{1, 2}, false, core.ErrInvalidRegionContext},
		{"empty_attribute_context", testClass, 4, testOwner, []core.DDMDimensionHandle{1}, false, core.ErrInvalidRegionContext},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newManager(t, registrationFOM())
			valid := newRegion(t, m, testOwner, 2)
			rh := newRegion(t, m, tc.regionOwner, tc.dims...)
			if tc.missing {
				rh = 999
			}
			before := m.ExportState(testFed)
			tx, err := registrar(t, m).PrepareObjectRegistration(context.Background(), testFed, testOwner, tc.class, map[core.AttributeHandle][]core.DDMRegionHandleCore{2: {valid}, tc.attr: {rh}})
			if tx != nil {
				tx.Release()
				t.Error("failed preparation returned a transaction")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Prepare = %v, want %v", err, tc.want)
			}
			if after := m.ExportState(testFed); !reflect.DeepEqual(before, after) {
				t.Fatal("rejected preparation mutated state")
			}
			good := prepare(t, m, map[core.AttributeHandle][]core.DDMRegionHandleCore{2: {valid}})
			good.Release()
		})
	}
}

func TestPrepareObjectRegistrationDimensionSets(t *testing.T) {
	for _, tc := range []struct {
		name string
		attr core.AttributeHandle
		dims []core.DDMDimensionHandle
	}{
		{"order_independent", 3, []core.DDMDimensionHandle{2, 1}},
		{"duplicates_are_a_set", 1, []core.DDMDimensionHandle{1, 1}},
		// IEEE 1516.1-2010 9.5.3(j), PDF p232: subset, not equality.
		{"proper_subset_x", 3, []core.DDMDimensionHandle{1}},
		{"proper_subset_y", 3, []core.DDMDimensionHandle{2}},
		{"empty_region_context", 1, nil},
		{"both_empty", 4, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newManager(t, registrationFOM())
			rh := newRegion(t, m, testOwner, tc.dims...)
			before := m.ExportState(testFed)
			if tc.attr == 4 {
				// DDM is unavailable when the attribute has no available dimensions.
				tx, err := registrar(t, m).PrepareObjectRegistration(context.Background(), testFed, testOwner, testClass, map[core.AttributeHandle][]core.DDMRegionHandleCore{tc.attr: {rh}})
				if tx != nil {
					tx.Release()
					t.Fatal("no-dimension context returned registration")
				}
				if !errors.Is(err, core.ErrInvalidRegionContext) {
					t.Fatalf("no-dimension context: %v", err)
				}
				if !reflect.DeepEqual(before, m.ExportState(testFed)) {
					t.Fatal("rejected context mutated state")
				}
				return
			}
			tx := prepare(t, m, map[core.AttributeHandle][]core.DDMRegionHandleCore{tc.attr: {rh}})
			tx.Release()
			if !reflect.DeepEqual(before, m.ExportState(testFed)) {
				t.Fatal("uncommitted subset preparation mutated state")
			}
			tx = prepare(t, m, map[core.AttributeHandle][]core.DDMRegionHandleCore{tc.attr: {rh}})
			tx.Commit(42)
			tx.Release()
			if got := m.PublisherRegionsFor(testFed, 42, tc.attr); !slices.Equal(got, []core.DDMRegionHandleCore{rh}) {
				t.Fatalf("regions = %v", got)
			}
		})
	}
}

func TestPrepareObjectRegistrationRequiresDimensionContext(t *testing.T) {
	m := newManager(t, minimalFOM{})
	tx, err := registrar(t, m).PrepareObjectRegistration(context.Background(), testFed, testOwner, testClass, nil)
	if tx != nil {
		tx.Release()
	}
	if !errors.Is(err, core.ErrInvalidRegionContext) {
		t.Fatalf("missing FOM dimension capability = %v", err)
	}
	if m.ExportState(testFed) != nil {
		t.Fatal("rejected preparation created federation state")
	}
}

func TestPrepareObjectRegistrationEmptyReleaseDoesNotCreateState(t *testing.T) {
	m := newManager(t, registrationFOM())
	tx := prepare(t, m, nil)
	tx.Release()
	if m.ExportState(testFed) != nil {
		t.Fatal("uncommitted preparation created federation state")
	}
}

func TestPrepareObjectRegistrationCancellation(t *testing.T) {
	m := newManager(t, registrationFOM())
	rh := newRegion(t, m, testOwner, 1)
	before := m.ExportState(testFed)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx, err := registrar(t, m).PrepareObjectRegistration(ctx, testFed, testOwner, testClass, map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {rh}})
	if tx != nil {
		tx.Release()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Prepare = %v", err)
	}
	if after := m.ExportState(testFed); !reflect.DeepEqual(before, after) {
		t.Fatal("cancelled preparation mutated state")
	}
}

func TestPrepareObjectRegistrationHoldsLockUntilRelease(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "commit"}[commit], func(t *testing.T) {
			m := newManager(t, registrationFOM())
			rh := newRegion(t, m, testOwner, 1)
			tx := prepare(t, m, map[core.AttributeHandle][]core.DDMRegionHandleCore{1: {rh}})
			started, done := make(chan struct{}), make(chan error, 1)
			go func() {
				close(started)
				done <- m.DeleteRegion(context.Background(), testFed, testOwner, rh)
			}()
			<-started
			select {
			case err := <-done:
				t.Fatalf("DeleteRegion passed preparation lock: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			if commit {
				tx.Commit(42)
				select {
				case err := <-done:
					t.Fatalf("Commit unlocked before Release: %v", err)
				case <-time.After(30 * time.Millisecond):
				}
			}
			tx.Release()
			select {
			case err := <-done:
				if commit && !errors.Is(err, core.ErrRegionInUse) {
					t.Fatalf("delete after commit = %v", err)
				}
				if !commit && err != nil {
					t.Fatalf("delete after rollback = %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Release did not unblock DeleteRegion")
			}
		})
	}
}
