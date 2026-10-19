package declaration_test

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

func TestApplicableR1StopRegistrationRequiresLastActiveInterestAndSwitch(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo := composition.NewFOMRepository()
			fom, err := repo.Load(ctx, []core.FOMModule{{Path: "advice.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Item</name><attribute><name>value</name></attribute><attribute><name>other</name></attribute></objectClass></objectClass></objects></objectModel>`)}})
			if err != nil {
				t.Fatal(err)
			}
			repo.RememberFor("advice", fom)
			cls, _ := fom.LookupObjectClass("Item")
			value, _ := fom.LookupAttribute(cls, "value")
			other, _ := fom.LookupAttribute(cls, "other")
			m := declaration.New()
			m.SetFOMRepository(repo)
			out := delivery.NewWithBatch(16, 1, 0)
			queue, closeQueue, err := out.Subscribe(ctx, "advice", 1)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = closeQueue() })
			m.SetAdvisoryOutbox(out)
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			check(m.PublishObjectClassAttributes(ctx, "advice", 1, cls, []core.AttributeHandle{value}))
			if !enabled {
				check(m.SetAdvisorySwitch(ctx, "advice", 1, core.AdvisoryObjectClassRelevance, false))
			}
			check(m.SubscribeObjectClassAttributes(ctx, "advice", 2, cls, []core.AttributeHandle{value}))
			check(m.SubscribeObjectClassAttributes(ctx, "advice", 3, cls, []core.AttributeHandle{value}))
			check(m.SubscribeObjectClassAttributesWithOptions(ctx, "advice", 4, cls, []core.AttributeHandle{value}, core.SubscriptionOptions{Active: false}))
			check(m.SubscribeObjectClassAttributes(ctx, "advice", 5, cls, []core.AttributeHandle{other}))
			var start, stop int
			drain := func() {
				for {
					select {
					case batch := <-queue:
						for _, event := range batch {
							advice, ok := event.(*declaration.AdvisoryEvent)
							if !ok || advice.ObjectClass != cls {
								t.Fatalf("wrong advice class/type: %#v", event)
							}
							switch advice.Kind {
							case declaration.AdvisoryStartRegistration:
								start++
							case declaration.AdvisoryStopRegistration:
								stop++
							default:
								t.Fatalf("unexpected advice=%+v", advice)
							}
						}
					default:
						return
					}
				}
			}
			drain()
			check(m.UnsubscribeObjectClassAttributes(ctx, "advice", 2, cls, []core.AttributeHandle{value}))
			drain()
			if stop != 0 {
				t.Fatal("stop advice while another matching active subscriber remains")
			}
			check(m.UnsubscribeObjectClassAttributes(ctx, "advice", 3, cls, []core.AttributeHandle{value}))
			drain()
			check(m.UnsubscribeObjectClassAttributes(ctx, "advice", 3, cls, []core.AttributeHandle{value}))
			drain()
			want := 0
			if enabled {
				want = 1
			}
			if start != want || stop != want {
				t.Fatalf("start/stop=%d/%d want=%d/%d", start, stop, want, want)
			}
			if len(m.SubscribersFor(ctx, "advice", cls, []core.AttributeHandle{value})) != 1 {
				t.Fatal("last active removal should retain passive subscription")
			}
		})
	}
}
