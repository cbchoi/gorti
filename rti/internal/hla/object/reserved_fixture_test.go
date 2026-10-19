package object

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Reserve through the real store so fixture setup does not add a reservation
// callback to tests that measure only the operation under examination.
func reserveFixtureName[T core.LogicalTimeScalar](t testing.TB, r *RegistryOf[T], fed core.FederationName, holder core.FederateHandle, name string) {
	t.Helper()
	ok, err := r.reservations.Reserve(fed, holder, name)
	if err != nil || !ok {
		t.Fatalf("fixture reservation %q: success=%v error=%v", name, ok, err)
	}
}
