package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// The literal inventory deliberately does not derive coverage from dispatch.
var momFullServices = []string{
	momServiceResignFederationExecution, momServiceSynchronizationPointAchieved, momServiceFederateSaveBegun, momServiceFederateSaveComplete, momServiceFederateRestoreComplete,
	momServicePublishObjectClassAttributes, momServiceUnpublishObjectClassAttributes, momServicePublishInteractionClass, momServiceUnpublishInteractionClass,
	momServiceSubscribeObjectClassAttributes, momServiceUnsubscribeObjectClassAttributes, momServiceSubscribeInteractionClass, momServiceUnsubscribeInteractionClass,
	momServiceDeleteObjectInstance, momServiceLocalDeleteObjectInstance, momServiceRequestAttributeTransportationTypeChange, momServiceRequestInteractionTransportationTypeChange,
	momServiceUnconditionalAttributeOwnershipDivestiture, momServiceEnableTimeRegulation, momServiceDisableTimeRegulation, momServiceEnableTimeConstrained, momServiceDisableTimeConstrained,
	momServiceTimeAdvanceRequest, momServiceTimeAdvanceRequestAvailable, momServiceNextMessageRequest, momServiceNextMessageRequestAvailable, momServiceFlushQueueRequest,
	momServiceEnableAsynchronousDelivery, momServiceDisableAsynchronousDelivery, momServiceModifyLookahead, momServiceChangeAttributeOrderType, momServiceChangeInteractionOrderType,
}

type momFullFixture struct {
	t                          *testing.T
	r                          *rtid
	conn                       *grpc.ClientConn
	ctx, managerCtx, targetCtx context.Context
	manager, target            *rtiv1.JoinFederationResponse
	factory, service           string
	command                    mom.MOMServiceCommand
	request                    *rtiv1.SendInteractionRequest
	streams                    map[uint64]grpc.ServerStreamingClient[rtiv1.FederateEventBatch]
	auths                      map[uint64]context.Context
	pending                    map[uint64][]*rtiv1.FederateEvent
}

const momFullFederation = "mom-full-production"

