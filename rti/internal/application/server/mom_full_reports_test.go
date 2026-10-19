package server

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/pkg/encoding"
	"google.golang.org/grpc/metadata"
)

func (f *momFullFixture) addReceiver(name string) *rtiv1.JoinFederationResponse {
	f.t.Helper()
	generation, _ := f.r.fedMgr.GenerationFor(momFullFederation)
	h := momCompletionReviewJoin(f.ctx, f.t, f.conn, momFullFederation, name, "", generation)
	auth := metadata.AppendToOutgoingContext(f.ctx, session.MetadataKey, string(h.SessionToken))
	stream, err := rtiv1.NewStreamServiceClient(f.conn).EventBatches(auth, &rtiv1.EventsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: h.FederateHandle})
	f.must(err)
	ready, err := stream.Recv()
	f.must(err)
	if !ready.Ready {
		f.t.Fatal("receiver not ready")
	}
	f.streams[h.FederateHandle], f.auths[h.FederateHandle] = stream, auth
	return h
}

func (f *momFullFixture) subscribe(h uint64, className string) {
	f.t.Helper()
	model, err := f.r.foms.Get(f.ctx, momFullFederation)
	f.must(err)
	class, ok := model.LookupInteractionClass(className)
	if !ok {
		f.t.Fatal("subscription class missing", className)
	}
	_, err = rtiv1.NewDeclarationServiceClient(f.conn).SubscribeInteractionClass(f.auths[h], &rtiv1.SubInterRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: h, InteractionClassHandle: uint64(class)})
	f.must(err)
}

func (f *momFullFixture) regional(h uint64, className string, subject uint64, group *uint64) (uint64, uint64) {
	f.t.Helper()
	model, err := f.r.foms.Get(f.ctx, momFullFederation)
	f.must(err)
	names := model.(core.FOMHandleNameLookup)
	fd, ok := names.LookupDimension("HLAfederate")
	if !ok {
		f.t.Fatal("MIM federate dimension absent")
	}
	dims := []uint64{uint64(fd)}
	var gd core.DimensionHandle
	if group != nil {
		gd, ok = names.LookupDimension("HLAserviceGroup")
		if !ok {
			f.t.Fatal("MIM service dimension absent")
		}
		dims = append(dims, uint64(gd))
	}
	regions := rtiv1.NewDDMServiceClient(f.conn)
	r, err := regions.CreateRegion(f.auths[h], &rtiv1.CreateRegionRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: h, RoutingSpaceHandle: 1, DimensionHandles: dims})
	f.must(err)
	f.bounds(h, r.RegionHandle, uint64(fd), uint64(uint32(subject)))
	if group != nil {
		f.bounds(h, r.RegionHandle, uint64(gd), *group)
	}
	f.commit(h, r.RegionHandle)
	class, ok := model.LookupInteractionClass(className)
	if !ok {
		f.t.Fatal("MIM report absent", className)
	}
	_, err = regions.SubscribeInteractionClassWithRegions(f.auths[h], &rtiv1.SubscribeICWithRegionsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: h, InteractionClassHandle: uint64(class), RegionHandles: []uint64{r.RegionHandle}})
	f.must(err)
	return r.RegionHandle, uint64(gd)
}

func (f *momFullFixture) bounds(h, region, dim, value uint64) {
	_, err := rtiv1.NewDDMServiceClient(f.conn).SetRangeBounds(f.auths[h], &rtiv1.SetRangeBoundsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: h, RegionHandle: region, DimensionHandle: dim, Bounds: &rtiv1.Range{Lower: value, Upper: value + 1}})
	f.must(err)
}

func (f *momFullFixture) commit(h, region uint64) {
	_, err := rtiv1.NewDDMServiceClient(f.conn).CommitRegionModifications(f.auths[h], &rtiv1.CommitRegionRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: h, RegionHandles: []uint64{region}})
	f.must(err)
}

// A same-recipient RO marker follows each synchronously admitted report. Seeing
// it on the actual stream proves no matching report remains ahead of the marker.
func (f *momFullFixture) barrier(marker string, h uint64, className, service string) []map[string][]byte {
	var reports []map[string][]byte
	f.callback(h, func(e *rtiv1.FederateEvent) bool {
		if received := e.GetReceive(); received != nil && bytes.Equal(received.UserSuppliedTag, []byte(marker)) {
			return true
		}
		class, params := f.reportParams(e)
		if strings.TrimPrefix(class, "HLAinteractionRoot.") != className {
			return false
		}
		if subject, exists := params["HLAfederate"]; exists && !bytes.Equal(subject, catalogRPCHandle(f.target.FederateHandle)) {
			return false
		}
		if service != "" && catalogRPCString(f.t, params["HLAservice"]) != service {
			return false
		}
		reports = append(reports, params)
		return false
	})
	return reports
}

