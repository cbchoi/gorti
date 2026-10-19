package server

import (
	"context"
	"fmt"
	"slices"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

type momCheckpointServices interface {
	FederateSaveBegun(context.Context, core.FederationName, core.FederateHandle) error
	FederateSaveComplete(context.Context, core.FederationName, core.FederateHandle) error
	FederateSaveNotComplete(context.Context, core.FederationName, core.FederateHandle) error
	FederateRestoreComplete(context.Context, core.FederationName, core.FederateHandle) error
	FederateRestoreNotComplete(context.Context, core.FederationName, core.FederateHandle) error
}

type momServiceExecutionDependencies struct {
	Federations            *federation.Manager
	Declarations           *declaration.Manager
	FOMs                   core.FOMRepository
	Ownership              core.KnownOwnershipCoordinator
	Synchronization        core.SyncCoordinator
	FloatTime              core.TimeManager
	IntegerTime            core.TimeManagerOf[int64]
	FloatObjects           *object.Registry
	IntegerObjects         *object.RegistryOf[int64]
	FloatSave, IntegerSave momCheckpointServices
	// Admission authenticates the managing sender and pins the target identity.
	// Resign needs the lifecycle writer; save/restore confirmations need their
	// allowed checkpoint-control admission rather than ordinary service admission.
	Admission func(context.Context, core.FederationName, core.FederateHandle, core.FederateHandle, string) (func(), error)
	Resign    func(context.Context, core.MemberIdentity, core.ResignAction) error
	// TransportationConfirmed owns preparation and queue admission. It must not
	// activate the change before the target enters its confirmation callback.
	TransportationConfirmed func(context.Context, core.FederationName, core.FederateHandle, mom.MOMServiceCommand, core.TransportType) error
}

type momServiceExecutor struct {
	deps momServiceExecutionDependencies
}

func configureMOMServiceExecution(dispatcher *mom.Dispatcher, deps momServiceExecutionDependencies) error {
	if dispatcher == nil {
		return fmt.Errorf("MOM service dispatcher unavailable")
	}
	executor, err := newMOMServiceExecutor(deps)
	if err != nil {
		return err
	}
	dispatcher.SetServiceExecutor(executor)
	return nil
}

func newMOMServiceExecutor(deps momServiceExecutionDependencies) (mom.MOMServiceExecutor, error) {
	if deps.Federations == nil || deps.Declarations == nil || deps.FOMs == nil || deps.Ownership == nil || deps.Synchronization == nil || deps.FloatTime == nil || deps.IntegerTime == nil || deps.FloatObjects == nil || deps.IntegerObjects == nil || deps.Admission == nil || deps.Resign == nil {
		return nil, fmt.Errorf("MOM service execution dependencies incomplete")
	}
	return &momServiceExecutor{deps: deps}, nil
}

func (e *momServiceExecutor) ExecuteMOMService(ctx context.Context, fed core.FederationName, sender core.FederateHandle, c mom.MOMServiceCommand) (invocation mom.ServiceInvocation, err error) {
	invocation = momCommandInvocation(c)
	defer func() {
		invocation.Success = err == nil
		if err != nil {
			invocation.Exception = grpcadapter.MOMServiceExceptionName(err)
			invocation.ReturnedArguments = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return invocation, err
	}
	release, err := e.deps.Admission(ctx, fed, sender, c.Target, c.Service)
	if err != nil {
		return invocation, err
	}
	if release == nil {
		return invocation, fmt.Errorf("MOM service admission returned no lease")
	}
	defer release()
	// The transport token continues to identify the managing sender. The target
	// is resolved through current membership, not by replacing authentication.
	identity, err := e.deps.Federations.MemberIdentity(fed, c.Target)
	if err != nil {
		return invocation, err
	}
	if _, err := e.deps.Federations.MemberIdentity(fed, sender); err != nil {
		return invocation, err
	}
	factory, exists := e.deps.Federations.LogicalTimeImplementationFor(fed)
	if !exists {
		return invocation, core.ErrFederationNotFound
	}
	fom, err := e.deps.FOMs.Get(ctx, fed)
	if err != nil {
		return invocation, err
	}
	if err := validateMOMCommand(fom, c); err != nil {
		return invocation, err
	}
	err = e.execute(ctx, fed, identity, c, factory)
	invocation.Success = err == nil
	if err != nil {
		invocation.Exception = grpcadapter.MOMServiceExceptionName(err)
	}
	return invocation, err
}

func executeTypedMOMService[T core.LogicalTimeScalar](ctx context.Context, fed core.FederationName, c mom.MOMServiceCommand, time core.TimeManagerOf[T], objects *object.RegistryOf[T], save momCheckpointServices, confirm func(context.Context, core.FederationName, core.FederateHandle, mom.MOMServiceCommand, core.TransportType) error) error {
	timestamp, interval, err := decodeMOMCommandTime[T](c)
	if err != nil {
		return err
	}
	if time == nil || objects == nil {
		return fmt.Errorf("MOM typed services unavailable")
	}
	switch c.Service {
	case momServiceFederateSaveBegun, momServiceFederateSaveComplete, momServiceFederateRestoreComplete:
		return executeMOMCheckpoint(ctx, fed, c, save)
	case momServiceDeleteObjectInstance:
		return objects.Delete(ctx, fed, c.Target, c.Object, timestamp, c.Tag)
	case momServiceLocalDeleteObjectInstance:
		return objects.LocalDelete(ctx, fed, c.Target, c.Object)
	case momServiceRequestAttributeTransportationTypeChange, momServiceRequestInteractionTransportationTypeChange:
		return executeMOMTransportation(ctx, fed, c, confirm)
	case momServiceChangeAttributeOrderType:
		return objects.ChangeAttributeOrderType(ctx, fed, c.Target, c.Object, c.Attributes, c.Order)
	case momServiceChangeInteractionOrderType:
		return objects.ChangeInteractionOrderType(ctx, fed, c.Target, c.InteractionClass, c.Order)
	default:
		return executeMOMTimeService(ctx, fed, c, time, timestamp, interval)
	}
}

func momCommandInvocation(c mom.MOMServiceCommand) mom.ServiceInvocation {
	invocation := mom.ServiceInvocation{Service: c.Service, SuppliedArguments: slices.Clone(c.SuppliedArguments)}
	if c.Service == momServiceUnpublishObjectClassAttributes && !c.AttributesPresent {
		invocation.Service = "unpublishObjectClass"
	}
	if c.Service == momServiceUnsubscribeObjectClassAttributes && !c.AttributesPresent {
		invocation.Service = "unsubscribeObjectClass"
	}
	if c.Service == momServiceFederateSaveComplete || c.Service == momServiceFederateRestoreComplete {
		// The MIM boolean selects one of two argument-free HLA services; it is
		// not a success argument supplied to either federate service call.
		invocation.SuppliedArguments = nil
		if !c.Success {
			if c.Service == momServiceFederateSaveComplete {
				invocation.Service = "federateSaveNotComplete"
			} else {
				invocation.Service = "federateRestoreNotComplete"
			}
		}
	}
	return invocation
}

func validateMOMCommand(fom core.FOMHandle, c mom.MOMServiceCommand) error {
	if names, ok := fom.(core.FOMHandleNameLookup); ok {
		if c.ObjectClass != 0 {
			if _, valid := names.ObjectClassName(c.ObjectClass); !valid {
				return core.ErrObjectClassNotFound
			}
			for _, a := range c.Attributes {
				if _, valid := names.AttributeName(c.ObjectClass, a); !valid {
					return core.ErrAttributeNotFound
				}
			}
		}
		if c.InteractionClass != 0 {
			if _, valid := names.InteractionClassName(c.InteractionClass); !valid {
				return core.ErrInteractionClassNotFound
			}
		}
	}
	return nil
}

func executeMOMDeclaration(ctx context.Context, fed core.FederationName, c mom.MOMServiceCommand, d *declaration.Manager) error {
	active := true
	if c.Active != nil {
		active = *c.Active
	}
	switch c.Service {
	case momServicePublishObjectClassAttributes:
		return d.PublishObjectClassAttributes(ctx, fed, c.Target, c.ObjectClass, c.Attributes)
	case momServiceUnpublishObjectClassAttributes:
		if c.AttributesPresent {
			return d.UnpublishObjectClassAttributeSet(ctx, fed, c.Target, c.ObjectClass, c.Attributes)
		}
		return d.UnpublishObjectClass(ctx, fed, c.Target, c.ObjectClass)
	case momServicePublishInteractionClass:
		return d.PublishInteractionClass(ctx, fed, c.Target, c.InteractionClass)
	case momServiceUnpublishInteractionClass:
		return d.UnpublishInteractionClass(ctx, fed, c.Target, c.InteractionClass)
	case momServiceSubscribeObjectClassAttributes:
		return d.SubscribeObjectClassAttributesWithOptions(ctx, fed, c.Target, c.ObjectClass, c.Attributes, core.SubscriptionOptions{Active: active})
	case momServiceUnsubscribeObjectClassAttributes:
		if c.AttributesPresent {
			return d.UnsubscribeObjectClassAttributeSet(ctx, fed, c.Target, c.ObjectClass, c.Attributes)
		}
		return d.UnsubscribeObjectClass(ctx, fed, c.Target, c.ObjectClass)
	case momServiceSubscribeInteractionClass:
		return d.SubscribeInteractionClassWithOptions(ctx, fed, c.Target, c.InteractionClass, active)
	case momServiceUnsubscribeInteractionClass:
		return d.UnsubscribeInteractionClass(ctx, fed, c.Target, c.InteractionClass)
	default:
		return fmt.Errorf("MOM declaration service %q is not implemented", c.Service)
	}
}

func (e *momServiceExecutor) execute(ctx context.Context, fed core.FederationName, identity core.MemberIdentity, c mom.MOMServiceCommand, factory string) error {
	switch c.Service {
	case momServiceResignFederationExecution:
		return e.deps.Resign(ctx, identity, c.ResignAction)
	case momServiceSynchronizationPointAchieved:
		return e.deps.Synchronization.Achieve(ctx, fed, c.Target, c.Label)
	case momServicePublishObjectClassAttributes, momServiceUnpublishObjectClassAttributes, momServicePublishInteractionClass, momServiceUnpublishInteractionClass, momServiceSubscribeObjectClassAttributes, momServiceUnsubscribeObjectClassAttributes, momServiceSubscribeInteractionClass, momServiceUnsubscribeInteractionClass:
		return executeMOMDeclaration(ctx, fed, c, e.deps.Declarations)
	case momServiceUnconditionalAttributeOwnershipDivestiture:
		_, err := e.deps.Ownership.ExecuteKnownOwnership(ctx, core.OwnershipCommand{Operation: core.OwnershipUnconditionalDivest, Federation: fed, Federate: c.Target, Object: c.Object, Attributes: slices.Clone(c.Attributes)})
		return err
	default:
		switch factory {
		case logicaltime.HLAfloat64Time.String():
			return executeTypedMOMService(ctx, fed, c, e.deps.FloatTime, e.deps.FloatObjects, e.deps.FloatSave, e.deps.TransportationConfirmed)
		case logicaltime.HLAinteger64Time.String():
			return executeTypedMOMService(ctx, fed, c, e.deps.IntegerTime, e.deps.IntegerObjects, e.deps.IntegerSave, e.deps.TransportationConfirmed)
		default:
			return core.ErrCouldNotCreateLogicalTimeFactory
		}
	}
}

func decodeMOMCommandTime[T core.LogicalTimeScalar](c mom.MOMServiceCommand) (*T, T, error) {
	var timestamp *T
	var interval T
	if c.Time != nil {
		value, err := logicaltime.DecodeTime(core.ScalarRepresentation[T](), c.Time[:])
		if err != nil {
			return nil, interval, core.ErrTimeInvalidLogicalTime
		}
		scalar, err := core.ScalarFromTimeValue[T](value)
		if err != nil {
			return nil, interval, err
		}
		timestamp = &scalar
	}
	if c.Lookahead != nil {
		value, err := logicaltime.DecodeInterval(core.ScalarRepresentation[T](), c.Lookahead[:])
		if err != nil {
			return nil, interval, core.ErrTimeInvalidLookahead
		}
		interval, err = core.ScalarFromIntervalValue[T](value)
		if err != nil {
			return nil, interval, err
		}
	}
	return timestamp, interval, nil
}

func executeMOMCheckpoint(ctx context.Context, fed core.FederationName, c mom.MOMServiceCommand, save momCheckpointServices) error {
	if save == nil {
		return fmt.Errorf("MOM checkpoint service unavailable")
	}
	if c.Service == momServiceFederateSaveBegun {
		return save.FederateSaveBegun(ctx, fed, c.Target)
	}
	if c.Service == momServiceFederateSaveComplete {
		if c.Success {
			return save.FederateSaveComplete(ctx, fed, c.Target)
		}
		return save.FederateSaveNotComplete(ctx, fed, c.Target)
	}
	if c.Success {
		return save.FederateRestoreComplete(ctx, fed, c.Target)
	}
	return save.FederateRestoreNotComplete(ctx, fed, c.Target)
}

func executeMOMTransportation(ctx context.Context, fed core.FederationName, c mom.MOMServiceCommand, confirm func(context.Context, core.FederationName, core.FederateHandle, mom.MOMServiceCommand, core.TransportType) error) error {
	if confirm == nil {
		return fmt.Errorf("MOM transportation confirmation delivery unavailable")
	}
	var tt core.TransportType
	switch c.TransportationName {
	case "HLAreliable":
		tt = core.TransportTypeReliable
	case bestEffortTransportationName:
		tt = core.TransportTypeBestEffort
	default:
		return core.ErrTransportTypeUnspecified
	}
	return confirm(ctx, fed, c.Target, c, tt)
}

func executeMOMTimeService[T core.LogicalTimeScalar](ctx context.Context, fed core.FederationName, c mom.MOMServiceCommand, time core.TimeManagerOf[T], timestamp *T, interval T) error {
	switch c.Service {
	case momServiceEnableTimeRegulation:
		if c.Lookahead == nil {
			return core.ErrTimeInvalidLookahead
		}
		return time.EnableRegulation(ctx, fed, c.Target, interval)
	case momServiceDisableTimeRegulation:
		return time.DisableRegulation(ctx, fed, c.Target)
	case momServiceEnableTimeConstrained:
		return time.EnableConstrained(ctx, fed, c.Target)
	case momServiceDisableTimeConstrained:
		return time.DisableConstrained(ctx, fed, c.Target)
	case momServiceEnableAsynchronousDelivery:
		return time.EnableAsynchronousDelivery(ctx, fed, c.Target)
	case momServiceDisableAsynchronousDelivery:
		return time.DisableAsynchronousDelivery(ctx, fed, c.Target)
	case momServiceModifyLookahead:
		if c.Lookahead == nil {
			return core.ErrTimeInvalidLookahead
		}
		return time.ModifyLookahead(ctx, fed, c.Target, interval)
	case momServiceTimeAdvanceRequest, momServiceTimeAdvanceRequestAvailable, momServiceNextMessageRequest, momServiceNextMessageRequestAvailable, momServiceFlushQueueRequest:
		return executeMOMTimeAdvance(ctx, fed, c, time, timestamp)
	default:
		return fmt.Errorf("MOM service %q is not implemented", c.Service)
	}
}

func executeMOMTimeAdvance[T core.LogicalTimeScalar](ctx context.Context, fed core.FederationName, c mom.MOMServiceCommand, time core.TimeManagerOf[T], timestamp *T) error {
	if timestamp == nil {
		return core.ErrTimeInvalidLogicalTime
	}
	switch c.Service {
	case momServiceTimeAdvanceRequest:
		return time.TimeAdvanceRequest(ctx, fed, c.Target, *timestamp)
	case momServiceTimeAdvanceRequestAvailable:
		return time.TimeAdvanceRequestAvailable(ctx, fed, c.Target, *timestamp)
	case momServiceNextMessageRequest:
		return time.NextMessageRequest(ctx, fed, c.Target, *timestamp)
	case momServiceNextMessageRequestAvailable:
		return time.NextMessageRequestAvailable(ctx, fed, c.Target, *timestamp)
	default:
		return time.FlushQueueRequest(ctx, fed, c.Target, *timestamp)
	}
}