func newMOMFullFixture(t *testing.T, factory, service string) *momFullFixture {
	t.Helper()
	r, conn, _ := integerProductionServer(t, t.TempDir(), 241)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	f := &momFullFixture{t: t, r: r, conn: conn, ctx: ctx, factory: factory, service: service, streams: make(map[uint64]grpc.ServerStreamingClient[rtiv1.FederateEventBatch]), auths: make(map[uint64]context.Context)}
	module := momCompletionReviewModule(t, "Item")
	module.Xml = bytes.ReplaceAll(module.Xml, []byte("<transportation>HLAbestEffort</transportation>"), []byte("<transportation>HLAreliable</transportation>"))
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: momFullFederation, LogicalTimeImplementationName: factory, FomModules: []*rtiv1.FOMModule{module}})
	f.must(err)
	f.manager = momCompletionReviewJoin(ctx, t, conn, momFullFederation, "manager", "", created.FederationGeneration)
	f.target = momCompletionReviewJoin(ctx, t, conn, momFullFederation, "target", "", created.FederationGeneration)
	f.managerCtx = metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(f.manager.SessionToken))
	f.targetCtx = metadata.AppendToOutgoingContext(ctx, session.MetadataKey, string(f.target.SessionToken))
	model, err := r.foms.Get(ctx, momFullFederation)
	f.must(err)
	cls, ok := model.LookupObjectClass("Item")
	if !ok {
		t.Fatal("joined FOM object absent")
	}
	attr, ok := model.LookupAttribute(cls, "Inherited")
	if !ok {
		t.Fatal("joined FOM attribute absent")
	}
	interaction, ok := model.LookupInteractionClass("Event")
	if !ok {
		t.Fatal("joined FOM interaction absent")
	}
	f.command = mom.MOMServiceCommand{Service: service, Target: core.FederateHandle(f.target.FederateHandle), ObjectClass: cls, InteractionClass: interaction, Attributes: []core.AttributeHandle{attr}, AttributesPresent: true, Label: "phase", Success: true, TransportationName: bestEffortTransportationName, Order: core.OrderTimestamp}
	f.command.Time, f.command.Lookahead = new([8]byte), new([8]byte)
	if factory == integer64TimeName {
		binary.BigEndian.PutUint64(f.command.Time[:], uint64(1<<53)+1)
		binary.BigEndian.PutUint64(f.command.Lookahead[:], 1)
	} else {
		binary.BigEndian.PutUint64(f.command.Time[:], math.Float64bits(2))
		binary.BigEndian.PutUint64(f.command.Lookahead[:], math.Float64bits(1))
	}
	for _, h := range []*rtiv1.JoinFederationResponse{f.manager, f.target} {
		auth := f.managerCtx
		if h == f.target {
			auth = f.targetCtx
		}
		stream, err := rtiv1.NewStreamServiceClient(conn).EventBatches(auth, &rtiv1.EventsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: h.FederateHandle})
		f.must(err)
		ready, err := stream.Recv()
		f.must(err)
		if !ready.Ready {
			t.Fatal("production callback stream not ready")
		}
		f.streams[h.FederateHandle] = stream
		f.auths[h.FederateHandle] = auth
	}
	reportClass, ok := model.LookupInteractionClass(mom.ClassReportServiceInvocation)
	if !ok {
		t.Fatal("standard service report class absent")
	}
	_, err = rtiv1.NewDeclarationServiceClient(conn).SubscribeInteractionClass(f.managerCtx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, InteractionClassHandle: uint64(reportClass)})
	f.must(err)
	class, ok := model.LookupInteractionClass(mom.ServiceClassPrefix + "HLA" + service)
	if !ok {
		t.Fatal("standard MIM service absent", service)
	}
	_, err = rtiv1.NewDeclarationServiceClient(conn).PublishInteractionClass(f.managerCtx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, InteractionClassHandle: uint64(class)})
	f.must(err)
	f.request = &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, InteractionClassHandle: uint64(class), Parameters: map[uint64][]byte{}}
	f.refreshArguments()
	f.must(f.sendMOM(mom.ClassSetServiceReporting, map[string][]byte{"HLAfederate": catalogRPCHandle(f.target.FederateHandle), "HLAreportingState": catalogRPCInt(1)}))
	return f
}

func (f *momFullFixture) sendMOM(className string, params map[string][]byte) error {
	f.t.Helper()
	model, err := f.r.foms.Get(f.ctx, momFullFederation)
	if err != nil {
		return err
	}
	cls, ok := model.LookupInteractionClass(className)
	if !ok {
		f.t.Fatal("MIM request absent", className)
	}
	_, err = rtiv1.NewDeclarationServiceClient(f.conn).PublishInteractionClass(f.managerCtx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, InteractionClassHandle: uint64(cls)})
	if err != nil {
		return err
	}
	request := &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, InteractionClassHandle: uint64(cls), Parameters: map[uint64][]byte{}}
	for name, value := range params {
		h, ok := model.LookupParameter(cls, name)
		if !ok {
			f.t.Fatal("MIM parameter absent", name)
		}
		request.Parameters[uint64(h)] = value
	}
	_, err = rtiv1.NewObjectServiceClient(f.conn).SendInteraction(f.managerCtx, request)
	return err
}

func (f *momFullFixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *momFullFixture) refreshArguments() {
	f.t.Helper()
	model, err := f.r.foms.Get(f.ctx, momFullFederation)
	f.must(err)
	args := momCatalogRPCArguments(f.t, &momExecutionFixture{command: f.command})
	if f.service == momServiceModifyLookahead {
		args["HLAlookahead"] = catalogRPCHandle(binary.BigEndian.Uint64(f.command.Lookahead[:]))
	}
	args["HLAfederate"] = catalogRPCHandle(f.target.FederateHandle)
	for name, value := range args {
		h, ok := model.LookupParameter(core.InteractionClassHandle(f.request.InteractionClassHandle), name)
		if !ok {
			f.t.Fatal("MIM parameter missing", name)
		}
		f.request.Parameters[uint64(h)] = value
	}
}