func (f *momFullFixture) marker(marker string) {
	_, err := rtiv1.NewObjectServiceClient(f.conn).SendInteraction(f.targetCtx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, InteractionClassHandle: uint64(f.command.InteractionClass), UserSuppliedTag: []byte(marker)})
	f.must(err)
}

func (f *momFullFixture) publishMarker(receivers []uint64) {
	_, err := rtiv1.NewDeclarationServiceClient(f.conn).PublishInteractionClass(f.targetCtx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, InteractionClassHandle: uint64(f.command.InteractionClass)})
	f.must(err)
	for _, h := range receivers {
		f.subscribe(h, "Event")
	}
}

func TestMOMFullServiceReportRegionalRoutesActualServer(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			f := newMOMFullFixture(t, factory, momServiceEnableAsynchronousDelivery)
			matched, wrongSubject, wrongGroup := f.addReceiver("matched"), f.addReceiver("wrong-subject"), f.addReceiver("wrong-group")
			group := uint64(4)
			region, dimension := f.regional(matched.FederateHandle, mom.ClassReportServiceInvocation, f.target.FederateHandle, &group)
			f.regional(wrongSubject.FederateHandle, mom.ClassReportServiceInvocation, f.manager.FederateHandle, &group)
			group = 1
			f.regional(wrongGroup.FederateHandle, mom.ClassReportServiceInvocation, f.target.FederateHandle, &group)
			// Global + regional subscriptions select a recipient only once.
			f.regional(f.manager.FederateHandle, mom.ClassReportServiceInvocation, f.target.FederateHandle, &group)
			receivers := []uint64{f.manager.FederateHandle, matched.FederateHandle, wrongSubject.FederateHandle, wrongGroup.FederateHandle}
			f.publishMarker(receivers)
			f.marker("baseline")
			for _, h := range receivers {
				f.barrier("baseline", h, mom.ClassReportServiceInvocation, "")
			}
			for stage, service := range []string{momServiceEnableAsynchronousDelivery, momServiceDisableAsynchronousDelivery, momServiceEnableAsynchronousDelivery} {
				t.Run([]string{"committedMatch", "pendingBoundsDoNotRoute", "committedGroupExcludes"}[stage], func(t *testing.T) {
					f.t = t
					if stage == 1 {
						f.bounds(matched.FederateHandle, region, dimension, 1)
					}
					if stage == 2 {
						f.commit(matched.FederateHandle, region)
					}
					f.must(f.sendMOM(mom.ServiceClassPrefix+"HLA"+service, map[string][]byte{"HLAfederate": catalogRPCHandle(f.target.FederateHandle)}))
					marker := fmt.Sprintf("stage-%d", stage)
					f.marker(marker)
					for _, h := range receivers {
						got := f.barrier(marker, h, mom.ClassReportServiceInvocation, service)
						want := 0
						if h == f.manager.FederateHandle || h == matched.FederateHandle && stage < 2 {
							want = 1
						}
						if len(got) != want {
							t.Fatalf("recipient=%d report count=%d want=%d", h, len(got), want)
						}
						for _, report := range got {
							if !bytes.Equal(report["HLAsuccessIndicator"], catalogRPCInt(1)) {
								t.Fatal("successful call reported failure")
							}
						}
					}
				})
				f.t = t
			}
		})
	}
}

