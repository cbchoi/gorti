package federation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

func TestInjectedTimeCapabilityIsRecordedWithoutChangingDefault(t *testing.T) {
	opts := validOptions()
	opts.ResolveTimeImplementation = func(name string) (string, error) {
		return core.ResolveLogicalTimeImplementationCapabilities(name, "HLAfloat64Time", "HLAinteger64Time")
	}
	manager, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"HLAfloat64Time", "HLAinteger64Time"} {
		if err := manager.CreateFederation(context.Background(), core.CreateFederationRequest{Name: core.FederationName(name), LogicalTimeImplementationName: name}); err != nil {
			t.Fatal(err)
		}
		if actual, ok := manager.LogicalTimeImplementationFor(core.FederationName(name)); !ok || actual != name {
			t.Fatalf("factory %q: %q, %v", name, actual, ok)
		}
	}
	if err := manager.CreateFederation(context.Background(), core.CreateFederationRequest{Name: "unsupported", LogicalTimeImplementationName: "custom"}); !errors.Is(err, core.ErrCouldNotCreateLogicalTimeFactory) {
		t.Fatalf("unknown factory accepted: %v", err)
	}
	if _, err := core.ResolveLogicalTimeImplementationName("HLAinteger64Time"); !errors.Is(err, core.ErrCouldNotCreateLogicalTimeFactory) {
		t.Fatalf("default capability changed: %v", err)
	}
}
