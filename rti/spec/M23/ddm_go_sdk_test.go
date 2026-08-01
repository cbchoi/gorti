// TASK-263 (M23 W4) — Go SDK DDM surface introspection.
//
// Per AC §3.7: the Go SDK gains 10 DDM methods (mirroring pysdk's
// existing surface) so cross-language feature parity is restored.
//
// Pre-M23 the Go SDK had zero DDM coverage; this test pins the
// surface so any future regression that drops a method fails loudly.

package m23spec

import (
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

// Methods that must be present on *federate.Federate after M23 W4.
// These mirror pysdk/rti1516e/ddm.py's 10 public methods.
var ddmMethodNames = []string{
	"LookupRoutingSpace",
	"LookupDimension",
	"CreateRegion",
	"SetRangeBounds",
	"CommitRegionModifications",
	"DeleteRegion",
	"QueryBounds",
	"SubscribeObjectClassAttributesWithRegions",
	"SubscribeInteractionClassWithRegions",
	"RegisterObjectInstanceWithRegions",
}

// TestACDDMGoSDKSurface — AC §3.7. Each method exists with a method
// receiver on *Federate. Reflective check; no behavior asserted here
// (RPC-level smoke is in W5's spec test once the missing services
// land).
func TestACDDMGoSDKSurface(t *testing.T) {
	fedType := reflect.TypeOf((*federate.Federate)(nil))
	for _, name := range ddmMethodNames {
		if _, ok := fedType.MethodByName(name); !ok {
			t.Errorf("Federate.%s missing — M23 W4 incomplete", name)
		}
	}
}

// TestACDDMTypeAdapter — AttributeRegions struct exists with the
// documented fields.
func TestACDDMTypeAdapter(t *testing.T) {
	a := federate.AttributeRegions{
		AttributeHandle: 7,
		RegionHandles:   []uint64{1, 2, 3},
	}
	if a.AttributeHandle != 7 || len(a.RegionHandles) != 3 {
		t.Errorf("AttributeRegions struct fields not as expected: %+v", a)
	}
}