func TestMOMFullCounterReportsActualServer(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		for _, transport := range []string{"HLAreliable", bestEffortTransportationName} {
			t.Run(factory+"/"+transport, func(t *testing.T) {
				f := newMOMFullFixture(t, factory, momServiceRequestAttributeTransportationTypeChange)
				f.prepare()
				f.subscribe(f.manager.FederateHandle, "HLAmanager.HLAfederate.HLAreport")
				decl := rtiv1.NewDeclarationServiceClient(f.conn)
				_, err := decl.SubscribeObjectClassAttributes(f.managerCtx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, ObjectClassHandle: uint64(f.command.ObjectClass), AttributeHandles: []uint64{uint64(f.command.Attributes[0])}})
				f.must(err)
				f.callback(f.manager.FederateHandle, func(e *rtiv1.FederateEvent) bool { return e.GetDiscover() != nil })
				f.publishMarker([]uint64{f.manager.FederateHandle})
				if transport == bestEffortTransportationName {
					f.activateTransportation(false)
					f.activateTransportation(true)
				}
				f.marker("baseline")
				f.barrier("baseline", f.manager.FederateHandle, mom.ClassReportServiceInvocation, "")
				objects := rtiv1.NewObjectServiceClient(f.conn)
				_, err = objects.UpdateAttributeValues(f.targetCtx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, ObjectHandle: uint64(f.command.Object), Attributes: map[uint64][]byte{uint64(f.command.Attributes[0]): {7}}})
				f.must(err)
				f.callback(f.manager.FederateHandle, func(e *rtiv1.FederateEvent) bool {
					if reflected := e.GetReflect(); reflected != nil && reflected.ObjectHandle == uint64(f.command.Object) {
						want := rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE
						if transport == bestEffortTransportationName {
							want = rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT
						}
						if reflected.Metadata == nil || reflected.Metadata.TransportationType == nil || reflected.Metadata.GetTransportationType() != want {
							t.Fatal("actual callback transportation mismatch", reflected.Metadata)
						}
						return true
					}
					return false
				})
				for _, tc := range []struct {
					request, report, parameter string
					subject, class             uint64
					want                       uint32
				}{
					{mom.ClassRequestUpdatesSent, mom.ClassReportUpdatesSent, "HLAupdateCounts", f.target.FederateHandle, uint64(f.command.ObjectClass), 1},
					{mom.ClassRequestReflectionsReceived, mom.ClassReportReflectionsReceived, "HLAreflectCounts", f.manager.FederateHandle, uint64(f.command.ObjectClass), 1},
					{mom.ClassRequestInteractionsSent, mom.ClassReportInteractionsSent, "HLAinteractionCounts", f.target.FederateHandle, uint64(f.command.InteractionClass), 3},
					{mom.ClassRequestInteractionsReceived, mom.ClassReportInteractionsReceived, "HLAinteractionCounts", f.manager.FederateHandle, uint64(f.command.InteractionClass), 4},
				} {
					t.Run(tc.request, func(t *testing.T) {
						f.t = t
						f.must(f.sendMOM(tc.request, map[string][]byte{"HLAfederate": catalogRPCHandle(tc.subject)}))
						marker := tc.request + "-boundary"
						f.marker(marker)
						found := false
						f.callback(f.manager.FederateHandle, func(e *rtiv1.FederateEvent) bool {
							if r := e.GetReceive(); r != nil && bytes.Equal(r.UserSuppliedTag, []byte(marker)) {
								return true
							}
							class, params := f.reportParams(e)
							if strings.TrimPrefix(class, "HLAinteractionRoot.") != tc.report || catalogRPCString(t, params["HLAtransportation"]) != transport {
								return false
							}
							if !bytes.Equal(params["HLAfederate"], catalogRPCHandle(tc.subject)) {
								t.Fatal("report subject mismatch")
							}
							counts := params[tc.parameter]
							if len(counts) < 4 || len(counts) != 4+16*int(binary.BigEndian.Uint32(counts)) {
								t.Fatal("malformed standard counts", counts)
							}
							for offset := 4; offset < len(counts); offset += 16 {
								if bytes.Equal(counts[offset:offset+12], catalogRPCHandle(tc.class)) {
									if got := binary.BigEndian.Uint32(counts[offset+12:]); got != tc.want {
										t.Fatalf("partition count=%d want=%d", got, tc.want)
									}
									found = true
								}
							}
							return false
						})
						if !found {
							t.Fatal("actual traffic not present in requested transportation partition")
						}
					})
					f.t = t
				}
			})
		}
	}
}

