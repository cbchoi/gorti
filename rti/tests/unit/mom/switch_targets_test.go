package mom_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

const (
	switchSender    core.FederateHandle  = 7
	switchTarget    core.FederateHandle  = 0x8122334481020304
	switchBystander core.FederateHandle  = 0x81020304 // low-32-bit truncation decoy
	targetParam     core.ParameterHandle = 91
)

// The official MIM declares HLAfederate on the ancestor interaction class.
// This lookup models its resolution on each adjustment subclass, separately
// from that subclass's own parameters. Literal vectors use the MIM's
// HLAvariableArray framing around the SDK's eight-byte encoded handle.
type targetedSwitchFOM struct {
	switchFOM
	unresolved bool
}

func (f targetedSwitchFOM) LookupParameter(cls core.InteractionClassHandle, name string) (core.ParameterHandle, bool) {
	if name == "HLAfederate" {
		return targetParam, !f.unresolved
	}
	return f.switchFOM.LookupParameter(cls, name)
}

type switchValues [4]bool

func readSwitchValues(m *mom.Manager, h core.FederateHandle) switchValues {
	return switchValues{
		m.ConveyRegionDesignatorSetsSwitch("switches", h),
		m.ConveyProducingFederateSwitch("switches", h),
		m.ServiceReporting("switches", h),
		m.ExceptionReporting("switches", h),
	}
}

func writeSwitchValues(m *mom.Manager, h core.FederateHandle, v switchValues) {
	m.SetConveyRegionDesignatorSetsSwitch("switches", h, v[0])
	m.SetConveyProducingFederateSwitch("switches", h, v[1])
	m.SetServiceReporting("switches", h, v[2])
	m.SetExceptionReporting("switches", h, v[3])
}

func newTargetedSwitchManager(t *testing.T) (*mom.Manager, *mom.Dispatcher) {
	t.Helper()
	m, d := newSwitchManager(t)
	for _, h := range []core.FederateHandle{switchTarget, switchBystander, 8} {
		if err := m.FederateJoined(context.Background(), "switches", h, "target", "test"); err != nil {
			t.Fatal(err)
		}
	}
	return m, d
}

var targetedSwitchCases = []struct {
	name   string
	class  string
	params []core.ParameterHandle
	fields []int
}{
	{"regions", mom.ClassFederateSetSwitches, []core.ParameterHandle{1}, []int{0}},
	{"producer", mom.ClassFederateSetSwitches, []core.ParameterHandle{2}, []int{1}},
	{"both", mom.ClassFederateSetSwitches, []core.ParameterHandle{1, 2}, []int{0, 1}},
	{"serviceReporting", mom.ClassSetServiceReporting, []core.ParameterHandle{1}, []int{2}},
	{"exceptionReporting", mom.ClassSetExceptionReporting, []core.ParameterHandle{1}, []int{3}},
}

func TestSwitchAdjustmentsUseInheritedTarget(t *testing.T) {
	for _, tc := range targetedSwitchCases {
		t.Run(tc.name, func(t *testing.T) {
			m, d := newTargetedSwitchManager(t)
			for _, value := range []byte{1, 0} {
				initial := switchValues{value == 0, value == 0, value == 0, value == 0}
				for _, h := range []core.FederateHandle{switchSender, switchTarget, switchBystander, 8} {
					writeSwitchValues(m, h, initial)
				}
				params := map[core.ParameterHandle][]byte{targetParam: {0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}}
				for _, p := range tc.params {
					params[p] = []byte{0, 0, 0, value}
				}
				if err := d.Dispatch(context.Background(), "switches", tc.class, switchSender, params, targetedSwitchFOM{}, nil); err != nil {
					t.Fatal(err)
				}
				want := initial
				for _, field := range tc.fields {
					want[field] = value == 1
				}
				if got := readSwitchValues(m, switchTarget); got != want {
					t.Errorf("target switches = %v, want %v", got, want)
				}
				for _, h := range []core.FederateHandle{switchSender, switchBystander, 8} {
					if got := readSwitchValues(m, h); got != initial {
						t.Errorf("non-target %d changed: %v, want %v", h, got, initial)
					}
				}
			}
		})
	}
}

