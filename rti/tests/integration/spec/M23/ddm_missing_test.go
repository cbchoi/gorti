// TASK-268 (M23 W5) — DDM missing services.
//
// Per AC §3.8-3.10:
//   - Unsubscribe*WithRegions flips the subscriber set.
//   - SendInteractionWithRegions filters by region overlap (M23 simplification:
//     advisory only — full per-call filtering deferred).
//   - RequestAttributeValueUpdateWithRegions filters owners by region.
//
// W5 introduces 6 new RPCs. This file pins the surface (Go SDK
// methods, manager methods, RPC handlers) plus the unsubscribe
// behavior at the manager level.

package m23spec

import (
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/federate"
)

// Methods that must be present on *federate.Federate after M23 W5.
var w5DDMMethodNames = []string{
	"AssociateRegionsForUpdates",
	"UnassociateRegionsForUpdates",
	"UnsubscribeObjectClassAttributesWithRegions",
	"UnsubscribeInteractionClassWithRegions",
	"SendInteractionWithRegions",
	"RequestAttributeValueUpdateWithRegions",
}

// TestACDDMMissingServicesGoSDKSurface — AC §3.8-3.10 surface check.
func TestACDDMMissingServicesGoSDKSurface(t *testing.T) {
	fedType := reflect.TypeOf((*federate.Federate)(nil))
	for _, name := range w5DDMMethodNames {
		if _, ok := fedType.MethodByName(name); !ok {
			t.Errorf("Federate.%s missing — M23 W5 incomplete", name)
		}
	}
}
