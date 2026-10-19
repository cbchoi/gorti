package ddm_test

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ddm"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

type realizationFilter struct{ *ddm.Manager }

func (d realizationFilter) PublisherRegionsFor(f core.FederationName, o core.ObjectHandle, a core.AttributeHandle) []object.DDMRegionHandle {
	var out []object.DDMRegionHandle
	for _, r := range d.Manager.PublisherRegionsFor(f, o, a) {
		out = append(out, object.DDMRegionHandle(r))
	}
	return out
}
func (d realizationFilter) SubscribersForUpdate(f core.FederationName, c core.ObjectClassHandle, a core.AttributeHandle, rs []object.DDMRegionHandle) []core.FederateHandle {
	var regions []ddm.RegionHandle
	for _, r := range rs {
		regions = append(regions, ddm.RegionHandle(r))
	}
	return d.Manager.SubscribersForUpdate(f, c, a, regions)
}

func TestRealizationROAndTSOServicePorts(t *testing.T) {
	for _, timestamped := range []bool{false, true} {
		for _, regional := range []bool{true, false} {
			t.Run(map[bool]string{false: "RO", true: "TSO"}[timestamped]+"/"+map[bool]string{false: "default", true: "regional"}[regional], func(t *testing.T) {
				ctx := context.Background()
				const fed core.FederationName = "realization-delivery"
				ok := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				repo := composition.NewFOMRepository()
				xml := []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Vehicle</name><attribute><name>A</name><dataType>HLAinteger32BE</dataType><dimensions><dimension>X</dimension><dimension>Y</dimension></dimensions></attribute><attribute><name>B</name><dataType>HLAinteger32BE</dataType><dimensions><dimension>Y</dimension></dimensions></attribute></objectClass></objectClass></objects><interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Signal</name><dimensions><dimension>X</dimension><dimension>Y</dimension></dimensions><parameter><name>value</name><dataType>HLAinteger32BE</dataType></parameter></interactionClass></interactionClass></interactions><dimensions><dimension><name>X</name><upperBound>100</upperBound><value>[10..20)</value></dimension><dimension><name>Y</name><upperBound>100</upperBound><value>Excluded</value></dimension></dimensions></objectModel>`)
				h, err := repo.Load(ctx, []core.FOMModule{{Path: "realization.xml", XML: xml}})
				ok(err)
				repo.RememberFor(fed, h)
				cls, _ := h.LookupObjectClass("Vehicle")
				a, _ := h.LookupAttribute(cls, "A")
				b, _ := h.LookupAttribute(cls, "B")
				signal, _ := h.LookupInteractionClass("Signal")
				parameter, _ := h.LookupParameter(signal, "value")
				out := delivery.NewWithBatch(64, 1, 0)
				queues := map[core.FederateHandle]<-chan []core.OutboundEvent{}
				for _, sub := range []core.FederateHandle{2, 3, 4} {
					q, closeQ, err := out.Subscribe(ctx, fed, sub)
					ok(err)
					t.Cleanup(func() { _ = closeQ() })
					queues[sub] = q
				}
				drain := func(sub core.FederateHandle) []*object.CallbackEvent {
					var result []*object.CallbackEvent
					for {
						select {
						case batch := <-queues[sub]:
							for _, e := range batch {
								if callback, yes := e.(*object.CallbackEvent); yes {
									result = append(result, callback)
								}
							}
						default:
							return result
						}
					}
				}
				clock := core.NewManualClock(time.Unix(0, 0))
				tm, err := timepkg.New(timepkg.Options{Clock: clock, Outbox: out})
				ok(err)
				decl := declaration.New()
				decl.SetFOMRepository(repo)
				var registry *object.Registry
				regions, err := ddm.New(ddm.Options{Outbox: out, FOMs: repo, OnObjectSubscriptionInterest: func(ctx context.Context, f core.FederationName, s core.FederateHandle, c core.ObjectClassHandle, attrs []core.AttributeHandle) error {
					registry.ObjectClassSubscribed(ctx, f, s, c, attrs)
					return nil
				}, OnInteractionSubscriptionInterest: decl.SetRegionalInteractionSubscriptionInterest})
				ok(err)
				registry, err = object.New(object.Options{Clock: clock, FOMs: repo, Declarations: decl, Outbox: out, DDM: realizationFilter{regions}, RegistrationRegions: regions, TSOGate: tm, TSOValidator: tm})
				ok(err)
				decl.SetOnSubscribeObjectClass(registry.ObjectClassSubscribed)
				ok(decl.PublishObjectClassAttributes(ctx, fed, 1, cls, []core.AttributeHandle{a, b}))
				ok(decl.PublishInteractionClass(ctx, fed, 1, signal))
				space, _ := regions.LookupRoutingSpace(fed, ddm.DefaultRoutingSpace)
				x, _ := regions.LookupDimension(fed, space, "X")
				spec := func(owner core.FederateHandle, bounds *ddm.Range) ddm.RegionHandle {
					var dims []ddm.DimensionHandle
					if bounds != nil {
						dims = []ddm.DimensionHandle{x}
					}
					r, err := regions.CreateRegion(ctx, fed, owner, space, dims)
					ok(err)
					if bounds != nil {
						ok(regions.SetRangeBounds(fed, owner, r, x, *bounds))
					}
					ok(regions.CommitRegionModifications(ctx, fed, owner, []ddm.RegionHandle{r}))
					return r
				}
				pub := spec(1, &ddm.Range{Lower: 12, Upper: 15})
				emptyPub := spec(1, nil)
				defaultSub := spec(2, nil)
				nonoverlap := spec(3, &ddm.Range{Lower: 40, Upper: 50})
				ok(regions.SubscribeObjectClassAttributesWithRegions(ctx, fed, 2, cls, []core.AttributeHandle{a, b}, []ddm.RegionHandle{defaultSub}))
				ok(regions.SubscribeObjectClassAttributesWithRegions(ctx, fed, 3, cls, []core.AttributeHandle{a}, []ddm.RegionHandle{nonoverlap}))
				ok(decl.SubscribeObjectClassAttributes(ctx, fed, 4, cls, []core.AttributeHandle{a, b}))
				ok(regions.SubscribeInteractionClassWithRegions(ctx, fed, 2, signal, []ddm.RegionHandle{defaultSub}))
				ok(regions.SubscribeInteractionClassWithRegions(ctx, fed, 3, signal, []ddm.RegionHandle{nonoverlap}))
				ok(decl.SubscribeInteractionClass(ctx, fed, 4, signal))
				obj, _, err := registry.RegisterWithRegions(ctx, fed, 1, cls, "", map[core.AttributeHandle][]ddm.RegionHandle{a: {pub}, b: {emptyPub}})
				ok(err)
				for _, sub := range []core.FederateHandle{2, 3, 4} {
					if timestamped {
						ok(tm.EnableConstrained(ctx, fed, sub))
					} else {
						ok(tm.EnableAsynchronousDelivery(ctx, fed, sub))
					}
					drain(sub)
				}
				var stamp *core.LogicalTime
				if timestamped {
					value := core.LogicalTime(10)
					stamp = &value
				}
				ok(registry.UpdateAttributes(ctx, fed, 1, obj, map[core.AttributeHandle][]byte{a: {1}, b: {2}}, stamp))
				if regional {
					ok(registry.SendInteractionWithRegions(ctx, fed, 1, signal, map[core.ParameterHandle][]byte{parameter: {3}}, stamp, []ddm.RegionHandle{pub}))
				} else {
					ok(registry.SendInteraction(ctx, fed, 1, signal, map[core.ParameterHandle][]byte{parameter: {3}}, stamp))
				}
				if timestamped {
					for _, sub := range []core.FederateHandle{2, 3, 4} {
						for _, event := range drain(sub) {
							if event.Kind == object.CallbackReflect || event.Kind == object.CallbackReceive {
								t.Fatalf("TSO delivered before grant: %+v", event)
							}
						}
						ok(tm.TimeAdvanceRequest(ctx, fed, sub, 10))
					}
				}
				for _, sub := range []core.FederateHandle{2, 3, 4} {
					events := drain(sub)
					reflects, receives := 0, 0
					values := map[uint64]bool{}
					for _, event := range events {
						switch event.Kind {
						case object.CallbackReflect:
							reflects++
							for attr := range event.Attributes {
								values[attr] = true
							}
							if sub == 2 || sub == 4 {
								if len(event.Attributes) != 1 || event.Attributes[uint64(a)] == nil {
									t.Fatalf("all-excluded B leaked: %+v", event)
								}
							}
						case object.CallbackReceive:
							receives++
						}
					}
					wantReflect, wantReceive := 1, 1
					if sub == 3 {
						wantReflect, wantReceive = 0, 0
						if !regional {
							wantReceive = 1
						}
					}
					if sub == 4 {
						if !values[uint64(a)] || values[uint64(b)] {
							t.Fatal("ordinary subscription lost A or received all-excluded B")
						}
					}
					if reflects != wantReflect || receives != wantReceive {
						t.Fatalf("sub %d got reflect=%d receive=%d; want %d/%d", sub, reflects, receives, wantReflect, wantReceive)
					}
				}
				queued := &object.CallbackEvent{Kind: object.CallbackReflect, ObjectClass: cls, Attributes: map[uint64][]byte{uint64(b): {2}}, Metadata: &core.ObjectCallbackMetadata{Producer: 1, Regions: []core.DDMRegionHandleCore{emptyPub}}}
				if registry.FilterQueuedMessage(ctx, fed, 4, queued) != nil {
					t.Fatal("queued all-excluded B overlaps an ordinary subscription")
				}
			})
		}
	}
}

func TestExcludedDefaultInteractionReceiversServicePorts(t *testing.T) {
	ctx := context.Background()
	const fed core.FederationName = "excluded-default"
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	repo := composition.NewFOMRepository()
	h := composition.NewModelHandle(model.NewFOMWithDimensions(nil, []model.InteractionClass{{Name: "Signal", Dimensions: []string{"Y"}, Parameters: []model.Parameter{{Name: "value"}}}}, nil, []model.Dimension{{Name: "Y", UpperBound: 100, DefaultValue: "Excluded"}}))
	repo.RememberFor(fed, h)
	signal, _ := h.LookupInteractionClass("Signal")
	parameter, _ := h.LookupParameter(signal, "value")
	out := delivery.NewWithBatch(32, 1, 0)
	queues := map[core.FederateHandle]<-chan []core.OutboundEvent{}
	for _, sub := range []core.FederateHandle{2, 3} {
		q, closeQ, err := out.Subscribe(ctx, fed, sub)
		ok(err)
		t.Cleanup(func() { _ = closeQ() })
		queues[sub] = q
	}
	count := func(sub core.FederateHandle) int {
		n := 0
		for {
			select {
			case batch := <-queues[sub]:
				for _, event := range batch {
					if e, yes := event.(*object.CallbackEvent); yes && e.Kind == object.CallbackReceive {
						n++
					}
				}
			default:
				return n
			}
		}
	}
	decl := declaration.New()
	decl.SetFOMRepository(repo)
	regions, err := ddm.New(ddm.Options{Outbox: out, FOMs: repo, OnInteractionSubscriptionInterest: decl.SetRegionalInteractionSubscriptionInterest})
	ok(err)
	registry, err := object.New(object.Options{Clock: core.NewManualClock(time.Unix(0, 0)), Outbox: out, FOMs: repo, Declarations: decl, RegistrationRegions: regions})
	ok(err)
	space, _ := regions.LookupRoutingSpace(fed, ddm.DefaultRoutingSpace)
	y, _ := regions.LookupDimension(fed, space, "Y")
	spec := func(owner core.FederateHandle, explicit bool) ddm.RegionHandle {
		var dims []ddm.DimensionHandle
		if explicit {
			dims = []ddm.DimensionHandle{y}
		}
		r, err := regions.CreateRegion(ctx, fed, owner, space, dims)
		ok(err)
		if explicit {
			ok(regions.SetRangeBounds(fed, owner, r, y, ddm.Range{Lower: 1, Upper: 2}))
		}
		ok(regions.CommitRegionModifications(ctx, fed, owner, []ddm.RegionHandle{r}))
		return r
	}
	allExcluded := spec(2, false)
	ok(regions.SubscribeInteractionClassWithRegions(ctx, fed, 2, signal, []ddm.RegionHandle{allExcluded}))
	ok(decl.PublishInteractionClass(ctx, fed, 1, signal))
	ok(decl.SubscribeInteractionClass(ctx, fed, 3, signal))
	params := map[core.ParameterHandle][]byte{parameter: {7}}
	ok(registry.SendInteraction(ctx, fed, 1, signal, params, nil))
	if count(2) != 0 || count(3) != 1 {
		t.Fatal("all-excluded regional realization received ordinary send or ordinary subscriber lost it")
	}
	emptyPub := spec(1, false)
	ok(registry.SendInteractionWithRegions(ctx, fed, 1, signal, params, nil, []ddm.RegionHandle{emptyPub}))
	if count(2) != 0 || count(3) != 0 {
		t.Fatal("all-excluded producer realization overlaps ordinary default-region subscription")
	}
	ok(decl.UnsubscribeInteractionClass(ctx, fed, 3, signal))
	explicitSub := spec(3, true)
	ok(regions.SubscribeInteractionClassWithRegions(ctx, fed, 3, signal, []ddm.RegionHandle{explicitSub}))
	ok(registry.SendInteraction(ctx, fed, 1, signal, params, nil))
	if count(2) != 0 || count(3) != 1 {
		t.Fatal("explicit excluded-dimension override did not overlap default")
	}
	ok(registry.SendInteractionWithRegions(ctx, fed, 1, signal, params, nil, []ddm.RegionHandle{emptyPub}))
	if count(2) != 0 || count(3) != 0 {
		t.Fatal("empty realization overlaps explicit realization")
	}
	explicitPub := spec(1, true)
	ok(registry.SendInteractionWithRegions(ctx, fed, 1, signal, params, nil, []ddm.RegionHandle{explicitPub}))
	if count(2) != 0 || count(3) != 1 {
		t.Fatal("explicit override lost regional overlap")
	}
}