func TestSwitchTargetFramedUint64Boundaries(t *testing.T) {
	for _, vector := range []struct {
		name   string
		target core.FederateHandle
		low    core.FederateHandle
		bytes  []byte
	}{
		{"small", 8, 8, []byte{0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 8}},
		{"aboveUint32", 0x100000001, 1, []byte{0, 0, 0, 8, 0, 0, 0, 1, 0, 0, 0, 1}},
		{"maxUint64", 0xffffffffffffffff, 0xffffffff, []byte{0, 0, 0, 8, 255, 255, 255, 255, 255, 255, 255, 255}},
	} {
		for _, tc := range targetedSwitchCases {
			t.Run(vector.name+"/"+tc.name, func(t *testing.T) {
				m, d := newTargetedSwitchManager(t)
				for _, h := range []core.FederateHandle{vector.target, vector.low} {
					if err := m.FederateJoined(context.Background(), "switches", h, "target", "test"); err != nil {
						t.Fatal(err)
					}
				}
				params := map[core.ParameterHandle][]byte{targetParam: vector.bytes}
				want := switchValues{}
				for _, p := range tc.params {
					params[p] = []byte{0, 0, 0, 1}
				}
				for _, field := range tc.fields {
					want[field] = true
				}
				if err := d.Dispatch(context.Background(), "switches", tc.class, switchSender, params, targetedSwitchFOM{}, nil); err != nil {
					t.Fatal(err)
				}
				if got := readSwitchValues(m, vector.target); got != want {
					t.Errorf("target %x switches = %v, want %v", vector.target, got, want)
				}
				for _, h := range []core.FederateHandle{switchSender, vector.low, 8} {
					if h != vector.target && readSwitchValues(m, h) != (switchValues{}) {
						t.Errorf("adjustment changed decoy %x", h)
					}
				}
			})
		}
	}
}

func TestSwitchAdjustmentsRejectInvalidTargetWithoutMutation(t *testing.T) {
	invalidTargets := []struct {
		name       string
		bytes      []byte
		omit       bool
		unresolved bool
		resigned   bool
	}{
		{name: "missing", omit: true},
		{name: "nil"},
		{name: "empty", bytes: []byte{}},
		{name: "shortHeader", bytes: []byte{0, 0, 8}},
		{name: "headerOnly", bytes: []byte{0, 0, 0, 8}},
		{name: "legacyRaw4", bytes: []byte{0x81, 2, 3, 4}},
		{name: "unframedSDKHandle", bytes: []byte{0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}},
		{name: "shortPayload", bytes: []byte{0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3}},
		{name: "trailingByte", bytes: []byte{0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4, 0}},
		{name: "emptyArray", bytes: []byte{0, 0, 0, 0}},
		{name: "wrongCount", bytes: []byte{0, 0, 0, 7, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}},
		{name: "littleEndianCount", bytes: []byte{8, 0, 0, 0, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}},
		{name: "oversizedCount", bytes: []byte{255, 255, 255, 255, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}},
		{name: "nineByteOverflow", bytes: []byte{0, 0, 0, 9, 1, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}},
		{name: "framedFourBytePayload", bytes: []byte{0, 0, 0, 4, 0x81, 2, 3, 4}},
		{name: "zero", bytes: []byte{0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 0}},
		{name: "unknown", bytes: []byte{0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 99}},
		{name: "otherFederation", bytes: []byte{0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 98}},
		{name: "resigned", bytes: []byte{0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}, resigned: true},
		{name: "unresolvedParameter", bytes: []byte{0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}, unresolved: true},
	}
	for _, tc := range targetedSwitchCases {
		for _, invalid := range invalidTargets {
			t.Run(tc.name+"/"+invalid.name, func(t *testing.T) {
				m, d := newTargetedSwitchManager(t)
				if err := m.FederateJoined(context.Background(), "other", 98, "other", "test"); err != nil {
					t.Fatal(err)
				}
				if invalid.resigned {
					if err := m.FederateResigned(context.Background(), "switches", switchTarget); err != nil {
						t.Fatal(err)
					}
				}
				params := make(map[core.ParameterHandle][]byte)
				for _, p := range tc.params {
					params[p] = []byte{0, 0, 0, 1}
				}
				if !invalid.omit {
					params[targetParam] = invalid.bytes
				}
				err := d.Dispatch(context.Background(), "switches", tc.class, switchSender, params, targetedSwitchFOM{unresolved: invalid.unresolved}, nil)
				// This checks the existing dispatcher error channel, not an IEEE
				// exception identity or the still-open MOM failure-report protocol.
				if err == nil || !strings.Contains(err.Error(), "HLAfederate") {
					t.Errorf("invalid target error = %v, want HLAfederate diagnostic", err)
				}
				for _, h := range []core.FederateHandle{switchSender, switchTarget, switchBystander, 8, 98, 99} {
					if got := readSwitchValues(m, h); got != (switchValues{}) {
						t.Errorf("invalid target changed %d: %v", h, got)
					}
				}
				if m.ServiceReporting("other", 98) || m.ExceptionReporting("other", 98) ||
					m.ConveyRegionDesignatorSetsSwitch("other", 98) || m.ConveyProducingFederateSwitch("other", 98) {
					t.Error("invalid target changed another federation")
				}
				for _, h := range []core.FederateHandle{98, 99} {
					if _, exists := m.QueryFederateAttributes("switches", h); exists {
						t.Errorf("invalid target created federate %d", h)
					}
				}
				if m.AutoProvideSwitch("switches") {
					t.Error("per-federate adjustment changed federation switch")
				}
			})
		}
	}
}

