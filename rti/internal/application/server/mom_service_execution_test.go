package server

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle/storage"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

var momExecutionServices = strings.Fields(`resignFederationExecution synchronizationPointAchieved federateSaveBegun federateSaveComplete federateRestoreComplete publishObjectClassAttributes unpublishObjectClassAttributes publishInteractionClass unpublishInteractionClass subscribeObjectClassAttributes unsubscribeObjectClassAttributes subscribeInteractionClass unsubscribeInteractionClass deleteObjectInstance localDeleteObjectInstance requestAttributeTransportationTypeChange requestInteractionTransportationTypeChange unconditionalAttributeOwnershipDivestiture enableTimeRegulation disableTimeRegulation enableTimeConstrained disableTimeConstrained timeAdvanceRequest timeAdvanceRequestAvailable nextMessageRequest nextMessageRequestAvailable flushQueueRequest enableAsynchronousDelivery disableAsynchronousDelivery modifyLookahead changeAttributeOrderType changeInteractionOrderType`)

type momExecutionFixture struct {
	deps           momCatalogDependencies
	executor       mom.MOMServiceExecutor
	sender, target core.FederateHandle
	command        mom.MOMServiceCommand
	time           *timepkg.Manager
	integerTime    *timepkg.ManagerOf[int64]
	save           *savepoint.Manager
	confirmed      int
	prepared       core.TransportationChange
	dispatcher     *mom.Dispatcher
	out            *momCatalogOutbox
}

