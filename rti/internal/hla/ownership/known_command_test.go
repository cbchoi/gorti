package ownership

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type knownPinStrategy struct {
	defaultNegotiation
	check func()
}

func (s knownPinStrategy) SelectAcquirer(c SelectAcquirerContext) core.FederateHandle {
	s.check()
	return s.defaultNegotiation.SelectAcquirer(c)
}

func TestKnownOwnershipPinsMappingThroughMutationAndReleasesBeforeCallback(t *testing.T) {
	ctx := context.Background()
	var pin sync.Mutex
	preparations, releases, selections := 0, 0, 0
	prepare := func(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
		if !reflect.DeepEqual(attrs, []core.AttributeHandle{10}) {
			t.Fatalf("request was already mapped: %v", attrs)
		}
		pin.Lock()
		preparations++
		return []core.AttributeHandle{20}, func() { releases++; pin.Unlock() }, nil
	}
	var m *Manager
	observing := false
	out := divestObserverOutbox{out: &fakeOutbox{}, observe: func() {
		if !observing {
			return
		}
		if !pin.TryLock() {
			t.Error("callback ran under object pin")
			return
		}
		pin.Unlock()
		if !m.IsOwnedBy("fed", 2, 100, 20) {
			t.Error("callback preceded ownership commit")
		}
	}}
	m, err := New(Options{Outbox: out, PrepareKnownAcquisition: prepare, PrepareKnownOperation: prepare,
		Strategy: knownPinStrategy{check: func() {
			selections++
			if pin.TryLock() {
				pin.Unlock()
				t.Error("mapping released before selection/mutation")
			}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{10, 20})
	// Establish the queued request through the existing registered-coordinate API.
	if err := m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{20}, nil); err != nil {
		t.Fatal(err)
	}
	observing = true
	got, err := m.ExecuteKnownOwnership(ctx, core.OwnershipCommand{Operation: core.OwnershipDivestIfWanted, Federation: "fed", Federate: 1, Object: 100, Attributes: []core.AttributeHandle{10}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []core.AttributeHandle{10}) || !m.IsOwnedBy("fed", 1, 100, 10) || !m.IsOwnedBy("fed", 2, 100, 20) {
		t.Fatalf("wrong result/attribute mutated: result=%v state=%+v", got, m.ExportState("fed"))
	}
	if preparations != 1 || releases != 1 || selections != 1 {
		t.Fatalf("prepare/release/select = %d/%d/%d", preparations, releases, selections)
	}
}

func TestKnownOwnershipRejectsInvalidMappingBeforeMutation(t *testing.T) {
	for _, mapped := range [][]core.AttributeHandle{nil, {20}, {20, 20}} {
		t.Run("invalid", func(t *testing.T) {
			released := 0
			prepare := func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
				return mapped, func() { released++ }, nil
			}
			m, err := New(Options{Outbox: &fakeOutbox{}, PrepareKnownOperation: prepare, PrepareKnownAcquisition: prepare})
			if err != nil {
				t.Fatal(err)
			}
			m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{10, 20})
			before := m.ExportState("fed")
			_, err = m.ExecuteKnownOwnership(context.Background(), core.OwnershipCommand{Operation: core.OwnershipUnconditionalDivest, Federation: "fed", Federate: 1, Object: 100, Attributes: []core.AttributeHandle{10, 11}})
			if err == nil || released != 1 || !reflect.DeepEqual(before, m.ExportState("fed")) {
				t.Fatalf("invalid mapping mutated/leaked: err=%v releases=%d", err, released)
			}
		})
	}
}

func TestKnownReleaseDeniedRemapsAfterAdmissionBeforeCommit(t *testing.T) {
	m, out, _ := denialManager(t)
	var pin sync.Mutex
	mapped := core.AttributeHandle(10)
	prepares, reserves := 0, 0
	prepare := func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
		pin.Lock()
		prepares++
		return []core.AttributeHandle{mapped}, pin.Unlock, nil
	}
	m.opts.PrepareKnownOperation, m.opts.PrepareKnownAcquisition = prepare, prepare
	out.beforeReserve = func() error {
		if !pin.TryLock() {
			return errors.New("reservation held object pin")
		}
		defer pin.Unlock()
		reserves++
		mapped = 20
		return nil
	}
	_, err := m.ExecuteKnownOwnership(context.Background(), core.OwnershipCommand{Operation: core.OwnershipReleaseDenied, Federation: "fed", Federate: 1, Object: 100, Attributes: []core.AttributeHandle{99}})
	if err != nil {
		t.Fatal(err)
	}
	if prepares != 4 || reserves != 2 {
		t.Fatalf("preparations/reservations=%d/%d; want retry", prepares, reserves)
	}
	for _, p := range m.ExportState("fed").PendingAcquires {
		if p.Object == 100 && p.Attribute == 20 {
			t.Error("new mapping's request was not denied")
		}
	}
	for _, event := range out.Sent() {
		cb := event.Event.(*CallbackEvent)
		if !reflect.DeepEqual(cb.Attributes, []core.AttributeHandle{20}) {
			t.Errorf("stale reservation committed: %v", cb.Attributes)
		}
	}
}

