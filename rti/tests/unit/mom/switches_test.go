package mom_test

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

type switchFOM struct{ core.FOMHandle }

func (switchFOM) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}

func (switchFOM) LookupParameter(_ core.InteractionClassHandle, name string) (core.ParameterHandle, bool) {
	switch name {
	case "HLAconveyProducingFederate":
		return 2, true
	case "HLAfederate":
		return 3, true
	case "HLAautoProvide", "HLAconveyRegionDesignatorSets", "HLAreportingState":
		return 1, true
	}
	return 0, false
}

type unusedOutbox struct{ core.Outbox }

func newSwitchManager(t *testing.T) (*mom.Manager, *mom.Dispatcher) {
	t.Helper()
	m, err := mom.New(mom.Options{Outbox: unusedOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.FederationCreated(context.Background(), "switches", nil); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateJoined(context.Background(), "switches", 7, "federate", "test"); err != nil {
		t.Fatal(err)
	}
	return m, mom.NewDispatcher(m)
}

func dispatchSwitch(d *mom.Dispatcher, class string, params map[core.ParameterHandle][]byte) error {
	targeted := make(map[core.ParameterHandle][]byte, len(params)+1)
	for h, value := range params {
		targeted[h] = value
	}
	if class != mom.ClassFederationSetSwitches {
		targeted[3] = []byte{0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 7}
	}
	return d.Dispatch(context.Background(), "switches", class, 7, targeted, switchFOM{}, nil)
}

func TestStandardSwitchEncoding(t *testing.T) {
	cases := []struct {
		name  string
		class string
		get   func(*mom.Manager) bool
	}{
		{"autoProvide", mom.ClassFederationSetSwitches, func(m *mom.Manager) bool { return m.AutoProvideSwitch("switches") }},
		{"regions", mom.ClassFederateSetSwitches, func(m *mom.Manager) bool { return m.ConveyRegionDesignatorSetsSwitch("switches", 7) }},
		{"serviceReporting", mom.ClassSetServiceReporting, func(m *mom.Manager) bool { return m.ServiceReporting("switches", 7) }},
		{"exceptionReporting", mom.ClassSetExceptionReporting, func(m *mom.Manager) bool { return m.ExceptionReporting("switches", 7) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, d := newSwitchManager(t)
			// Independent MIM vectors: HLAinteger32BE enumerators 1 and 0.
			for _, v := range []byte{1, 0} {
				if err := dispatchSwitch(d, tc.class, map[core.ParameterHandle][]byte{1: {0, 0, 0, v}}); err != nil {
					t.Fatal(err)
				}
				if got := tc.get(m); got != (v == 1) {
					t.Fatalf("switch = %v, encoded value = %d", got, v)
				}
			}
			if err := dispatchSwitch(d, tc.class, map[core.ParameterHandle][]byte{1: {0, 0, 0, 1}}); err != nil {
				t.Fatal(err)
			}
			if err := dispatchSwitch(d, tc.class, nil); err != nil || !tc.get(m) {
				t.Fatalf("omitted switch changed state: err=%v", err)
			}
			for _, malformed := range [][]byte{nil, {}, {1}, {0, 0, 1}, {1, 0, 0, 0}, {0, 0, 0, 2}, {255, 255, 255, 255}, {0, 0, 0, 0, 1}} {
				if err := dispatchSwitch(d, tc.class, map[core.ParameterHandle][]byte{1: malformed}); err == nil {
					t.Errorf("accepted malformed switch %v", malformed)
				}
				if !tc.get(m) {
					t.Errorf("malformed switch %v changed state", malformed)
				}
			}
		})
	}
}

func TestFederateSwitchesValidateBeforeMutation(t *testing.T) {
	m, d := newSwitchManager(t)
	err := dispatchSwitch(d, mom.ClassFederateSetSwitches, map[core.ParameterHandle][]byte{
		1: {0, 0, 0, 1},
		2: {0, 0, 0, 2},
	})
	if err == nil {
		t.Fatal("accepted invalid second parameter")
	}
	if m.ConveyRegionDesignatorSetsSwitch("switches", 7) || m.ConveyProducingFederateSwitch("switches", 7) {
		t.Fatal("invalid interaction partially changed switches")
	}
	if err := dispatchSwitch(d, mom.ClassFederateSetSwitches, map[core.ParameterHandle][]byte{2: {0, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	if m.ConveyRegionDesignatorSetsSwitch("switches", 7) || !m.ConveyProducingFederateSwitch("switches", 7) {
		t.Fatal("producer-only update changed the wrong switch")
	}
}