func TestTargetedSwitchesValidateAllValuesBeforeMutation(t *testing.T) {
	for _, tc := range targetedSwitchCases {
		for _, malformed := range [][]byte{nil, {}, {1}, {0, 0, 0, 2}, {0, 0, 0, 0, 1}} {
			t.Run(tc.name, func(t *testing.T) {
				m, d := newTargetedSwitchManager(t)
				initial := switchValues{true, true, true, true}
				writeSwitchValues(m, switchTarget, initial)
				params := map[core.ParameterHandle][]byte{targetParam: {0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}}
				for _, p := range tc.params {
					params[p] = []byte{0, 0, 0, 0}
				}
				params[tc.params[len(tc.params)-1]] = malformed
				if err := d.Dispatch(context.Background(), "switches", tc.class, switchSender, params, targetedSwitchFOM{}, nil); err == nil {
					t.Errorf("accepted malformed switch %v", malformed)
				}
				if got := readSwitchValues(m, switchTarget); got != initial {
					t.Errorf("invalid switch partially changed target: %v", got)
				}
				for _, h := range []core.FederateHandle{switchSender, switchBystander, 8} {
					if got := readSwitchValues(m, h); got != (switchValues{}) {
						t.Errorf("invalid switch changed non-target %d: %v", h, got)
					}
				}
			})
		}
	}
}

func TestTargetedSwitchesOmittedValuesLeaveStateUnchanged(t *testing.T) {
	for _, class := range []string{mom.ClassFederateSetSwitches, mom.ClassSetServiceReporting, mom.ClassSetExceptionReporting} {
		t.Run(class, func(t *testing.T) {
			m, d := newTargetedSwitchManager(t)
			initial := switchValues{true, true, true, true}
			writeSwitchValues(m, switchTarget, initial)
			if err := d.Dispatch(context.Background(), "switches", class, switchSender,
				map[core.ParameterHandle][]byte{targetParam: {0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4}}, targetedSwitchFOM{}, nil); err != nil {
				t.Fatal(err)
			}
			if got := readSwitchValues(m, switchTarget); got != initial {
				t.Errorf("omitted values changed target: %v", got)
			}
		})
	}
}

func TestTargetedSwitchesConcurrentAdjustmentsAndResign(t *testing.T) {
	m, d := newTargetedSwitchManager(t)
	var workers sync.WaitGroup
	start := make(chan struct{})
	for _, class := range []string{mom.ClassFederateSetSwitches, mom.ClassSetServiceReporting, mom.ClassSetExceptionReporting} {
		workers.Add(1)
		go func(class string) {
			defer workers.Done()
			<-start
			for i := 0; i < 100; i++ {
				params := map[core.ParameterHandle][]byte{
					targetParam: {0, 0, 0, 8, 0x81, 0x22, 0x33, 0x44, 0x81, 2, 3, 4},
					1:           {0, 0, 0, byte(i % 2)},
				}
				if class == mom.ClassFederateSetSwitches {
					params[2] = []byte{0, 0, 0, byte(i % 2)}
				}
				err := d.Dispatch(context.Background(), "switches", class, switchSender, params, targetedSwitchFOM{}, nil)
				if err != nil && !strings.Contains(err.Error(), "not joined") {
					t.Errorf("concurrent dispatch: %v", err)
				}
				_ = readSwitchValues(m, switchTarget)
			}
		}(class)
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for i := 0; i < 100; i++ {
			if err := m.FederateResigned(context.Background(), "switches", switchTarget); err != nil {
				t.Errorf("resign: %v", err)
			}
			if err := m.FederateJoined(context.Background(), "switches", switchTarget, "target", "test"); err != nil {
				t.Errorf("join: %v", err)
			}
		}
	}()
	close(start)
	workers.Wait()
	for _, h := range []core.FederateHandle{switchSender, switchBystander, 8} {
		if got := readSwitchValues(m, h); got != (switchValues{}) {
			t.Errorf("concurrent adjustments changed non-target %d: %v", h, got)
		}
	}
	values := readSwitchValues(m, switchTarget)
	if values[0] != values[1] {
		t.Errorf("concurrent adjustment/resign left a partial pair: %v", values)
	}
}
