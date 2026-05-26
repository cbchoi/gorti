// M20.3 — Dispatcher unit tests. Exercises the class-name routing
// and HLAsetSwitches handler against a stubbed FOM lookup.

package mom

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// stubFOMHandle satisfies both core.FOMHandle and core.FOMHandleNameLookup
// with a tiny in-memory table sufficient for the switch handlers.
type stubFOMHandle struct {
	icByName  map[string]core.InteractionClassHandle
	icByH     map[core.InteractionClassHandle]string
	paramByH  map[core.InteractionClassHandle]map[string]core.ParameterHandle
}

func (s *stubFOMHandle) IsValid() bool { return true }
func (s *stubFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 0, false
}
func (s *stubFOMHandle) LookupInteractionClass(name string) (core.InteractionClassHandle, bool) {
	h, ok := s.icByName[name]
	return h, ok
}
func (s *stubFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 0, false
}
func (s *stubFOMHandle) LookupParameter(cls core.InteractionClassHandle, name string) (core.ParameterHandle, bool) {
	if m, ok := s.paramByH[cls]; ok {
		p, ok := m[name]
		return p, ok
	}
	return 0, false
}
func (s *stubFOMHandle) ObjectClassName(core.ObjectClassHandle) (string, bool) { return "", false }
func (s *stubFOMHandle) InteractionClassName(h core.InteractionClassHandle) (string, bool) {
	n, ok := s.icByH[h]
	return n, ok
}
func (s *stubFOMHandle) AttributeName(core.ObjectClassHandle, core.AttributeHandle) (string, bool) {
	return "", false
}
func (s *stubFOMHandle) ParameterName(core.InteractionClassHandle, core.ParameterHandle) (string, bool) {
	return "", false
}
func (s *stubFOMHandle) LookupDimension(string) (core.DimensionHandle, bool) { return 0, false }
func (s *stubFOMHandle) DimensionName(core.DimensionHandle) (string, bool)   { return "", false }
func (s *stubFOMHandle) DimensionUpperBound(core.DimensionHandle) (uint64, bool) {
	return 0, false
}

// buildSwitchFOM returns a stubFOMHandle with HLAsetSwitches
// interaction classes + their declared parameters wired up.
func buildSwitchFOM() *stubFOMHandle {
	const (
		fedSwitchCls    = core.InteractionClassHandle(100)
		perFedSwitchCls = core.InteractionClassHandle(101)
		svcReportCls    = core.InteractionClassHandle(102)
		excReportCls    = core.InteractionClassHandle(103)
	)
	return &stubFOMHandle{
		icByName: map[string]core.InteractionClassHandle{
			ClassFederationSetSwitches: fedSwitchCls,
			ClassFederateSetSwitches:   perFedSwitchCls,
			ClassSetServiceReporting:   svcReportCls,
			ClassSetExceptionReporting: excReportCls,
		},
		icByH: map[core.InteractionClassHandle]string{
			fedSwitchCls:    ClassFederationSetSwitches,
			perFedSwitchCls: ClassFederateSetSwitches,
			svcReportCls:    ClassSetServiceReporting,
			excReportCls:    ClassSetExceptionReporting,
		},
		paramByH: map[core.InteractionClassHandle]map[string]core.ParameterHandle{
			fedSwitchCls: {
				"HLAautoProvide": core.ParameterHandle(1),
			},
			perFedSwitchCls: {
				"HLAconveyRegionDesignatorSets": core.ParameterHandle(1),
				"HLAconveyProducingFederate":    core.ParameterHandle(2),
			},
			svcReportCls: {"HLAreportingState": core.ParameterHandle(1)},
			excReportCls: {"HLAreportingState": core.ParameterHandle(1)},
		},
	}
}

// --- IsManagerClass / Lookup ------------------------------------------------

