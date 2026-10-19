package server

import (
	"bytes"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMOMFullExceptionReportsActualServer(t *testing.T) {
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		t.Run(factory, func(t *testing.T) {
			f := newMOMFullFixture(t, factory, momServiceEnableAsynchronousDelivery)
			matched, excluded := f.addReceiver("exception-matched"), f.addReceiver("exception-excluded")
			for _, class := range []string{mom.ClassReportException, mom.ClassReportMOMException} {
				f.subscribe(f.manager.FederateHandle, class)
				f.regional(matched.FederateHandle, class, f.target.FederateHandle, nil)
				f.regional(excluded.FederateHandle, class, f.manager.FederateHandle, nil)
			}
			f.must(f.sendMOM(mom.ClassSetExceptionReporting, map[string][]byte{"HLAfederate": catalogRPCHandle(f.target.FederateHandle), "HLAreportingState": catalogRPCInt(1)}))
			receivers := []uint64{f.manager.FederateHandle, matched.FederateHandle, excluded.FederateHandle}
			f.publishMarker(receivers)
			model, err := f.r.foms.Get(f.ctx, momFullFederation)
			f.must(err)
			regulationClass, ok := model.LookupInteractionClass(mom.ServiceClassPrefix + "HLAenableTimeRegulation")
			if !ok {
				t.Fatal("standard regulation service absent")
			}
			_, err = rtiv1.NewDeclarationServiceClient(f.conn).PublishInteractionClass(f.managerCtx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, InteractionClassHandle: uint64(regulationClass)})
			f.must(err)
			f.must(f.sendMOM(mom.ServiceClassPrefix+"HLAenableAsynchronousDelivery", map[string][]byte{"HLAfederate": catalogRPCHandle(f.target.FederateHandle)}))
			f.marker("exception-baseline")
			for _, h := range receivers {
				f.barrier("exception-baseline", h, mom.ClassReportServiceInvocation, "")
			}
			for _, malformed := range []bool{false, true} {
				name := "servicePrecondition"
				if malformed {
					name = "malformedParameter"
				}
				t.Run(name, func(t *testing.T) {
					f.t = t
					before := f.state()
					var err error
					if malformed {
						err = f.sendMOM(mom.ServiceClassPrefix+"HLAenableTimeRegulation", map[string][]byte{"HLAfederate": catalogRPCHandle(f.target.FederateHandle), "HLAlookahead": {0}})
					} else {
						_, err = rtiv1.NewObjectServiceClient(f.conn).SendInteraction(f.managerCtx, f.request)
					}
					if err == nil || status.Code(err) == codes.DeadlineExceeded {
						t.Fatalf("negative service not rejected synchronously: %v", err)
					}
					if !bytes.Equal(before, f.state()) {
						t.Fatal("failed service changed authoritative state")
					}
					f.marker(name)
					for _, h := range receivers {
						seen := map[string]int{}
						f.callback(h, func(e *rtiv1.FederateEvent) bool {
							if r := e.GetReceive(); r != nil && bytes.Equal(r.UserSuppliedTag, []byte(name)) {
								return true
							}
							class, params := f.reportParams(e)
							class = strings.TrimPrefix(class, "HLAinteractionRoot.")
							if !bytes.Equal(params["HLAfederate"], catalogRPCHandle(f.target.FederateHandle)) {
								return false
							}
							switch class {
							case mom.ClassReportServiceInvocation:
								if catalogRPCString(t, params["HLAservice"]) != momServiceEnableAsynchronousDelivery {
									return false
								}
								if !bytes.Equal(params["HLAsuccessIndicator"], catalogRPCInt(0)) || catalogRPCString(t, params["HLAexception"]) == "" {
									t.Fatal("failed actual service reported success")
								}
							case mom.ClassReportException:
								if catalogRPCString(t, params["HLAservice"]) != momServiceEnableAsynchronousDelivery || catalogRPCString(t, params["HLAexception"]) == "" {
									t.Fatal("exception report lost actual exception")
								}
							case mom.ClassReportMOMException:
								want := catalogRPCInt(0)
								if malformed {
									want = catalogRPCInt(1)
								}
								if !bytes.Equal(params["HLAparameterError"], want) || catalogRPCString(t, params["HLAexception"]) == "" {
									t.Fatal("MOM parameter failure classification incorrect")
								}
							default:
								return false
							}
							seen[class]++
							return false
						})
						for _, class := range []string{mom.ClassReportServiceInvocation, mom.ClassReportException, mom.ClassReportMOMException} {
							want := 0
							if h != excluded.FederateHandle && class == mom.ClassReportMOMException || !malformed && (class == mom.ClassReportException && h != excluded.FederateHandle || class == mom.ClassReportServiceInvocation && h == f.manager.FederateHandle) {
								want = 1
							}
							if seen[class] != want {
								t.Fatalf("recipient=%d class=%s reports=%d want=%d", h, class, seen[class], want)
							}
						}
					}
				})
				f.t = t
			}
		})
	}
}
