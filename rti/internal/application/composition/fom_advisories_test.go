package composition

import (
	"context"
	"fmt"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func advisoryModule(body string) core.FOMModule {
	return core.FOMModule{Path: "switches.xml", XML: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><switches>` + body + `</switches></objectModel>`)}
}

func TestFOMAdvisoryDefaultsPreservePresence(t *testing.T) {
	h, err := NewFOMRepository().Load(context.Background(), []core.FOMModule{advisoryModule(`<objectClassRelevanceAdvisory isEnabled="true"/><attributeRelevanceAdvisory isEnabled="0"/><attributeScopeAdvisory/>`)})
	if err != nil {
		t.Fatal(err)
	}
	lookup, ok := h.(core.FOMAdvisoryDefaults)
	if !ok {
		t.Fatal("FOM drops initial advisory-switch metadata")
	}
	for _, test := range []struct {
		kind           core.AdvisorySwitchKind
		value, present bool
	}{{1, true, true}, {2, false, true}, {3, false, true}, {4, false, false}, {99, false, false}} {
		value, present := lookup.AdvisorySwitchDefault(test.kind)
		if value != test.value || present != test.present {
			t.Errorf("switch %d = %v,%v; want %v,%v", test.kind, value, present, test.value, test.present)
		}
	}
	h, err = NewFOMRepository().Load(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := h.(core.FOMAdvisoryDefaults).AdvisorySwitchDefault(1); present {
		t.Fatal("empty FOM invented default")
	}
}

func TestFOMAdvisoryInvalidOrConflictingDeclarationsRejected(t *testing.T) {
	for _, value := range []string{"yes", "TRUE", "2", ""} {
		t.Run(value, func(t *testing.T) {
			_, err := NewFOMRepository().Load(context.Background(), []core.FOMModule{advisoryModule(fmt.Sprintf(`<attributeScopeAdvisory isEnabled="%s"/>`, value))})
			if err == nil {
				t.Fatal("invalid XML boolean accepted")
			}
		})
	}
	_, err := NewFOMRepository().Load(context.Background(), []core.FOMModule{advisoryModule(`<attributeScopeAdvisory isEnabled="true"/>`), advisoryModule(`<attributeScopeAdvisory isEnabled="false"/>`)})
	if err == nil {
		t.Fatal("conflicting switch values accepted")
	}
}

func TestFOMClassAncestryQualifiedAndDefensive(t *testing.T) {
	h, err := NewFOMRepository().Load(context.Background(), []core.FOMModule{{Path: "hierarchy.xml", XML: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><objectClass><name>Car</name></objectClass></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name><interactionClass><name>Alert</name></interactionClass></interactionClass></interactionClass></interactions></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	lookup, ok := h.(interface {
		ObjectClassAncestors(core.ObjectClassHandle) []core.ObjectClassHandle
		InteractionClassAncestors(core.InteractionClassHandle) []core.InteractionClassHandle
	})
	if !ok {
		t.Fatal("FOM ancestry lookup unavailable")
	}
	car, _ := h.LookupObjectClass("HLAobjectRoot.Vehicle.Car")
	vehicle, _ := h.LookupObjectClass("Vehicle")
	root, _ := h.LookupObjectClass("HLAobjectRoot")
	got := lookup.ObjectClassAncestors(car)
	if fmt.Sprint(got) != fmt.Sprint([]core.ObjectClassHandle{car, vehicle, root}) {
		t.Fatalf("ancestry %v", got)
	}
	got[0] = 999
	if lookup.ObjectClassAncestors(car)[0] != car {
		t.Fatal("caller mutated ancestry")
	}
	alert, _ := h.LookupInteractionClass("HLAinteractionRoot.Signal.Alert")
	signal, _ := h.LookupInteractionClass("Signal")
	interactionRoot, _ := h.LookupInteractionClass("HLAinteractionRoot")
	if got := lookup.InteractionClassAncestors(alert); fmt.Sprint(got) != fmt.Sprint([]core.InteractionClassHandle{alert, signal, interactionRoot}) {
		t.Fatalf("interaction ancestry %v", got)
	}
	if len(lookup.ObjectClassAncestors(0)) != 0 || len(lookup.InteractionClassAncestors(999999)) != 0 {
		t.Fatal("unknown class has ancestry")
	}
}