func (f *momFullFixture) callback(h uint64, match func(*rtiv1.FederateEvent) bool) *rtiv1.FederateEvent {
	return f.callbackAtEntry(h, match, nil)
}

func (f *momFullFixture) callbackAtEntry(h uint64, match func(*rtiv1.FederateEvent) bool, entryAction func(*rtiv1.FederateEvent)) *rtiv1.FederateEvent {
	f.t.Helper()
	auth := f.auths[h]
	if f.pending == nil {
		f.pending = make(map[uint64][]*rtiv1.FederateEvent)
	}
	for {
		if len(f.pending[h]) == 0 {
			batch, err := f.streams[h].Recv()
			f.must(err)
			f.pending[h] = batch.Events
			if len(f.pending[h]) == 0 {
				continue
			}
		}
		event := f.pending[h][0]
		f.pending[h] = f.pending[h][1:]
		if (event.GetReflect() != nil || event.GetReceive() != nil) && len(event.CallbackReceipt) != 32 {
			f.t.Fatal("data/report callback missing mandatory authenticated receipt")
		}
		matched := false
		if len(event.CallbackReceipt) != 0 {
			report := &rtiv1.ReportCallbackInvocationRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: h, CallbackReceipt: event.CallbackReceipt, Success: true, InvocationEntry: true}
			_, err := rtiv1.NewSupportServiceClient(f.conn).ReportCallbackInvocation(auth, report)
			f.must(err)
			matched = match(event)
			if matched && entryAction != nil {
				entryAction(event)
			}
			report.InvocationEntry = false
			_, err = rtiv1.NewSupportServiceClient(f.conn).ReportCallbackInvocation(auth, report)
			f.must(err)
		}
		if len(event.CallbackReceipt) == 0 {
			matched = match(event)
			if matched && entryAction != nil {
				entryAction(event)
			}
		}
		if matched {
			return event
		}
	}
}

