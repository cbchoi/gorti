package savebundle

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func TestDeclarationRestoreValidatesRatesAgainstCandidateOnly(t *testing.T) {
	for _, regional := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "regional"}[regional], func(t *testing.T) {
			ctx := context.Background()
			const fed core.FederationName = "candidate"
			repo := composition.NewFOMRepository()
			base, err := repo.Load(ctx, []core.FOMModule{{Path: "base.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>V</name><attribute><name>x</name></attribute></objectClass></objectClass></objects></objectModel>`)}})
			if err != nil {
				t.Fatal(err)
			}
			repo.RememberFor(fed, base)
			baseBytes, err := (FOM{Repository: repo}).Marshal(fed)
			if err != nil {
				t.Fatal(err)
			}
			extension, err := repo.PrepareLiveExtension(ctx, fed, []core.FOMModule{{Path: "rate.xml", XML: []byte(`<objectModel><updateRates><updateRate><name>NewRate</name><rate>12.5</rate></updateRate></updateRates></objectModel>`)}})
			if err != nil {
				t.Fatal(err)
			}
			defer extension.Release()
			if err := extension.Commit(); err != nil {
				t.Fatal(err)
			}
			candidate := extension.FOM()
			cls, _ := candidate.LookupObjectClass("HLAobjectRoot.V")
			attr, _ := candidate.LookupAttribute(cls, "x")
			manager := declaration.New()
			manager.SetFOMRepository(repo)
			manager.SetAdvisoryOutbox(delivery.NewWithBatch(1, 1, 0))
			options := core.SubscriptionOptions{Active: true, UpdateRateDesignator: "NewRate"}
			if regional {
				err = manager.SetRegionalUpdateRateSubscriptions(ctx, fed, []core.RegionalUpdateRateSubscription{{Object: 100, Attribute: attr, Subscriber: 2, Options: options}})
			} else {
				err = manager.SubscribeObjectClassAttributesWithOptions(ctx, fed, 2, cls, []core.AttributeHandle{attr}, options)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.SyncObjectUpdateRateOwners(ctx, fed, 100, cls, map[core.AttributeHandle]core.FederateHandle{attr: 1}); !errors.Is(err, core.ErrOutboxUnavailable) {
				t.Fatalf("missing publisher queue: %v", err)
			}
			before := manager.ExportState(fed)
			if len(before.PendingAdvisories) != 1 {
				t.Fatalf("missing durable advice: %+v", before.PendingAdvisories)
			}
			adapter := Declaration{Manager: manager}
			data, err := adapter.Marshal(fed)
			if err != nil {
				t.Fatal(err)
			}
			// The live model supports NewRate, but the supplied saved base does not.
			if _, err := adapter.PrepareRestoreWithFOM(fed, data, base); !errors.Is(err, core.ErrInvalidUpdateRateDesignator) {
				t.Fatalf("wrong candidate accepted live rate: %v", err)
			}
			if _, err := adapter.PrepareRestoreWithFOM(fed, data, nil); err == nil {
				t.Fatal("nil candidate accepted")
			}
			if err := (FOM{Repository: repo}).Unmarshal(fed, baseBytes); err != nil {
				t.Fatal(err)
			}
			if _, err := adapter.PrepareRestore(fed, data); !errors.Is(err, core.ErrInvalidUpdateRateDesignator) {
				t.Fatalf("legacy path stopped validating live model: %v", err)
			}
			commit, err := adapter.PrepareRestoreWithFOM(fed, data, candidate)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, manager.ExportState(fed)) {
				t.Fatal("preparation changed declarations")
			}
			live, _ := repo.Get(ctx, fed)
			if _, found := live.(core.FOMUpdateRates).UpdateRateValue("NewRate"); found {
				t.Fatal("preparation published candidate")
			}
			commit()
			if !reflect.DeepEqual(before, manager.ExportState(fed)) {
				t.Fatal("candidate restore lost subscription or pending advice")
			}
		})
	}
}
