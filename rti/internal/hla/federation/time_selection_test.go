package federation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

func TestFederationTimeSelectionIsExplicitAndImmutable(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"", "HLAfloat64Time"} {
		t.Run("selected-"+name, func(t *testing.T) {
			mgr, err := federation.New(validOptions())
			if err != nil {
				t.Fatal(err)
			}
			if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "selected", LogicalTimeImplementationName: name}); err != nil {
				t.Fatal(err)
			}
			rows, err := mgr.List(ctx)
			if err != nil || len(rows) != 1 || rows[0].LogicalTimeImplementationName != "HLAfloat64Time" {
				t.Fatalf("selected representation not recorded: %+v, %v", rows, err)
			}
			lookup, ok := any(mgr).(core.FederationTimeImplementation)
			if !ok {
				t.Fatal("time representation lookup missing")
			}
			if got, exists := lookup.LogicalTimeImplementationFor("selected"); !exists || got != rows[0].LogicalTimeImplementationName {
				t.Fatalf("lookup=%q,%t", got, exists)
			}
			rosters := mgr.Snapshot()
			if len(rosters) != 1 || rosters[0].LogicalTimeImplementationName != rows[0].LogicalTimeImplementationName {
				t.Fatalf("roster lost selected time representation: %+v", rosters)
			}
			if err := mgr.DestroyFederation(ctx, "selected"); err != nil {
				t.Fatal(err)
			}
			if _, exists := lookup.LogicalTimeImplementationFor("selected"); exists {
				t.Fatal("destroyed representation remains visible")
			}
		})
	}
}

func TestUnsupportedTimeSelectionFailsBeforeModelOrGenerationMutation(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"HLAinteger64Time", "custom", "hlafloat64time", " HLAfloat64Time"} {
		t.Run(name, func(t *testing.T) {
			opts := validOptions()
			modelFailure := errors.New("FOM must not be loaded")
			repo := opts.FOMs.(*stubFOMRepo)
			repo.loadErr = modelFailure
			mgr, _ := federation.New(opts)
			err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "reusable", LogicalTimeImplementationName: name})
			if err == nil || errors.Is(err, modelFailure) {
				t.Fatalf("unsupported time reached FOM loader: %v", err)
			}
			rows, _ := mgr.List(ctx)
			if len(rows) != 0 {
				t.Fatal("unsupported creation published federation")
			}
			repo.loadErr = nil
			if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "reusable"}); err != nil {
				t.Fatal(err)
			}
			rows, _ = mgr.List(ctx)
			if rows[0].Generation != 0 {
				t.Fatal("unsupported creation consumed generation")
			}
		})
	}
}