func (f *momFullFixture) prepare() {
	f.t.Helper()
	d := rtiv1.NewDeclarationServiceClient(f.conn)
	o := rtiv1.NewObjectServiceClient(f.conn)
	s := f.service
	objectService := slices.Contains([]string{momServiceUnpublishObjectClassAttributes, momServiceDeleteObjectInstance, momServiceLocalDeleteObjectInstance, momServiceRequestAttributeTransportationTypeChange, momServiceUnconditionalAttributeOwnershipDivestiture, momServiceChangeAttributeOrderType}, s)
	if objectService {
		publisher, auth := f.target, f.targetCtx
		if s == momServiceLocalDeleteObjectInstance {
			publisher, auth = f.manager, f.managerCtx
			_, err := d.SubscribeObjectClassAttributes(f.targetCtx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, ObjectClassHandle: uint64(f.command.ObjectClass), AttributeHandles: []uint64{uint64(f.command.Attributes[0])}})
			f.must(err)
		}
		attrs := []uint64{uint64(f.command.Attributes[0])}
		if s == momServiceDeleteObjectInstance || s == momServiceRequestAttributeTransportationTypeChange {
			model, err := f.r.foms.Get(f.ctx, momFullFederation)
			f.must(err)
			privilege, ok := model.LookupAttribute(f.command.ObjectClass, "HLAprivilegeToDeleteObject")
			if !ok {
				f.t.Fatal("delete privilege missing")
			}
			attrs = append(attrs, uint64(privilege))
		}
		_, err := d.PublishObjectClassAttributes(auth, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: publisher.FederateHandle, ObjectClassHandle: uint64(f.command.ObjectClass), AttributeHandles: attrs})
		f.must(err)
		registered, err := o.RegisterObjectInstance(auth, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: publisher.FederateHandle, ObjectClassHandle: uint64(f.command.ObjectClass)})
		f.must(err)
		f.command.Object = core.ObjectHandle(registered.ObjectHandle)
		if s == momServiceLocalDeleteObjectInstance {
			f.callback(f.target.FederateHandle, func(e *rtiv1.FederateEvent) bool { return e.GetDiscover() != nil })
			_, err = d.UnsubscribeObjectClassAttributes(f.targetCtx, &rtiv1.UnsubObjAttrsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, ObjectClassHandle: uint64(f.command.ObjectClass), AttributeHandles: []uint64{uint64(f.command.Attributes[0])}})
			f.must(err)
		}
	}
	switch s {
	case momServiceUnpublishInteractionClass, momServiceRequestInteractionTransportationTypeChange, momServiceChangeInteractionOrderType:
		_, err := d.PublishInteractionClass(f.targetCtx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, InteractionClassHandle: uint64(f.command.InteractionClass)})
		f.must(err)
	case momServiceUnsubscribeObjectClassAttributes:
		_, err := d.SubscribeObjectClassAttributes(f.targetCtx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, ObjectClassHandle: uint64(f.command.ObjectClass), AttributeHandles: []uint64{uint64(f.command.Attributes[0])}})
		f.must(err)
	case momServiceUnsubscribeInteractionClass:
		_, err := d.SubscribeInteractionClass(f.targetCtx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, InteractionClassHandle: uint64(f.command.InteractionClass)})
		f.must(err)
	case momServiceDisableTimeRegulation, momServiceModifyLookahead:
		req := &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle, Lookahead: 1}
		if f.factory == integer64TimeName {
			req.Lookahead, req.LookaheadValue = 0, integerWireTime(f.t, 1)
		}
		_, err := rtiv1.NewTimeServiceClient(f.conn).EnableTimeRegulation(f.targetCtx, req)
		f.must(err)
		f.callback(f.target.FederateHandle, func(e *rtiv1.FederateEvent) bool { return e.GetTimeRegulationEnabled() != nil })
		if f.factory == integer64TimeName {
			binary.BigEndian.PutUint64(f.command.Lookahead[:], uint64(1<<53)+1)
		} else {
			binary.BigEndian.PutUint64(f.command.Lookahead[:], math.Float64bits(2))
		}
	case momServiceDisableTimeConstrained:
		_, err := rtiv1.NewTimeServiceClient(f.conn).EnableTimeConstrained(f.targetCtx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle})
		f.must(err)
		f.callback(f.target.FederateHandle, func(e *rtiv1.FederateEvent) bool { return e.GetTimeConstrainedEnabled() != nil })
	case momServiceDisableAsynchronousDelivery:
		_, err := rtiv1.NewTimeServiceClient(f.conn).EnableAsynchronousDelivery(f.targetCtx, &rtiv1.EnableAsynchronousDeliveryRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle})
		f.must(err)
	case momServiceSynchronizationPointAchieved:
		_, err := rtiv1.NewSyncServiceClient(f.conn).RegisterFederationSynchronizationPoint(f.managerCtx, &rtiv1.RegisterSyncPointRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, Label: "phase", RequiredFederates: []uint64{f.target.FederateHandle}})
		f.must(err)
		f.callback(f.target.FederateHandle, func(e *rtiv1.FederateEvent) bool { return e.GetSyncAnnounced() != nil })
	case momServiceFederateSaveBegun, momServiceFederateSaveComplete, momServiceFederateRestoreComplete:
		f.prepareCheckpoint()
	}
	f.refreshArguments()
}

