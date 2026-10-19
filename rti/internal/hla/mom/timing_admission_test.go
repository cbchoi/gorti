package mom_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

type periodicGatedRegistry struct {
	core.ObjectRegistry
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *periodicGatedRegistry) UpdateAttributes(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, map[core.AttributeHandle][]byte, *core.LogicalTime) error {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	return nil
}

type periodicFOMRepo struct{ fom core.FOMHandle }

func (r periodicFOMRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return r.fom, nil
}
func (r periodicFOMRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return r.fom, nil
}

func TestMOMPeriodicAdmissionBeforeRestoreLocks(t *testing.T) {
	h, s := newTimedCatalog(t)
	if err := h.request(t, mom.ClassSetTiming, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget)), "HLAreportPeriod": catalogInt(1)}); err != nil {
		t.Fatal(err)
	}
	state := h.m.ExportState(catalogFederation)
	for i := range state.Federates {
		state.Federates[i].ReportPeriod = 0
	}
	commit, err := h.m.PrepareImportState(catalogFederation, state)
	if err != nil {
		t.Fatal(err)
	}
	gate := &periodicGatedRegistry{entered: make(chan struct{}), release: make(chan struct{})}
	h.m.EnableInstanceFanout(gate, h.decl, periodicFOMRepo{h.fom})
	admission := make(chan struct{})
	releaseAdmission := make(chan struct{})
	var once sync.Once
	h.m.SetBackgroundAdmission(func(context.Context, core.FederationName) (func(), error) {
		once.Do(func() { close(admission) })
		<-releaseAdmission
		return func() {}, nil
	})
	tickDone := make(chan struct{})
	go func() { defer close(tickDone); s.call() }()
	defer func() {
		close(releaseAdmission)
		close(gate.release)
		select {
		case <-tickDone:
		case <-time.After(time.Second):
			t.Error("tick cleanup blocked")
		}
	}()
	select {
	case <-admission:
	case <-gate.entered:
		t.Error("tick entered outbox while checkpoint admission was unavailable")
	case <-time.After(time.Second):
		t.Fatal("tick failed to reach admission")
	}
	committed := make(chan struct{})
	go func() { commit(); close(committed) }()
	select {
	case <-committed:
	case <-time.After(time.Second):
		t.Error("restore commit blocked on periodicMu while tick awaited reserved outbox")
	}
}

func TestMOMPeriodicBusyAdmissionSkipsAndRearms(t *testing.T) {
	h, s := newTimedCatalog(t)
	busy := errors.New("checkpoint busy")
	calls := 0
	h.m.SetBackgroundAdmission(func(context.Context, core.FederationName) (func(), error) { calls++; return nil, busy })
	if err := h.request(t, mom.ClassSetTiming, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget)), "HLAreportPeriod": catalogInt(1)}); err != nil {
		t.Fatal(err)
	}
	h.out.sent = nil
	s.call()
	_, err := h.m.PeriodicStatus(catalogFederation, catalogTarget)
	if calls != 1 || !errors.Is(err, busy) || len(h.out.sent) != 0 {
		t.Fatalf("busy tick emitted: calls=%d events=%d err=%v", calls, len(h.out.sent), err)
	}
	s.call()
	if calls != 2 {
		t.Fatal("busy timer not rearmed")
	}
	h.m.SetBackgroundAdmission(func(context.Context, core.FederationName) (func(), error) {
		return func() {}, nil
	})
	s.call()
	if _, err := h.m.PeriodicStatus(catalogFederation, catalogTarget); err != nil || len(h.out.sent) == 0 {
		t.Fatalf("admitted retry did not resume publication: events=%d err=%v", len(h.out.sent), err)
	}
}

func TestMOMPeriodicAdmissionHeldThroughPublication(t *testing.T) {
	h, s := newTimedCatalog(t)
	if err := h.request(t, mom.ClassSetTiming, map[string][]byte{"HLAfederate": catalogHandle(uint64(catalogTarget)), "HLAreportPeriod": catalogInt(1)}); err != nil {
		t.Fatal(err)
	}
	gate := &periodicGatedRegistry{entered: make(chan struct{}), release: make(chan struct{})}
	h.m.EnableInstanceFanout(gate, h.decl, periodicFOMRepo{h.fom})
	admitted := make(chan struct{})
	released := make(chan struct{})
	h.m.SetBackgroundAdmission(func(context.Context, core.FederationName) (func(), error) {
		close(admitted)
		return func() { close(released) }, nil
	})
	tickDone := make(chan struct{})
	go func() { defer close(tickDone); s.call() }()
	defer func() {
		close(gate.release)
		select {
		case <-tickDone:
		case <-time.After(time.Second):
			t.Error("tick cleanup blocked")
		}
		select {
		case <-released:
		default:
			t.Error("admission not released after publication")
		}
	}()
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		t.Fatal("tick did not reach publication")
	}
	select {
	case <-admitted:
	default:
		t.Error("publication started without admission")
	}
	select {
	case <-released:
		t.Error("admission released before publication finished")
	default:
	}
}
