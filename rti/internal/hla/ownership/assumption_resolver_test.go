package ownership_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

func TestPublisherResolverIncludesAncestorPublicationsNotSubscriptions(t *testing.T) {
	ctx := context.Background()
	repo := composition.NewFOMRepository()
	fom, err := repo.Load(ctx, []core.FOMModule{{Path: "assumption.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute><objectClass><name>Drone</name><attribute><name>altitude</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("fed", fom)
	parent, _ := fom.LookupObjectClass("Vehicle")
	child, _ := fom.LookupObjectClass("Vehicle.Drone")
	attr, _ := fom.LookupAttribute(child, "value")
	parentAttr, _ := fom.LookupAttribute(parent, "value")
	if attr == parentAttr {
		t.Fatal("fixture must exercise different inherited attribute handles")
	}
	if parent == 0 || child == 0 || attr == 0 {
		t.Fatal("missing class hierarchy fixture")
	}
	decl := declaration.New()
	decl.SetFOMRepository(repo)
	for _, entry := range []struct {
		h core.FederateHandle
		c core.ObjectClassHandle
	}{{3, child}, {2, parent}, {1, child}, {1, parent}} {
		published, _ := fom.LookupAttribute(entry.c, "value")
		if err := decl.PublishObjectClassAttributes(ctx, "fed", entry.h, entry.c, []core.AttributeHandle{published}); err != nil {
			t.Fatal(err)
		}
	}
	if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 4, child, []core.AttributeHandle{attr}); err != nil {
		t.Fatal(err)
	}
	resolve := ownership.NewPublisherResolver(func(_ core.FederationName, obj core.ObjectHandle) core.ObjectClassHandle {
		if obj == 10 {
			return child
		}
		return 0
	}, decl, repo)
	for i := 0; i < 2; i++ {
		got := resolve(ctx, "fed", 10, []core.AttributeHandle{attr, attr})
		if !reflect.DeepEqual(got, []core.FederateHandle{1, 2, 3}) {
			t.Fatalf("candidate publishers=%v", got)
		}
		got[0] = 99
	}
	if len(resolve(ctx, "fed", 99, []core.AttributeHandle{attr})) != 0 {
		t.Fatal("unknown object produced candidates")
	}
}