func (f *momFullFixture) prepareCheckpoint() {
	saves := rtiv1.NewSavepointServiceClient(f.conn)
	_, err := saves.RequestFederationSave(f.managerCtx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, Label: testCheckpointLabel})
	f.must(err)
	if f.service != momServiceFederateSaveBegun {
		_, err = saves.FederateSaveBegun(f.targetCtx, &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle})
		f.must(err)
	}
	if f.service == momServiceFederateRestoreComplete {
		_, err = saves.FederateSaveComplete(f.targetCtx, &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.target.FederateHandle})
		f.must(err)
		_, err = saves.FederateSaveBegun(f.managerCtx, &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle})
		f.must(err)
		_, err = saves.FederateSaveComplete(f.managerCtx, &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle})
		f.must(err)
		_, err = saves.RequestFederationRestore(f.managerCtx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: momFullFederation, FederateHandle: f.manager.FederateHandle, Label: testCheckpointLabel})
		f.must(err)
	}
}

func (f *momFullFixture) state() []byte {
	fed := core.FederationName(momFullFederation)
	d := f.r.declMgr.(*declaration.Manager).ExportState(fed)
	var objects, times any
	if f.factory == integer64TimeName {
		objects, times = f.r.integerObjects.ExportState(fed), f.r.integerTime.Metrics(fed, f.command.Target)
	} else {
		objects, times = f.r.objReg.ExportState(fed), f.r.timeMgr.Snapshot(fed).Federates
	}
	state, err := json.Marshal([]any{d, objects, times, f.asynchronous(), f.r.ownMgr.(*ownership.Manager).ExportState(fed), f.r.fedMgr.MembersOf(fed), f.r.syncMgr.Snapshot(fed)})
	f.must(err)
	return state
}

func TestMOMFullCatalogActualServer(t *testing.T) {
	if len(momFullServices) != 32 {
		t.Fatal("literal catalog must contain 32 leaves")
	}
	for _, factory := range []string{float64TimeName, integer64TimeName} {
		for _, service := range momFullServices {
			t.Run(factory+"/"+service, func(t *testing.T) {
				f := newMOMFullFixture(t, factory, service)
				f.prepare()
				for _, negative := range []string{"unsigned", "foreignToken", "unknownTarget", "undefinedParameter"} {
					t.Run(negative, func(t *testing.T) {
						parent := f.t
						f.t = t
						defer func() { f.t = parent }()
						before := f.state()
						req := proto.Clone(f.request).(*rtiv1.SendInteractionRequest)
						auth := f.managerCtx
						switch negative {
						case "unsigned":
							auth = f.ctx
						case "foreignToken":
							auth = f.targetCtx
						case "unknownTarget":
							model, err := f.r.foms.Get(f.ctx, momFullFederation)
							f.must(err)
							h, _ := model.LookupParameter(core.InteractionClassHandle(req.InteractionClassHandle), "HLAfederate")
							req.Parameters[uint64(h)] = catalogRPCHandle(999999)
						case "undefinedParameter":
							req.Parameters[999999] = []byte{1}
						}
						_, err := rtiv1.NewObjectServiceClient(f.conn).SendInteraction(auth, req)
						if err == nil || status.Code(err) == codes.DeadlineExceeded {
							t.Fatalf("not rejected synchronously: %v", err)
						}
						if negative == "unsigned" && status.Code(err) != codes.Unauthenticated {
							t.Fatalf("unsigned status=%v", err)
						}
						if !bytes.Equal(before, f.state()) {
							t.Fatal("rejected service mutated authoritative state")
						}
					})
				}
				t.Run("positive", func(t *testing.T) {
					f.t = t
					_, err := rtiv1.NewObjectServiceClient(f.conn).SendInteraction(f.managerCtx, f.request)
					f.must(err)
					f.verify()
					f.verifyServiceReport()
				})
			})
		}
	}
}