func TestKnownOwnershipAllOperationsUsePinnedCoordinates(t *testing.T) {
	ctx := context.Background()
	for op := core.OwnershipUnconditionalDivest; op <= core.OwnershipReleaseDenied; op++ {
		t.Run(fmt.Sprint(op), func(t *testing.T) {
			m, err := New(Options{Outbox: &denialOutbox{}})
			if err != nil {
				t.Fatal(err)
			}
			m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{10, 20})
			actual := []core.AttributeHandle{20}
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			caller := core.FederateHandle(1)
			switch op {
			case core.OwnershipConfirmDivestiture:
				must(m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, actual, nil))
				must(m.Acquire(ctx, "fed", 2, 100, actual, nil))
			case core.OwnershipAcquire, core.OwnershipAcquireIfAvailable:
				must(m.UnconditionalDivest(ctx, "fed", 1, 100, actual))
				caller = 2
			case core.OwnershipCancelDivest:
				must(m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, actual, nil))
			case core.OwnershipCancelAcquire, core.OwnershipDivestIfWanted, core.OwnershipReleaseDenied:
				must(m.Acquire(ctx, "fed", 2, 100, actual, nil))
				if op == core.OwnershipCancelAcquire {
					caller = 2
				}
			}
			acquisitions, operations, released := 0, 0, 0
			prepare := func(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
				if !reflect.DeepEqual(attrs, []core.AttributeHandle{10}) {
					t.Fatalf("not caller coordinates: %v", attrs)
				}
				return []core.AttributeHandle{20}, func() { released++ }, nil
			}
			m.opts.PrepareKnownOperation = func(ctx context.Context, fed core.FederationName, who core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
				operations++
				return prepare(ctx, fed, who, obj, attrs)
			}
			m.opts.PrepareKnownAcquisition = func(ctx context.Context, fed core.FederationName, who core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
				acquisitions++
				return prepare(ctx, fed, who, obj, attrs)
			}
			canonical := func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (func(), error) {
				return nil, errors.New("double canonical preparation")
			}
			m.opts.PrepareAcquisition, m.opts.PrepareDivestiture, m.opts.PrepareReleaseDenied = canonical, canonical, canonical
			result, err := m.ExecuteKnownOwnership(ctx, core.OwnershipCommand{Operation: op, Federation: "fed", Federate: caller, Object: 100, Attributes: []core.AttributeHandle{10}, Tag: []byte("tag")})
			must(err)
			if !m.IsOwnedBy("fed", 1, 100, 10) {
				t.Fatal("numerically colliding attribute changed owner")
			}
			if released != acquisitions+operations || released == 0 {
				t.Fatalf("pins not balanced: %d/%d/%d", acquisitions, operations, released)
			}
			if (acquisitions > 0) != (op == core.OwnershipAcquire || op == core.OwnershipAcquireIfAvailable) {
				t.Fatal("wrong publication precondition port")
			}
			st := m.ExportState("fed")
			switch op {
			case core.OwnershipUnconditionalDivest:
				if m.IsOwnedBy("fed", 1, 100, 20) {
					t.Fatal("attribute not divested")
				}
			case core.OwnershipNegotiatedDivest, core.OwnershipNegotiatedDivestTwoPhase:
				if len(st.PendingDivests) != 1 || st.PendingDivests[0].Attribute != 20 {
					t.Fatalf("wrong pending divest: %+v", st)
				}
			case core.OwnershipConfirmDivestiture, core.OwnershipAcquire, core.OwnershipAcquireIfAvailable, core.OwnershipDivestIfWanted:
				if !m.IsOwnedBy("fed", 2, 100, 20) {
					t.Fatal("attribute not acquired")
				}
				if op == core.OwnershipDivestIfWanted && !reflect.DeepEqual(result, []core.AttributeHandle{10}) {
					t.Fatalf("result not mapped: %v", result)
				}
			case core.OwnershipCancelDivest:
				if len(st.PendingDivests) != 0 {
					t.Fatal("divest not cancelled")
				}
			case core.OwnershipCancelAcquire, core.OwnershipReleaseDenied:
				if len(st.PendingAcquires) != 0 {
					t.Fatal("acquisition not cancelled")
				}
			}
		})
	}
}