func TestDispatcher_IsManagerClass(t *testing.T) {
	d := NewDispatcher(nil)
	cases := map[string]bool{
		"HLAmanager.HLAfederation.HLAadjust.HLAsetSwitches": true,
		"HLAmanager":           false, // exact root, no dot
		"HLAobjectRoot.Foo":    false,
		"":                     false,
		"HLAmanagerExtra.Bar":  false, // not the prefix
	}
	for name, want := range cases {
		if got := d.IsManagerClass(name); got != want {
			t.Errorf("IsManagerClass(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestDispatcher_LookupRegisteredHandler(t *testing.T) {
	d := NewDispatcher(nil)
	if _, ok := d.Lookup(ClassFederationSetSwitches); !ok {
		t.Errorf("ClassFederationSetSwitches handler not registered")
	}
	if _, ok := d.Lookup(ClassFederateSetSwitches); !ok {
		t.Errorf("ClassFederateSetSwitches handler not registered")
	}
	if _, ok := d.Lookup("HLAmanager.Unknown"); ok {
		t.Errorf("unknown handler reported as registered")
	}
}

// --- HLAsetSwitches handler behavior ----------------------------------------

func TestHandleFederationSetSwitches_SetsAutoProvide(t *testing.T) {
	mom := newTestMOM(t, "f1")
	// Bring the federation into existence + add a federate.
	mom.FederationCreated(context.Background(), "f1",
		[]core.FOMModule{{Path: "test.xml"}})
	mom.FederateJoined(context.Background(), "f1", 7, "alice", "atype")

	d := NewDispatcher(mom)
	fom := buildSwitchFOM()
	// HLAautoProvide=true → byte 0x01.
	params := map[core.ParameterHandle][]byte{
		1: {0x01},
	}
	if err := d.Dispatch(context.Background(), "f1",
		ClassFederationSetSwitches, 7, params, fom, fom); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got := mom.AutoProvideSwitch("f1"); !got {
		t.Errorf("AutoProvideSwitch = false, want true")
	}
}

func TestHandleFederateSetSwitches_SetsBothSwitches(t *testing.T) {
	mom := newTestMOM(t, "f1")
	mom.FederationCreated(context.Background(), "f1",
		[]core.FOMModule{{Path: "test.xml"}})
	mom.FederateJoined(context.Background(), "f1", 7, "alice", "atype")

	d := NewDispatcher(mom)
	fom := buildSwitchFOM()
	// HLAconveyRegionDesignatorSets=true (param 1), HLAconveyProducingFederate=true (param 2).
	params := map[core.ParameterHandle][]byte{
		1: {0x01},
		2: {0x01},
	}
	if err := d.Dispatch(context.Background(), "f1",
		ClassFederateSetSwitches, 7, params, fom, fom); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !mom.ConveyRegionDesignatorSetsSwitch("f1", 7) {
		t.Errorf("ConveyRegionDesignatorSetsSwitch = false, want true")
	}
	if !mom.ConveyProducingFederateSwitch("f1", 7) {
		t.Errorf("ConveyProducingFederateSwitch = false, want true")
	}
}

func TestHandleFederateSetSwitches_OmittedParamLeavesValueUnchanged(t *testing.T) {
	mom := newTestMOM(t, "f1")
	mom.FederationCreated(context.Background(), "f1",
		[]core.FOMModule{{Path: "test.xml"}})
	mom.FederateJoined(context.Background(), "f1", 7, "alice", "atype")
	mom.SetConveyProducingFederateSwitch("f1", 7, true) // prior state

	d := NewDispatcher(mom)
	fom := buildSwitchFOM()
	// Send only HLAconveyRegionDesignatorSets, leave HLAconveyProducingFederate omitted.
	params := map[core.ParameterHandle][]byte{
		1: {0x01},
	}
	if err := d.Dispatch(context.Background(), "f1",
		ClassFederateSetSwitches, 7, params, fom, fom); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !mom.ConveyRegionDesignatorSetsSwitch("f1", 7) {
		t.Errorf("ConveyRegionDesignatorSetsSwitch should now be true")
	}
	if !mom.ConveyProducingFederateSwitch("f1", 7) {
		t.Errorf("ConveyProducingFederateSwitch should stay true, omitted param mustn't reset")
	}
}

func TestHandleFederationSetSwitches_InvalidHLAswitchByteFails(t *testing.T) {
	mom := newTestMOM(t, "f1")
	mom.FederationCreated(context.Background(), "f1",
		[]core.FOMModule{{Path: "test.xml"}})
	mom.FederateJoined(context.Background(), "f1", 7, "alice", "atype")

	d := NewDispatcher(mom)
	fom := buildSwitchFOM()
	params := map[core.ParameterHandle][]byte{
		1: {0x99}, // not 0 or 1
	}
	err := d.Dispatch(context.Background(), "f1",
		ClassFederationSetSwitches, 7, params, fom, fom)
	if err == nil {
		t.Errorf("Dispatch with invalid HLAswitch byte should fail")
	}
}

// --- HLAsetServiceReporting + HLAsetExceptionReporting (M20.4) -------------

func TestHandleSetServiceReporting_Enables(t *testing.T) {
	mom := newTestMOM(t, "f1")
	mom.FederationCreated(context.Background(), "f1",
		[]core.FOMModule{{Path: "test.xml"}})
	mom.FederateJoined(context.Background(), "f1", 7, "alice", "atype")

	d := NewDispatcher(mom)
	fom := buildSwitchFOM()
	params := map[core.ParameterHandle][]byte{1: {0x01}}
	if err := d.Dispatch(context.Background(), "f1",
		ClassSetServiceReporting, 7, params, fom, fom); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !mom.ServiceReporting("f1", 7) {
		t.Errorf("ServiceReporting = false, want true")
	}
	if mom.ExceptionReporting("f1", 7) {
		t.Errorf("ExceptionReporting toggled by ServiceReporting interaction")
	}
}

func TestHandleSetExceptionReporting_Toggle(t *testing.T) {
	mom := newTestMOM(t, "f1")
	mom.FederationCreated(context.Background(), "f1",
		[]core.FOMModule{{Path: "test.xml"}})
	mom.FederateJoined(context.Background(), "f1", 7, "alice", "atype")
	mom.SetExceptionReporting("f1", 7, true) // prior state

	d := NewDispatcher(mom)
	fom := buildSwitchFOM()
	params := map[core.ParameterHandle][]byte{1: {0x00}}
	if err := d.Dispatch(context.Background(), "f1",
		ClassSetExceptionReporting, 7, params, fom, fom); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if mom.ExceptionReporting("f1", 7) {
		t.Errorf("ExceptionReporting should now be false")
	}
}

func TestHandleSetServiceReporting_InvalidByteFails(t *testing.T) {
	mom := newTestMOM(t, "f1")
	mom.FederationCreated(context.Background(), "f1",
		[]core.FOMModule{{Path: "test.xml"}})
	mom.FederateJoined(context.Background(), "f1", 7, "alice", "atype")

	d := NewDispatcher(mom)
	fom := buildSwitchFOM()
	params := map[core.ParameterHandle][]byte{1: {0x99}}
	if err := d.Dispatch(context.Background(), "f1",
		ClassSetServiceReporting, 7, params, fom, fom); err == nil {
		t.Errorf("invalid HLAswitch byte: want error, got nil")
	}
}

// --- Unknown class is a no-op ----------------------------------------------

func TestDispatch_UnknownHLAmanagerClassIsNoOp(t *testing.T) {
	d := NewDispatcher(nil)
	err := d.Dispatch(context.Background(), "f1",
		"HLAmanager.HLAfederation.HLAreport.HLAreportSomethingUnknown",
		1, nil, nil, nil)
	if err != nil {
		t.Errorf("unknown management class: want nil, got %v", err)
	}
}

func TestDispatch_NonManagerClassIsNoOp(t *testing.T) {
	d := NewDispatcher(nil)
	err := d.Dispatch(context.Background(), "f1",
		"HLAinteractionRoot.Honk", 1, nil, nil, nil)
	if err != nil {
		t.Errorf("non-manager class: want nil, got %v", err)
	}
}

// --- Helper ----------------------------------------------------------------

// newTestMOM builds a Manager with a no-op Outbox so handler tests can
// exercise state mutations without spinning up a real federate stream.
func newTestMOM(t *testing.T, _ core.FederationName) *Manager {
	t.Helper()
	m, err := New(Options{Outbox: noopOutbox{}})
	if err != nil {
		t.Fatalf("mom.New: %v", err)
	}
	return m
}

type noopOutbox struct{}

func (noopOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	return nil
}