func (f *momFullFixture) verify() {
	f.t.Helper()
	fed, h, c := core.FederationName(momFullFederation), f.command.Target, f.command
	d := f.r.declMgr.(*declaration.Manager)
	var objectState any
	var known bool
	var class core.ObjectClassHandle
	if f.factory == integer64TimeName {
		objectState = f.r.integerObjects.ExportState(fed)
		class = f.r.integerObjects.ClassOf(fed, c.Object)
		_, _, known = f.r.integerObjects.LookupKnownObjectInstance(fed, h, c.Object)
	} else {
		objectState = f.r.objReg.ExportState(fed)
		class = f.r.objReg.ClassOf(fed, c.Object)
		_, _, known = f.r.objReg.LookupKnownObjectInstance(fed, h, c.Object)
	}
	switch f.service {
	case momServiceResignFederationExecution:
		if slices.Contains(f.r.fedMgr.MembersOf(fed), h) || !slices.Contains(f.r.fedMgr.MembersOf(fed), core.FederateHandle(f.manager.FederateHandle)) {
			f.t.Fatal("resign roster incorrect")
		}
	case momServiceSynchronizationPointAchieved:
		state := f.r.syncMgr.Snapshot(fed)
		if len(state) != 1 || state[0].State != core.SyncPointStateAchieved || !slices.Equal(state[0].AchievedHandles, []core.FederateHandle{h}) {
			f.t.Fatal("sync not achieved", state)
		}
		f.callback(uint64(h), func(e *rtiv1.FederateEvent) bool { return e.GetSyncSynchronized() != nil })
	case momServicePublishObjectClassAttributes, momServiceUnpublishObjectClassAttributes:
		want := []core.AttributeHandle{c.Attributes[0]}
		if f.service == momServiceUnpublishObjectClassAttributes {
			want = nil
		} else {
			model, err := f.r.foms.Get(f.ctx, fed)
			f.must(err)
			privilege, ok := model.LookupAttribute(c.ObjectClass, "HLAprivilegeToDeleteObject")
			if !ok {
				f.t.Fatal("delete privilege missing from model")
			}
			want = append(slices.Clone(want), privilege)
			slices.Sort(want)
			want = slices.Compact(want)
		}
		if !slices.Equal(d.PublishedObjectAttributes(fed, h, c.ObjectClass), want) {
			f.t.Fatal("publication mismatch")
		}
	case momServicePublishInteractionClass, momServiceUnpublishInteractionClass:
		published := slices.Contains(d.InteractionPublishersFor(f.ctx, fed, c.InteractionClass), h)
		if published != (f.service == momServicePublishInteractionClass) {
			f.t.Fatal("interaction publication mismatch")
		}
	case momServiceSubscribeObjectClassAttributes, momServiceUnsubscribeObjectClassAttributes:
		subscribed := slices.Contains(d.SubscribersFor(f.ctx, fed, c.ObjectClass, c.Attributes), h)
		if subscribed != (f.service == momServiceSubscribeObjectClassAttributes) {
			f.t.Fatal("object subscription mismatch")
		}
	case momServiceSubscribeInteractionClass, momServiceUnsubscribeInteractionClass:
		subscribed := slices.Contains(d.InteractionSubscribersFor(f.ctx, fed, c.InteractionClass), h)
		if subscribed != (f.service == momServiceSubscribeInteractionClass) {
			f.t.Fatal("interaction subscription mismatch")
		}
	case momServiceDeleteObjectInstance:
		if class != 0 {
			f.t.Fatal("deleted object remains")
		}
	case momServiceLocalDeleteObjectInstance:
		if known || class == 0 {
			f.t.Fatal("local deletion must remove knowledge, not object")
		}
	case momServiceUnconditionalAttributeOwnershipDivestiture:
		if f.r.ownMgr.IsOwnedBy(fed, h, c.Object, c.Attributes[0]) {
			f.t.Fatal("ownership retained")
		}
	case momServiceChangeAttributeOrderType, momServiceChangeInteractionOrderType:
		var attrs []object.AttributeOrderState
		var interactions []object.InteractionOrderState
		switch st := objectState.(type) {
		case *object.RegistryStateOf[int64]:
			attrs, interactions = st.AttributeOrders, st.InteractionOrders
		case *object.RegistryStateOf[core.LogicalTime]:
			attrs, interactions = st.AttributeOrders, st.InteractionOrders
		}
		if f.service == momServiceChangeAttributeOrderType && !slices.Contains(attrs, object.AttributeOrderState{Object: c.Object, Attribute: c.Attributes[0], Order: core.OrderTimestamp}) {
			f.t.Fatal("attribute order unchanged")
		}
		if f.service == momServiceChangeInteractionOrderType && !slices.Contains(interactions, object.InteractionOrderState{Publisher: h, Class: c.InteractionClass, Order: core.OrderTimestamp}) {
			f.t.Fatal("interaction order unchanged")
		}
	case momServiceFederateSaveBegun, momServiceFederateSaveComplete, momServiceFederateRestoreComplete:
		f.verifyCheckpoint()
	case momServiceRequestAttributeTransportationTypeChange, momServiceRequestInteractionTransportationTypeChange:
		// The dedicated production test verifies deferred activation, entry,
		// foreign activation and duplicate completion for both service leaves.
		event := f.callback(uint64(h), func(e *rtiv1.FederateEvent) bool { return e.GetTransportationChangeConfirmed() != nil })
		if len(event.GetTransportationChangeConfirmed().Ticket) != 32 || len(event.CallbackReceipt) != 32 {
			f.t.Fatal("mandatory confirmation absent")
		}
	default:
		if !slices.Contains(momFullServices[18:30], f.service) {
			f.t.Fatal("no literal positive oracle", f.service)
		}
		f.verifyTime()
	}
}