func newMOMExecutionFixture(t *testing.T, service string) *momExecutionFixture {
	t.Helper()
	deps, fom, sender, target, out := newMOMCatalogDeps(t)
	manager, err := mom.New(mom.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetRuntimeMetricsEnabled(true); err != nil {
		t.Fatal(err)
	}
	deps.Traffic = mom.NewTrafficAccumulator()
	if err := manager.SetTrafficAccumulator(deps.Traffic); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{sender, target} {
		if err := manager.FederateJoined(context.Background(), "catalog-provider", h, "member", "test"); err != nil {
			t.Fatal(err)
		}
	}
	deps.Manager = manager
	f := &momExecutionFixture{deps: deps, sender: sender, target: target, out: out, dispatcher: mom.NewDispatcher(deps.Manager)}
	ctx := context.Background()
	fed := core.FederationName("catalog-provider")
	clock := core.NewManualClock(time.Unix(0, 0))
	f.time, err = timepkg.New(timepkg.Options{Clock: clock, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	f.integerTime, err = timepkg.NewOf[int64](timepkg.OptionsOf[int64]{Clock: clock, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	var reg *object.Registry
	owners, err := ownership.New(ownership.Options{Outbox: out,
		PrepareKnownAcquisition: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
			return reg.PrepareKnownOwnershipAcquisition(ctx, fed, h, obj, attrs)
		},
		PrepareKnownOperation: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, func(), error) {
			return reg.PrepareKnownOwnershipOperation(ctx, fed, h, obj, attrs)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reg, err = object.New(object.Options{Clock: clock, Outbox: out, FOMs: deps.FOMs, Declarations: deps.Declarations, Ownership: owners, ManagementDispatch: f.dispatcher, OnRegister: func(fed core.FederationName, h core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
		owners.RegisterInitialOwnership(fed, h, obj, attrs)
	}})
	if err != nil {
		t.Fatal(err)
	}
	integerObjects, err := object.NewOf[int64](object.OptionsOf[int64]{Clock: clock, Outbox: out, FOMs: deps.FOMs, Declarations: deps.Declarations})
	if err != nil {
		t.Fatal(err)
	}
	f.deps.Objects, f.deps.Ownership = reg, owners
	deps.Declarations.SetObjectUnpublication(reg, owners)
	deps.Declarations.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)
	store, err := storage.NewFSStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.save, err = savepoint.New(savepoint.Options{Outbox: out, BundleStore: store, Members: deps.Federations.MembersOf})
	if err != nil {
		t.Fatal(err)
	}
	e, err := newMOMServiceExecutor(momServiceExecutionDependencies{Federations: deps.Federations, Declarations: deps.Declarations, FOMs: deps.FOMs, Ownership: owners, Synchronization: deps.Synchronization, FloatTime: f.time, IntegerTime: f.integerTime, FloatObjects: reg, IntegerObjects: integerObjects, FloatSave: f.save,
		Admission: func(ctx context.Context, fed core.FederationName, sender, target core.FederateHandle, _ string) (func(), error) {
			if _, err := deps.Federations.MemberIdentity(fed, sender); err != nil {
				return nil, err
			}
			if _, err := deps.Federations.MemberIdentity(fed, target); err != nil {
				return nil, err
			}
			return func() {}, ctx.Err()
		},
		Resign: deps.Federations.ResignFederationIdentity,
		TransportationConfirmed: func(ctx context.Context, fed core.FederationName, target core.FederateHandle, command mom.MOMServiceCommand, transport core.TransportType) error {
			identity, err := deps.Federations.MemberIdentity(fed, target)
			if err != nil {
				return err
			}
			change, err := prepareMOMTransportationChange(ctx, reg, core.TransportationScope{Member: identity}, command, transport)
			if err != nil {
				return err
			}
			f.prepared = change
			f.confirmed++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.executor = e
	f.dispatcher.SetServiceExecutor(e)
	f.dispatcher.SetEmitter(mom.NewProductionEmitter(out))
	cls, _ := fom.LookupObjectClass("Vehicle")
	attr, _ := fom.LookupAttribute(cls, "value")
	privilege, _ := fom.LookupAttribute(cls, "HLAprivilegeToDeleteObject")
	interaction, _ := fom.LookupInteractionClass("Pulse")
	f.command = mom.MOMServiceCommand{Service: service, Target: target, ObjectClass: cls, InteractionClass: interaction, Attributes: []core.AttributeHandle{attr}, AttributesPresent: true, ResignAction: core.ResignActionNoAction, Label: "phase", Success: true, TransportationName: bestEffortTransportationName, Order: core.OrderTimestamp}
	timestamp, lookahead := new([8]byte), new([8]byte)
	binary.BigEndian.PutUint64(timestamp[:], math.Float64bits(2))
	binary.BigEndian.PutUint64(lookahead[:], math.Float64bits(1))
	f.command.Time = timestamp
	f.command.Lookahead = lookahead
	objectService := strings.Contains(service, "ObjectInstance") || strings.Contains(service, "AttributeTransportation") || service == momServiceUnconditionalAttributeOwnershipDivestiture || service == momServiceChangeAttributeOrderType || service == momServiceUnpublishObjectClassAttributes
	if objectService {
		publisher := target
		if service == momServiceLocalDeleteObjectInstance {
			publisher = sender
		}
		if err := deps.Declarations.PublishObjectClassAttributes(ctx, fed, publisher, cls, []core.AttributeHandle{attr, privilege}); err != nil {
			t.Fatal(err)
		}
		if service == momServiceLocalDeleteObjectInstance {
			if err := deps.Declarations.SubscribeObjectClassAttributes(ctx, fed, target, cls, []core.AttributeHandle{attr}); err != nil {
				t.Fatal(err)
			}
		}
		obj, _, err := reg.Register(ctx, fed, publisher, cls, "")
		if err != nil {
			t.Fatal(err)
		}
		f.command.Object = obj
		if service == momServiceLocalDeleteObjectInstance {
			if err := deps.Declarations.UnsubscribeObjectClassAttributes(ctx, fed, target, cls, []core.AttributeHandle{attr}); err != nil {
				t.Fatal(err)
			}
		}
	}
	switch service {
	case momServiceUnpublishInteractionClass, momServiceRequestInteractionTransportationTypeChange, momServiceChangeInteractionOrderType:
		if err := deps.Declarations.PublishInteractionClass(ctx, fed, target, interaction); err != nil {
			t.Fatal(err)
		}
	case momServiceUnsubscribeObjectClassAttributes:
		if err := deps.Declarations.SubscribeObjectClassAttributes(ctx, fed, target, cls, []core.AttributeHandle{attr}); err != nil {
			t.Fatal(err)
		}
	case momServiceUnsubscribeInteractionClass:
		if err := deps.Declarations.SubscribeInteractionClass(ctx, fed, target, interaction); err != nil {
			t.Fatal(err)
		}
	case momServiceDisableTimeRegulation, momServiceModifyLookahead:
		if err := f.time.EnableRegulation(ctx, fed, target, 1); err != nil {
			t.Fatal(err)
		}
	case momServiceDisableTimeConstrained:
		if err := f.time.EnableConstrained(ctx, fed, target); err != nil {
			t.Fatal(err)
		}
	case momServiceDisableAsynchronousDelivery:
		if err := f.time.EnableAsynchronousDelivery(ctx, fed, target); err != nil {
			t.Fatal(err)
		}
	case momServiceSynchronizationPointAchieved:
		if err := deps.Synchronization.Register(ctx, fed, "phase", nil, []core.FederateHandle{target}); err != nil {
			t.Fatal(err)
		}
	case momServiceFederateSaveBegun, momServiceFederateSaveComplete, momServiceFederateRestoreComplete:
		if err := f.save.RequestFederationSave(ctx, fed, testCheckpointLabel, nil); err != nil {
			t.Fatal(err)
		}
		if service != momServiceFederateSaveBegun {
			if err := f.save.FederateSaveBegun(ctx, fed, target); err != nil {
				t.Fatal(err)
			}
		}
		if service == momServiceFederateRestoreComplete {
			if err := f.save.FederateSaveComplete(ctx, fed, target); err != nil {
				t.Fatal(err)
			}
			if err := f.save.FederateSaveBegun(ctx, fed, sender); err != nil {
				t.Fatal(err)
			}
			if err := f.save.FederateSaveComplete(ctx, fed, sender); err != nil {
				t.Fatal(err)
			}
			if err := f.save.RequestFederationRestore(ctx, fed, testCheckpointLabel); err != nil {
				t.Fatal(err)
			}
		}
	}
	return f
}

func TestMOMServiceExecutionRealManagersAllLeaves(t *testing.T) {
	if len(momExecutionServices) != 32 {
		t.Fatal(len(momExecutionServices))
	}
	for _, service := range momExecutionServices {
		t.Run(service, func(t *testing.T) {
			f := newMOMExecutionFixture(t, service)
			fed := core.FederationName("catalog-provider")
			invocation, err := f.executor.ExecuteMOMService(context.Background(), fed, f.sender, f.command)
			if err != nil || !invocation.Success {
				t.Fatalf("real service failed: %+v %v", invocation, err)
			}
			switch service {
			case momServiceResignFederationExecution:
				if _, err := f.deps.Federations.MemberIdentity(fed, f.target); !errors.Is(err, core.ErrFederateNotJoined) {
					t.Fatal("target did not resign", err)
				}
			case momServicePublishObjectClassAttributes:
				model, err := f.deps.FOMs.Get(context.Background(), fed)
				if err != nil {
					t.Fatal(err)
				}
				privilege, ok := model.LookupAttribute(f.command.ObjectClass, "HLAprivilegeToDeleteObject")
				if !ok {
					t.Fatal("delete privilege missing from model")
				}
				want := append(slices.Clone(f.command.Attributes), privilege)
				slices.Sort(want)
				want = slices.Compact(want)
				if !reflect.DeepEqual(f.deps.Declarations.PublishedObjectAttributes(fed, f.target, f.command.ObjectClass), want) {
					t.Fatal("publication missing")
				}
			case momServiceUnpublishObjectClassAttributes, momServiceUnconditionalAttributeOwnershipDivestiture:
				if f.deps.Ownership.IsOwnedBy(fed, f.target, f.command.Object, f.command.Attributes[0]) {
					t.Fatal("instance ownership retained")
				}
			case momServiceDeleteObjectInstance:
				if f.deps.Objects.ClassOf(fed, f.command.Object) != 0 {
					t.Fatal("object not deleted")
				}
			case momServiceLocalDeleteObjectInstance:
				if _, _, known := f.deps.Objects.LookupKnownObjectInstance(fed, f.target, f.command.Object); known {
					t.Fatal("local knowledge retained")
				}
			case momServiceRequestAttributeTransportationTypeChange, momServiceRequestInteractionTransportationTypeChange:
				if f.confirmed != 1 || len(f.prepared.Ticket) != 32 {
					t.Fatal("confirmation not requested")
				}
			case momServiceEnableTimeRegulation:
				if !f.time.Metrics(fed, f.target).Regulating {
					t.Fatal("regulation not enabled")
				}
			case momServiceDisableTimeRegulation:
				if f.time.Metrics(fed, f.target).Regulating {
					t.Fatal("regulation not disabled")
				}
			case momServiceEnableTimeConstrained:
				if !f.time.Metrics(fed, f.target).Constrained {
					t.Fatal("constraint not enabled")
				}
			case momServiceDisableTimeConstrained:
				if f.time.Metrics(fed, f.target).Constrained {
					t.Fatal("constraint not disabled")
				}
			case momServiceEnableAsynchronousDelivery:
				if !f.time.Metrics(fed, f.target).AsynchronousDelivery {
					t.Fatal("async delivery not enabled")
				}
			case momServiceDisableAsynchronousDelivery:
				if f.time.Metrics(fed, f.target).AsynchronousDelivery {
					t.Fatal("async delivery not disabled")
				}
			}
		})
	}
}

func TestMOMServiceExecutionInvalidTargetHasNoServiceEffects(t *testing.T) {
	for _, service := range momExecutionServices {
		t.Run(service, func(t *testing.T) {
			f := newMOMExecutionFixture(t, service)
			fed := core.FederationName("catalog-provider")
			beforeDecl, beforeOwners, beforeTime := f.deps.Declarations.ExportState(fed), f.deps.Ownership.ExportState(fed), f.time.Snapshot(fed)
			f.command.Target = core.FederateHandle(999999)
			if _, err := f.executor.ExecuteMOMService(context.Background(), fed, f.sender, f.command); !errors.Is(err, core.ErrFederateNotJoined) {
				t.Fatal("invalid target accepted", err)
			}
			if !reflect.DeepEqual(beforeDecl, f.deps.Declarations.ExportState(fed)) || !reflect.DeepEqual(beforeOwners, f.deps.Ownership.ExportState(fed)) || !reflect.DeepEqual(beforeTime, f.time.Snapshot(fed)) {
				t.Fatal("rejected request changed service state")
			}
		})
	}
}

func TestMOMServiceExecutionInteger64NeverRoundsThroughFloat(t *testing.T) {
	f := newMOMExecutionFixture(t, momServiceTimeAdvanceRequest)
	value := int64(1<<53) + 1
	raw := new([8]byte)
	binary.BigEndian.PutUint64(raw[:], uint64(value))
	f.command.Time = raw
	integerObjects, err := object.NewOf[int64](object.OptionsOf[int64]{Clock: core.NewManualClock(time.Unix(0, 0)), Outbox: &momCatalogOutbox{}, FOMs: f.deps.FOMs, Declarations: f.deps.Declarations})
	if err != nil {
		t.Fatal(err)
	}
	if err := executeTypedMOMService(context.Background(), "catalog-provider", f.command, f.integerTime, integerObjects, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.integerTime.Metrics("catalog-provider", f.target).CurrentTime; got != value {
		t.Fatalf("exact time=%d want=%d", got, value)
	}
	if got := f.time.Metrics("catalog-provider", f.target).CurrentTime; got != 0 {
		t.Fatal("integer call touched float manager")
	}
}
