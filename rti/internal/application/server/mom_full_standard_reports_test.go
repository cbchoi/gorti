package server

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMOMFullStandardReportsActualServer(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			f := newMOMFullFixture(t, factory, momServiceRequestAttributeTransportationTypeChange)
			f.prepare()
			f.subscribe(f.manager.FederateHandle, "HLAmanager.HLAfederate.HLAreport")
			f.subscribe(f.manager.FederateHandle, "HLAmanager.HLAfederation.HLAreport")
			decl := rtiv1.NewDeclarationServiceClient(f.conn)
			for _, h := range []uint64{f.manager.FederateHandle, f.target.FederateHandle} {
				_, err := decl.SubscribeObjectClassAttributes(f.auths[h], &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: h, ObjectClassHandle: uint64(f.command.ObjectClass), AttributeHandles: []uint64{uint64(f.command.Attributes[0])}})
				f.must(err)
			}
			f.callback(f.manager.FederateHandle, func(e *rtiv1.FederateEvent) bool {
				return e.GetDiscover() != nil && e.GetDiscover().ObjectHandle == uint64(f.command.Object)
			})
			f.publishMarker([]uint64{f.manager.FederateHandle, f.target.FederateHandle})
			_, err := rtiv1.NewObjectServiceClient(f.conn).UpdateAttributeValues(f.targetCtx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, ObjectHandle: uint64(f.command.Object), Attributes: map[uint64][]byte{uint64(f.command.Attributes[0]): {9}}})
			f.must(err)
			f.callback(f.manager.FederateHandle, func(e *rtiv1.FederateEvent) bool {
				return e.GetReflect() != nil && e.GetReflect().ObjectHandle == uint64(f.command.Object)
			})
			_, err = rtiv1.NewSyncServiceClient(f.conn).RegisterFederationSynchronizationPoint(f.managerCtx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, Label: "phase", RequiredFederates: []uint64{f.target.FederateHandle}})
			f.must(err)
			f.callback(f.target.FederateHandle, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
			generation, _ := f.r.fedMgr.GenerationFor(momFullFederation)
			joinedModule := &rtiv1.FOMModule{Path: "Joined.xml", Xml: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Joined</name><sharing>PublishSubscribe</sharing></objectClass></objectClass></objects></objectModel>`)}
			joined, err := rtiv1.NewFederationServiceClient(f.conn).JoinFederation(f.ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: momFullFederation, FederateName: "module-source", ExpectedFederationGeneration: &generation, AdditionalFomModules: []*rtiv1.FOMModule{joinedModule}})
			f.must(err)
			f.marker("standard-baseline")
			f.barrier("standard-baseline", f.manager.FederateHandle, mom.ClassReportServiceInvocation, "")
			target := map[string][]byte{"HLAfederate": catalogRPCHandle(f.target.FederateHandle)}
			receiver := map[string][]byte{"HLAfederate": catalogRPCHandle(f.manager.FederateHandle)}
			cases := []struct {
				request string
				args    map[string][]byte
				reports []string
				key     string
			}{
				{mom.ClassRequestInteractionsSent, target, []string{mom.ClassReportInteractionsSent}, "HLAinteractionCounts"},
				{mom.ClassRequestInteractionsReceived, receiver, []string{mom.ClassReportInteractionsReceived}, "HLAinteractionCounts"},
				{mom.ClassRequestUpdatesSent, target, []string{mom.ClassReportUpdatesSent}, "HLAupdateCounts"},
				{mom.ClassRequestReflectionsReceived, receiver, []string{mom.ClassReportReflectionsReceived}, "HLAreflectCounts"},
				{mom.ClassRequestPublications, target, []string{mom.ClassReportObjectClassPublication, mom.ClassReportInteractionPublication}, ""},
				{mom.ClassRequestSubscriptions, target, []string{mom.ClassReportObjectClassSubscription, mom.ClassReportInteractionSubscription}, ""},
				{mom.ClassRequestObjectInstancesThatCanBeDeleted, target, []string{strings.ReplaceAll(mom.ClassRequestObjectInstancesThatCanBeDeleted, "HLArequest", "HLAreport")}, "HLAobjectInstanceCounts"},
				{mom.ClassRequestObjectInstancesUpdated, target, []string{strings.ReplaceAll(mom.ClassRequestObjectInstancesUpdated, "HLArequest", "HLAreport")}, "HLAobjectInstanceCounts"},
				{mom.ClassRequestObjectInstancesReflected, receiver, []string{strings.ReplaceAll(mom.ClassRequestObjectInstancesReflected, "HLArequest", "HLAreport")}, "HLAobjectInstanceCounts"},
				{mom.ClassRequestObjectInstanceInformation, map[string][]byte{"HLAfederate": catalogRPCHandle(f.target.FederateHandle), "HLAobjectInstance": catalogRPCHandle(uint64(f.command.Object))}, []string{strings.ReplaceAll(mom.ClassRequestObjectInstanceInformation, "HLArequest", "HLAreport")}, ""},
				{mom.ClassRequestFederateFOMModuleData, map[string][]byte{"HLAfederate": catalogRPCHandle(joined.FederateHandle), "HLAFOMmoduleIndicator": catalogRPCInt(0)}, []string{strings.ReplaceAll(mom.ClassRequestFederateFOMModuleData, "HLArequest", "HLAreport")}, "HLAFOMmoduleData"},
				{mom.ClassRequestFederationFOMModuleData, map[string][]byte{"HLAFOMmoduleIndicator": catalogRPCInt(0)}, []string{strings.ReplaceAll(mom.ClassRequestFederationFOMModuleData, "HLArequest", "HLAreport")}, "HLAFOMmoduleData"},
				{mom.ClassRequestMIMData, nil, []string{mom.ClassReportMIMData}, "HLAMIMdata"},
				{mom.ClassRequestSynchronizationPoints, nil, []string{strings.ReplaceAll(mom.ClassRequestSynchronizationPoints, "HLArequest", "HLAreport")}, "HLAsyncPoints"},
				{mom.ClassRequestSynchronizationPointStatus, nil, []string{strings.ReplaceAll(mom.ClassRequestSynchronizationPointStatus, "HLArequest", "HLAreport")}, "HLAsyncPointFederates"},
			}
			if len(cases) != 15 {
				t.Fatal("request/report catalog changed")
			}
			for _, tc := range cases {
				t.Run(tc.request, func(t *testing.T) {
					f.t = t
					model, err := f.r.foms.Get(f.ctx, momFullFederation)
					f.must(err)
					class, ok := model.LookupInteractionClass(tc.request)
					if !ok {
						t.Fatal("standard request absent")
					}
					_, err = decl.PublishInteractionClass(f.managerCtx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, InteractionClassHandle: uint64(class)})
					f.must(err)
					before := f.state()
					_, err = rtiv1.NewObjectServiceClient(f.conn).SendInteraction(f.managerCtx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, InteractionClassHandle: uint64(class), Parameters: map[uint64][]byte{999999: {1}}})
					if err == nil || status.Code(err) == codes.DeadlineExceeded {
						t.Fatalf("invalid request accepted or stalled: %v", err)
					}
					if !bytes.Equal(before, f.state()) {
						t.Fatal("malformed request changed service state")
					}
					f.must(f.sendMOM(tc.request, tc.args))
					f.marker(tc.request)
					seen := map[string][]map[string][]byte{}
					f.callback(f.manager.FederateHandle, func(e *rtiv1.FederateEvent) bool {
						if r := e.GetReceive(); r != nil && bytes.Equal(r.UserSuppliedTag, []byte(tc.request)) {
							return true
						}
						name, params := f.reportParams(e)
						name = strings.TrimPrefix(name, "HLAinteractionRoot.")
						for _, wanted := range tc.reports {
							if name == wanted {
								seen[name] = append(seen[name], params)
							}
						}
						return false
					})
					for _, report := range tc.reports {
						if len(seen[report]) != 1 {
							t.Fatalf("%s reports=%d want=1", report, len(seen[report]))
						}
						p := seen[report][0]
						if subject := tc.args["HLAfederate"]; subject != nil && !bytes.Equal(p["HLAfederate"], subject) {
							t.Fatal("report subject mismatch")
						}
						if tc.key != "" {
							if len(p[tc.key]) < 4 {
								t.Fatal("standard report field absent", tc.key)
							}
							if strings.HasSuffix(tc.key, "Counts") {
								counts := p[tc.key]
								if binary.BigEndian.Uint32(counts) == 0 || len(counts) != 4+16*int(binary.BigEndian.Uint32(counts)) {
									t.Fatal("nonzero actual class counts not encoded", counts)
								}
							}
						}
						if tc.key == "HLAFOMmoduleData" || tc.key == "HLAMIMdata" {
							if !strings.Contains(catalogRPCString(t, p[tc.key]), "objectModel") {
								t.Fatal("report lacks actual module source")
							}
						}
						if tc.request == mom.ClassRequestObjectInstanceInformation && (!bytes.Equal(p["HLAobjectInstance"], catalogRPCHandle(uint64(f.command.Object))) || !bytes.Equal(p["HLAregisteredClass"], catalogRPCHandle(uint64(f.command.ObjectClass))) || !bytes.Equal(p["HLAknownClass"], catalogRPCHandle(uint64(f.command.ObjectClass)))) {
							t.Fatal("registered/known object information mismatch")
						}
						if report == mom.ClassReportObjectClassPublication || report == mom.ClassReportObjectClassSubscription {
							if !bytes.Equal(p["HLAobjectClass"], catalogRPCHandle(uint64(f.command.ObjectClass))) || binary.BigEndian.Uint32(p["HLAnumberOfClasses"]) != 1 {
								t.Fatal("class report does not reflect actual declarations")
							}
						}
					}
				})
				f.t = t
			}
		})
	}
}