func (f *momFullFixture) reportParams(event *rtiv1.FederateEvent) (string, map[string][]byte) {
	if event.GetReceive() == nil {
		return "", nil
	}
	model, err := f.r.foms.Get(f.ctx, momFullFederation)
	f.must(err)
	names := model.(core.FOMHandleNameLookup)
	cls := core.InteractionClassHandle(event.GetReceive().InteractionClassHandle)
	className, ok := names.InteractionClassName(cls)
	if !ok {
		f.t.Fatal("report class unknown")
	}
	params := make(map[string][]byte)
	for h, value := range event.GetReceive().Parameters {
		name, ok := names.ParameterName(cls, core.ParameterHandle(h))
		if !ok {
			f.t.Fatal("report parameter unknown")
		}
		params[name] = value
	}
	return className, params
}

func (f *momFullFixture) verifyServiceReport() {
	f.callback(f.manager.FederateHandle, func(e *rtiv1.FederateEvent) bool {
		class, params := f.reportParams(e)
		if class != mom.ClassReportServiceInvocation && class != "HLAinteractionRoot."+mom.ClassReportServiceInvocation {
			return false
		}
		if !bytes.Equal(params["HLAfederate"], catalogRPCHandle(f.target.FederateHandle)) || catalogRPCString(f.t, params["HLAservice"]) != f.service {
			return false
		}
		if !bytes.Equal(params["HLAsuccessIndicator"], catalogRPCInt(1)) || catalogRPCString(f.t, params["HLAexception"]) != "" || len(params["HLAserialNumber"]) != 4 {
			f.t.Fatal("service report does not describe actual successful invocation", params)
		}
		return true
	})
}