func TestMOMFullMixedAttributeTransportationPartitionsActualServer(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			f := newMOMFullFixture(t, factory, momServiceRequestAttributeTransportationTypeChange)
			model, err := f.r.foms.Get(f.ctx, momFullFederation)
			f.must(err)
			class, ok := model.LookupObjectClass("Item.Leaf")
			if !ok {
				t.Fatal("mixed class missing")
			}
			inherited, ok := model.LookupAttribute(class, "Inherited")
			if !ok {
				t.Fatal("inherited attribute missing")
			}
			local, ok := model.LookupAttribute(class, "Local")
			if !ok {
				t.Fatal("local attribute missing")
			}
			f.command.ObjectClass, f.command.Attributes = class, []core.AttributeHandle{inherited}
			decl := rtiv1.NewDeclarationServiceClient(f.conn)
			attrs := []uint64{uint64(inherited), uint64(local)}
			_, err = decl.PublishObjectClassAttributes(f.targetCtx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: attrs})
			f.must(err)
			_, err = decl.SubscribeObjectClassAttributes(f.managerCtx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, ObjectClassHandle: uint64(class), AttributeHandles: attrs})
			f.must(err)
			objects := rtiv1.NewObjectServiceClient(f.conn)
			registered, err := objects.RegisterObjectInstance(f.targetCtx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, ObjectClassHandle: uint64(class)})
			f.must(err)
			f.command.Object = core.ObjectHandle(registered.ObjectHandle)
			f.callback(f.manager.FederateHandle, func(e *rtiv1.FederateEvent) bool {
				return e.GetDiscover() != nil && e.GetDiscover().ObjectHandle == registered.ObjectHandle
			})
			f.subscribe(f.manager.FederateHandle, "HLAmanager.HLAfederate.HLAreport")
			f.publishMarker([]uint64{f.manager.FederateHandle})
			f.activateTransportation(false)
			f.marker("mixed-baseline")
			f.barrier("mixed-baseline", f.manager.FederateHandle, mom.ClassReportServiceInvocation, "")
			_, err = objects.UpdateAttributeValues(f.targetCtx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, ObjectHandle: registered.ObjectHandle, Attributes: map[uint64][]byte{uint64(inherited): {1}, uint64(local): {2}}})
			f.must(err)
			seen := map[rtiv1.TransportationType]bool{}
			for i := 0; i < 2; i++ {
				f.callback(f.manager.FederateHandle, func(e *rtiv1.FederateEvent) bool {
					r := e.GetReflect()
					if r == nil || r.ObjectHandle != registered.ObjectHandle {
						return false
					}
					if r.Metadata == nil || r.Metadata.TransportationType == nil || len(r.Attributes) != 1 || seen[r.Metadata.GetTransportationType()] {
						t.Fatal("mixed update was not split into distinct actual partitions", r)
					}
					wantAttr := uint64(local)
					if r.Metadata.GetTransportationType() == rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT {
						wantAttr = uint64(inherited)
					} else if r.Metadata.GetTransportationType() != rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE {
						t.Fatal("unknown actual transportation")
					}
					if _, ok := r.Attributes[wantAttr]; !ok {
						t.Fatal("attribute assigned to wrong transportation partition")
					}
					seen[r.Metadata.GetTransportationType()] = true
					return true
				})
			}
			for _, tc := range []struct {
				request, report, parameter string
				subject                    uint64
			}{
				{mom.ClassRequestUpdatesSent, mom.ClassReportUpdatesSent, "HLAupdateCounts", f.target.FederateHandle},
				{mom.ClassRequestReflectionsReceived, mom.ClassReportReflectionsReceived, "HLAreflectCounts", f.manager.FederateHandle},
			} {
				t.Run(tc.request, func(t *testing.T) {
					f.t = t
					f.must(f.sendMOM(tc.request, map[string][]byte{"HLAfederate": catalogRPCHandle(tc.subject)}))
					f.marker(tc.request)
					partitions := map[string]int{}
					f.callback(f.manager.FederateHandle, func(e *rtiv1.FederateEvent) bool {
						if r := e.GetReceive(); r != nil && bytes.Equal(r.UserSuppliedTag, []byte(tc.request)) {
							return true
						}
						name, params := f.reportParams(e)
						if strings.TrimPrefix(name, "HLAinteractionRoot.") != tc.report {
							return false
						}
						counts := params[tc.parameter]
						if len(counts) != 20 || binary.BigEndian.Uint32(counts) != 1 || !bytes.Equal(counts[4:16], catalogRPCHandle(uint64(class))) || binary.BigEndian.Uint32(counts[16:]) != 1 {
							t.Fatal("mixed update partition must count exactly one actual class message", params)
						}
						partitions[catalogRPCString(t, params["HLAtransportation"])]++
						return false
					})
					if len(partitions) != 2 || partitions["HLAreliable"] != 1 || partitions[bestEffortTransportationName] != 1 {
						t.Fatal("mixed actual partitions not both reported once", partitions)
					}
				})
				f.t = t
			}
		})
	}
}

