package parser

import (
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"reflect"
	"testing"
)

func TestExportRetainsObjectClassSharing(t *testing.T) {
	r, err := Parse([]Module{{XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><sharing>PublishSubscribe</sharing></objectClass></objectClass></objects></objectModel>`)}})
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("parse: %v %+v", err, r.Diagnostics)
	}
	for _, c := range r.FOM.(*model.FOM).ObjectClasses() {
		if c.Name != testVehicleName {
			continue
		}
		field := reflect.ValueOf(c).FieldByName("Sharing")
		if !field.IsValid() || field.String() != sharingPublishSubscribe {
			t.Fatal("object class sharing discarded")
		}
		return
	}
	t.Fatal("Vehicle missing")
}

func TestExportRetainsSingularEnumeratorValue(t *testing.T) {
	r, err := Parse([]Module{{XML: []byte(`<objectModel><dataTypes><enumeratedDataTypes><enumeratedData><name>Choice</name><representation>HLAinteger32BE</representation><enumerator><name>One</name><value>17</value></enumerator></enumeratedData></enumeratedDataTypes></dataTypes></objectModel>`)}})
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("parse: %v %+v", err, r.Diagnostics)
	}
	got := r.FOM.(*model.FOM).DataTypes()[0].(*model.EnumeratedData).Enumerators[0].Values
	if got != "17" {
		t.Fatalf("singular enumerator value lost: %q", got)
	}
}

func TestExportRetainsNonAdvisorySwitches(t *testing.T) {
	r, err := Parse([]Module{{XML: []byte(`<objectModel><switches><autoProvide isEnabled="true"/><conveyRegionDesignatorSets isEnabled="true"/><conveyProducingFederate isEnabled="true"/><serviceReporting isEnabled="true"/><exceptionReporting isEnabled="true"/><delaySubscriptionEvaluation isEnabled="true"/></switches></objectModel>`)}})
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("parse: %v %+v", err, r.Diagnostics)
	}
	got := r.FOM.(*model.FOM).AdvisorySwitches()
	if len(got) != 6 {
		t.Fatalf("six explicit switch values lost: %+v", got)
	}
	for _, value := range got {
		if !value.Enabled {
			t.Errorf("switch disabled: %+v", value)
		}
	}
}

const (
	testVehicleName = "Vehicle"
)
