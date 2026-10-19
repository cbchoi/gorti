package time

import (
	"math"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestPersistentFQRMinimumDetachedAndRestored(t *testing.T) {
	m, _ := persistentTestManager(t)
	t.Cleanup(func() { managerExt.Delete(m) })
	minimum := core.LogicalTime(4)
	state := &ManagerState{StallTimeout: m.opts.StallTimeout, Federates: []FederateState{{
		Handle: 1, HasAdvanceState: true, CurrentTime: 2,
		PendingRequest: true, RequestedTime: 10, Mode: ModeFQR, FlushMinimum: &minimum,
	}}}
	commit, err := m.PrepareImportState("fed", state, clonePersistentTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	minimum = 9
	commit()
	commit()
	got, err := m.ExportState("fed", clonePersistentTestEvent)
	if err != nil || got == nil || got.Federates[0].FlushMinimum == nil || *got.Federates[0].FlushMinimum != 4 {
		t.Fatalf("FQR prepare retained alias: %+v, %v", got, err)
	}
	*got.Federates[0].FlushMinimum = 8
	again, err := m.ExportState("fed", clonePersistentTestEvent)
	if err != nil || *again.Federates[0].FlushMinimum != 4 {
		t.Fatal("FQR export retained alias")
	}
	if err := m.ImportState("fed", nil, clonePersistentTestEvent); err != nil {
		t.Fatal(err)
	}
	if err := m.ImportState("fed", again, clonePersistentTestEvent); err != nil {
		t.Fatal(err)
	}
	round, err := m.ExportState("fed", clonePersistentTestEvent)
	if err != nil || !reflect.DeepEqual(round, again) {
		t.Fatalf("FQR round trip: %+v, %v", round, err)
	}
}

func TestPersistentFQRMinimumRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   core.LogicalTime
		current core.LogicalTime
		mode    AdvanceMode
		pending bool
	}{
		{"negative", -1, 0, ModeFQR, true},
		{"NaN", core.LogicalTime(math.NaN()), 0, ModeFQR, true},
		{"past", 1, 2, ModeFQR, true},
		{"not FQR", 4, 2, ModeTAR, true},
		{"not pending", 4, 2, ModeNone, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := persistentTestManager(t)
			t.Cleanup(func() { managerExt.Delete(m) })
			entry := FederateState{Handle: 1, HasAdvanceState: true, CurrentTime: tc.current, PendingRequest: tc.pending, Mode: tc.mode, FlushMinimum: &tc.value}
			if tc.pending {
				entry.RequestedTime = 10
			}
			commit, err := m.PrepareImportState("fed", &ManagerState{StallTimeout: m.opts.StallTimeout, Federates: []FederateState{entry}}, clonePersistentTestEvent)
			if err == nil || commit != nil {
				t.Fatal("invalid FQR minimum accepted")
			}
			if got, err := m.ExportState("fed", clonePersistentTestEvent); err != nil || got != nil {
				t.Fatal("failed prepare changed live state")
			}
		})
	}
}

func TestPersistentPendingEnablesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		regulation, constrained bool
		lookahead               core.LogicalTime
	}{
		{"regulation", true, false, 0.5}, {"zero lookahead", true, false, 0}, {"constrained", false, true, 0}, {"both", true, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := persistentTestManager(t)
			t.Cleanup(func() { managerExt.Delete(m) })
			state := &ManagerState{StallTimeout: m.opts.StallTimeout, Federates: []FederateState{{
				Handle: 1, HasRegulationState: true, HasAdvanceState: true, CurrentTime: 7,
				RegulationPending: tc.regulation, RegulationLookahead: tc.lookahead, ConstrainedPending: tc.constrained,
			}}}
			if err := m.ImportState("fed", state, clonePersistentTestEvent); err != nil {
				t.Fatal(err)
			}
			got, err := m.ExportState("fed", clonePersistentTestEvent)
			if err != nil || !reflect.DeepEqual(got, state) {
				t.Fatalf("pending enable lost: %+v %v", got, err)
			}
		})
	}
}

func TestPersistentPendingEnablesRejectsMalformed(t *testing.T) {
	cases := map[string]func(*FederateState){
		"already regulating":        func(f *FederateState) { f.Regulating = true },
		"already constrained":       func(f *FederateState) { f.ConstrainedPending = true; f.Constrained = true },
		"pending advance":           func(f *FederateState) { f.PendingRequest = true; f.RequestedTime = 10; f.Mode = ModeTAR },
		"negative lookahead":        func(f *FederateState) { f.RegulationLookahead = -1 },
		"infinite lookahead":        func(f *FederateState) { f.RegulationLookahead = core.PositiveInfinity },
		"NaN lookahead":             func(f *FederateState) { f.RegulationLookahead = core.LogicalTime(math.NaN()) },
		"no regulation state":       func(f *FederateState) { f.HasRegulationState = false },
		"no advance state":          func(f *FederateState) { f.HasAdvanceState = false },
		"lookahead without request": func(f *FederateState) { f.RegulationPending = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m, _ := persistentTestManager(t)
			t.Cleanup(func() { managerExt.Delete(m) })
			f := FederateState{Handle: 1, HasRegulationState: true, HasAdvanceState: true, RegulationPending: true, RegulationLookahead: 1}
			mutate(&f)
			commit, err := m.PrepareImportState("fed", &ManagerState{StallTimeout: m.opts.StallTimeout, Federates: []FederateState{f}}, clonePersistentTestEvent)
			if err == nil || commit != nil {
				t.Fatal("malformed pending enable accepted")
			}
		})
	}
}

func TestPersistentReceiverHistoryDetached(t *testing.T) {
	m, _ := persistentTestManager(t)
	t.Cleanup(func() { managerExt.Delete(m) })
	received := core.LogicalTime(3)
	state := &ManagerState{StallTimeout: m.opts.StallTimeout, Federates: []FederateState{{
		Handle: 1, HasRegulationState: true, Constrained: true, HasAdvanceState: true,
		CurrentTime: 3, PreservedReceiveTime: &received, ReceivedTSOTime: &received,
	}}}
	commit, err := m.PrepareImportState("fed", state, clonePersistentTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	received = 9
	commit()
	got, err := m.ExportState("fed", clonePersistentTestEvent)
	if err != nil || got.Federates[0].PreservedReceiveTime == nil || *got.Federates[0].PreservedReceiveTime != 3 || got.Federates[0].ReceivedTSOTime == nil || *got.Federates[0].ReceivedTSOTime != 3 {
		t.Fatalf("receiver history aliases input: %+v %v", got, err)
	}
	*got.Federates[0].PreservedReceiveTime = 10
	if *got.Federates[0].ReceivedTSOTime != 3 {
		t.Fatal("independent receiver histories share mutable memory")
	}
	*got.Federates[0].ReceivedTSOTime = 11
	again, err := m.ExportState("fed", clonePersistentTestEvent)
	if err != nil || *again.Federates[0].PreservedReceiveTime != 3 || *again.Federates[0].ReceivedTSOTime != 3 {
		t.Fatal("receiver history aliases live state")
	}
}