func (f *momFullFixture) activateTransportation(interaction bool) {
	f.t.Helper()
	service := momServiceRequestAttributeTransportationTypeChange
	params := map[string][]byte{"HLAfederate": catalogRPCHandle(f.target.FederateHandle), "HLAobjectInstance": catalogRPCHandle(uint64(f.command.Object)), "HLAattributeList": append(catalogRPCInt(1), catalogRPCHandle(uint64(f.command.Attributes[0]))...)}
	if interaction {
		service = momServiceRequestInteractionTransportationTypeChange
		delete(params, "HLAobjectInstance")
		delete(params, "HLAattributeList")
		params["HLAinteractionClass"] = catalogRPCHandle(uint64(f.command.InteractionClass))
	}
	encoded, err := (encoding.HLAunicodeString{}).Encode(bestEffortTransportationName)
	f.must(err)
	params["HLAtransportation"] = encoded
	f.must(f.sendMOM(mom.ServiceClassPrefix+"HLA"+service, params))
	f.callbackAtEntry(f.target.FederateHandle, func(e *rtiv1.FederateEvent) bool { return e.GetTransportationChangeConfirmed() != nil }, func(event *rtiv1.FederateEvent) {
		change := event.GetTransportationChangeConfirmed()
		_, err := rtiv1.NewObjectServiceClient(f.conn).ActivateTransportationTypeChange(f.targetCtx, &rtiv1.ActivateTransportationTypeChangeRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, Ticket: change.Ticket, ExpectedFederationGeneration: change.FederationGeneration, ExpectedCallbackEpoch: change.CallbackEpoch})
		f.must(err)
	})
}

func TestMOMFullSentPartitionsWithoutSubscribersActualServer(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			f := newMOMFullFixture(t, factory, momServiceRequestAttributeTransportationTypeChange)
			f.prepare()
			f.subscribe(f.manager.FederateHandle, "HLAmanager.HLAfederate.HLAreport")
			_, err := rtiv1.NewDeclarationServiceClient(f.conn).PublishInteractionClass(f.targetCtx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, InteractionClassHandle: uint64(f.command.InteractionClass)})
			f.must(err)
			f.activateTransportation(false)
			f.activateTransportation(true)
			objects := rtiv1.NewObjectServiceClient(f.conn)
			_, err = objects.UpdateAttributeValues(f.targetCtx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, ObjectHandle: uint64(f.command.Object), Attributes: map[uint64][]byte{uint64(f.command.Attributes[0]): {7}}})
			f.must(err)
			_, err = objects.SendInteraction(f.targetCtx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, InteractionClassHandle: uint64(f.command.InteractionClass)})
			f.must(err)
			for _, tc := range []struct {
				request, report, parameter string
				class                      uint64
			}{
				{mom.ClassRequestUpdatesSent, mom.ClassReportUpdatesSent, "HLAupdateCounts", uint64(f.command.ObjectClass)},
				{mom.ClassRequestInteractionsSent, mom.ClassReportInteractionsSent, "HLAinteractionCounts", uint64(f.command.InteractionClass)},
			} {
				t.Run(tc.request, func(t *testing.T) {
					f.t = t
					f.must(f.sendMOM(tc.request, map[string][]byte{"HLAfederate": catalogRPCHandle(f.target.FederateHandle)}))
					f.callback(f.manager.FederateHandle, func(event *rtiv1.FederateEvent) bool {
						name, params := f.reportParams(event)
						if strings.TrimPrefix(name, "HLAinteractionRoot.") != tc.report || catalogRPCString(t, params["HLAtransportation"]) != bestEffortTransportationName {
							return false
						}
						counts := params[tc.parameter]
						if len(counts) != 20 || binary.BigEndian.Uint32(counts) != 1 || !bytes.Equal(counts[4:16], catalogRPCHandle(tc.class)) || binary.BigEndian.Uint32(counts[16:]) != 1 {
							t.Fatal("accepted no-recipient send must count exactly one best-effort message", params)
						}
						return true
					})
				})
				f.t = t
			}
		})
	}
}