func (f *momFullFixture) verifyTime() {
	fed, h := core.FederationName(momFullFederation), f.command.Target
	var regulating, constrained, async bool
	var current, lookahead []byte
	if f.factory == integer64TimeName {
		m := f.r.integerTime.Metrics(fed, h)
		regulating, constrained, async = m.Regulating, m.Constrained, m.AsynchronousDelivery
		current = catalogRPCHandle(uint64(m.CurrentTime))
		if m.Lookahead != nil {
			lookahead = catalogRPCHandle(uint64(*m.Lookahead))
		}
	} else {
		for _, m := range f.r.timeMgr.Snapshot(fed).Federates {
			if m.Handle != h {
				continue
			}
			regulating, constrained = m.Regulating, m.Constrained
			current, lookahead = catalogRPCHandle(math.Float64bits(float64(m.CurrentTime))), catalogRPCHandle(math.Float64bits(float64(m.Lookahead)))
		}
		async = f.asynchronous()
	}
	switch f.service {
	case momServiceEnableTimeRegulation, momServiceDisableTimeRegulation:
		if regulating != (f.service == momServiceEnableTimeRegulation) {
			f.t.Fatal("regulation mismatch")
		}
	case momServiceEnableTimeConstrained, momServiceDisableTimeConstrained:
		if constrained != (f.service == momServiceEnableTimeConstrained) {
			f.t.Fatal("constraint mismatch")
		}
	case momServiceEnableAsynchronousDelivery, momServiceDisableAsynchronousDelivery:
		if async != (f.service == momServiceEnableAsynchronousDelivery) {
			f.t.Fatal("async mismatch")
		}
	case momServiceModifyLookahead:
		if !bytes.Equal(lookahead[4:], f.command.Lookahead[:]) {
			f.t.Fatal("exact lookahead mismatch")
		}
	case momServiceTimeAdvanceRequest, momServiceTimeAdvanceRequestAvailable, momServiceNextMessageRequest, momServiceNextMessageRequestAvailable, momServiceFlushQueueRequest:
		if !bytes.Equal(current[4:], f.command.Time[:]) {
			f.t.Fatalf("exact granted time mismatch %x want %x", current[4:], f.command.Time[:])
		}
		f.callback(uint64(h), func(e *rtiv1.FederateEvent) bool { return e.GetGrant() != nil })
	default:
		f.t.Fatal("missing time oracle")
	}
}

func (f *momFullFixture) asynchronous() bool {
	metrics, err := f.r.momMgr.(*mom.Manager).FederateMetricValues(f.ctx, momFullFederation, f.command.Target)
	f.must(err)
	value := metrics.Values["HLAasynchronousDelivery"]
	if len(value) != 4 {
		f.t.Fatalf("asynchronous metric unavailable: %v", metrics.Unavailable["HLAasynchronousDelivery"])
	}
	return binary.BigEndian.Uint32(value) != 0
}

func (f *momFullFixture) verifyCheckpoint() {
	saves := rtiv1.NewSavepointServiceClient(f.conn)
	if f.service == momServiceFederateRestoreComplete {
		state, err := saves.QueryRestoreState(f.ctx, &rtiv1.QueryRestoreStateRequest{WireVersion: 1, FederationName: momFullFederation, Label: testCheckpointLabel})
		f.must(err)
		for _, participant := range state.FederateStatuses {
			if participant.PreRestoreHandle == f.target.FederateHandle && participant.Status == rtiv1.FederateRestoreProgress_FEDERATE_RESTORE_PROGRESS_WAITING_FOR_FEDERATION_TO_RESTORE {
				return
			}
		}
		f.t.Fatal("target restore completion not recorded", state)
	} else {
		state, err := saves.QuerySaveState(f.ctx, &rtiv1.QuerySaveStateRequest{WireVersion: 1, FederationName: momFullFederation, Label: testCheckpointLabel})
		f.must(err)
		want := rtiv1.FederateSaveProgress_FEDERATE_SAVE_PROGRESS_SAVING
		if f.service == momServiceFederateSaveComplete {
			want = rtiv1.FederateSaveProgress_FEDERATE_SAVE_PROGRESS_WAITING_FOR_FEDERATION_TO_SAVE
		}
		for _, participant := range state.FederateStatuses {
			if participant.FederateHandle == f.target.FederateHandle && participant.Status == want {
				return
			}
		}
		f.t.Fatal("target save transition not recorded", state)
	}
}
