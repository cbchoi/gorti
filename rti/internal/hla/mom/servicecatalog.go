package mom

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

const parameterTimeStamp = "HLAtimeStamp"

const ServiceClassPrefix = "HLAmanager.HLAfederate.HLAservice."

// MOMServiceCommand carries decoded service arguments. Scalar bytes retain the
// federation's representation; only its typed executor may interpret them.
type MOMServiceCommand struct { //nolint:revive // Retain the established public command name for existing executors.
	Service            string
	Target             core.FederateHandle
	ObjectClass        core.ObjectClassHandle
	InteractionClass   core.InteractionClassHandle
	Object             core.ObjectHandle
	Attributes         []core.AttributeHandle
	AttributesPresent  bool
	Active             *bool
	Tag                []byte
	Time               *[8]byte
	Lookahead          *[8]byte
	ResignAction       core.ResignAction
	Label              string
	Success            bool
	TransportationName string
	Order              core.OrderType
	SuppliedArguments  []string
}

type MOMServiceExecutor interface { //nolint:revive // Retain the established public executor interface name.
	ExecuteMOMService(context.Context, core.FederationName, core.FederateHandle, MOMServiceCommand) (ServiceInvocation, error)
}

type serviceDefinition struct {
	leaf               string
	required, optional []string
}

var serviceDefinitions = []serviceDefinition{
	{"HLAresignFederationExecution", []string{"HLAresignAction"}, nil},
	{"HLAsynchronizationPointAchieved", []string{"HLAlabel"}, nil},
	{"HLAfederateSaveBegun", nil, nil},
	{"HLAfederateSaveComplete", []string{"HLAsuccessIndicator"}, nil},
	{"HLAfederateRestoreComplete", []string{"HLAsuccessIndicator"}, nil},
	{"HLApublishObjectClassAttributes", []string{"HLAobjectClass", "HLAattributeList"}, nil},
	{"HLAunpublishObjectClassAttributes", []string{"HLAobjectClass"}, []string{"HLAattributeList"}},
	{"HLApublishInteractionClass", []string{"HLAinteractionClass"}, nil},
	{"HLAunpublishInteractionClass", []string{"HLAinteractionClass"}, nil},
	{"HLAsubscribeObjectClassAttributes", []string{"HLAobjectClass", "HLAattributeList"}, []string{"HLAactive"}},
	{"HLAunsubscribeObjectClassAttributes", []string{"HLAobjectClass"}, []string{"HLAattributeList"}},
	{"HLAsubscribeInteractionClass", []string{"HLAinteractionClass"}, []string{"HLAactive"}},
	{"HLAunsubscribeInteractionClass", []string{"HLAinteractionClass"}, nil},
	{"HLAdeleteObjectInstance", []string{"HLAobjectInstance", "HLAtag"}, []string{parameterTimeStamp}},
	{"HLAlocalDeleteObjectInstance", []string{"HLAobjectInstance"}, nil},
	{"HLArequestAttributeTransportationTypeChange", []string{"HLAobjectInstance", "HLAattributeList", "HLAtransportation"}, nil},
	{"HLArequestInteractionTransportationTypeChange", []string{"HLAinteractionClass", "HLAtransportation"}, nil},
	{"HLAunconditionalAttributeOwnershipDivestiture", []string{"HLAobjectInstance", "HLAattributeList"}, nil},
	{"HLAenableTimeRegulation", []string{"HLAlookahead"}, nil},
	{"HLAdisableTimeRegulation", nil, nil},
	{"HLAenableTimeConstrained", nil, nil},
	{"HLAdisableTimeConstrained", nil, nil},
	{"HLAtimeAdvanceRequest", []string{parameterTimeStamp}, nil},
	{"HLAtimeAdvanceRequestAvailable", []string{parameterTimeStamp}, nil},
	{"HLAnextMessageRequest", []string{parameterTimeStamp}, nil},
	{"HLAnextMessageRequestAvailable", []string{parameterTimeStamp}, nil},
	{"HLAflushQueueRequest", []string{parameterTimeStamp}, nil},
	{"HLAenableAsynchronousDelivery", nil, nil},
	{"HLAdisableAsynchronousDelivery", nil, nil},
	{"HLAmodifyLookahead", []string{"HLAlookahead"}, nil},
	{"HLAchangeAttributeOrderType", []string{"HLAobjectInstance", "HLAattributeList", "HLAsendOrder"}, nil},
	{"HLAchangeInteractionOrderType", []string{"HLAinteractionClass", "HLAsendOrder"}, nil},
}

func (d *Dispatcher) SetServiceExecutor(executor MOMServiceExecutor) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.executor = executor
}

// ServiceLeaf identifies the predefined RTI subscription behind an extension.
// Callers must still validate the supplied class against the federation FOM.
func (d *Dispatcher) ServiceLeaf(className string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	leaf, _, ok := d.lookupLocked(className)
	return leaf, ok && strings.HasPrefix(leaf, ServiceClassPrefix)
}

func registerServiceHandlers(d *Dispatcher) {
	for _, definition := range serviceDefinitions {
		d.Register(ServiceClassPrefix+definition.leaf, d.serviceHandler(definition))
	}
}

func (d *Dispatcher) serviceHandler(definition serviceDefinition) Handler {
	return func(ctx context.Context, dctx DispatchContext, sender core.FederateHandle, params map[core.ParameterHandle][]byte) ([]ResponseInteraction, error) {
		command, err := decodeMOMService(dctx, definition, params)
		if err == nil {
			err = validateServiceSubscription(dctx, command)
		}
		if err == nil {
			if dctx.Executor == nil {
				err = fmt.Errorf("MOM service executor unavailable")
			} else {
				var invocation ServiceInvocation
				var resignState *delegatedResignOutcome
				ctx, resignState = d.delegatedResignContext(ctx, dctx, command)
				invocation, err = dctx.Executor.ExecuteMOMService(ctx, dctx.Federation, sender, command)
				recorded := false
				if resignState != nil {
					var reportErr error
					recorded, reportErr = resignState.result()
					if reportErr != nil {
						return nil, errors.Join(err, reportErr)
					}
				}
				if dctx.MOM != nil && !recorded {
					if _, joined := dctx.MOM.QueryFederateAttributes(dctx.Federation, command.Target); joined {
						var countErr error
						invocation, countErr = recordDelegatedService(dctx, command, definition.leaf, invocation, err)
						if countErr != nil {
							return nil, countErr
						}
						if reportErr := d.ReportServiceInvocation(ctx, dctx.Federation, command.Target, invocation, dctx.FOM, dctx.FOMNames); reportErr != nil {
							if err == nil {
								return nil, reportErr
							}
							err = errors.Join(err, reportErr)
						}
					} else if command.Service == "resignFederationExecution" && err == nil {
						return nil, fmt.Errorf("MOM resign lifecycle reporting hook unavailable")
					}
				}
			}
		}
		if err != nil {
			return nil, errors.Join(err, d.reportServiceMOMFailure(ctx, dctx, ServiceClassPrefix+definition.leaf, sender, command.Target, err))
		}
		return nil, err
	}
}

func (d *Dispatcher) reportServiceMOMFailure(ctx context.Context, dctx DispatchContext, leaf string, sender, target core.FederateHandle, cause error) error {
	var parameter *parameterError
	report := ResponseInteraction{ClassName: ClassReportMOMException, Params: map[string][]byte{
		"HLAfederate": encodeHLAhandle(target), "HLAservice": encodeHLAunicodeString("HLAinteractionRoot." + leaf),
		"HLAexception": encodeHLAunicodeString(cause.Error()), "HLAparameterError": boolEncoding(errors.As(cause, &parameter)),
	}}
	return d.emitResponses(ctx, dctx.Federation, sender, []ResponseInteraction{report}, dctx.FOM, dctx.FOMNames, "")
}

func serviceName(leaf string) string {
	n := strings.TrimPrefix(leaf, "HLA")
	return strings.ToLower(n[:1]) + n[1:]
}

func decodeMOMService(dctx DispatchContext, definition serviceDefinition, params map[core.ParameterHandle][]byte) (MOMServiceCommand, error) {
	c := MOMServiceCommand{Service: serviceName(definition.leaf)}
	class := ServiceClassPrefix + definition.leaf
	target, err := decodeSwitchTarget(dctx, class, params)
	if err != nil {
		return c, malformed("%v", err)
	}
	c.Target = target
	for _, name := range append([]string{"HLAfederate"}, definition.required...) {
		if _, present := serviceParameter(dctx, class, name, params); !present {
			return c, malformed("%s: missing %s", class, name)
		}
	}
	for _, name := range append(slices.Clone(definition.required), definition.optional...) {
		b, present := serviceParameter(dctx, class, name, params)
		if !present {
			continue
		}
		// A deterministic encoded value preserves exact time representations and
		// optional argument presence without inventing default supplied arguments.
		c.SuppliedArguments = append(c.SuppliedArguments, fmt.Sprintf("%s=0x%x", name, b))
		if err := decodeServiceParameter(&c, dctx, definition, class, name, b, params); err != nil {
			return c, err
		}
	}
	return c, nil
}

func serviceParameter(dctx DispatchContext, class, name string, params map[core.ParameterHandle][]byte) ([]byte, bool) {
	if dctx.FOM == nil {
		return nil, false
	}
	cls, ok := dctx.FOM.LookupInteractionClass(class)
	if !ok {
		return nil, false
	}
	h, ok := dctx.FOM.LookupParameter(cls, name)
	if !ok {
		return nil, false
	}
	b, present := params[h]
	return b, present
}

func decodeServiceString(b []byte) (string, error) {
	if len(b) < 4 || uint64(binary.BigEndian.Uint32(b))*2 != (uint64(len(b))-4) {
		return "", fmt.Errorf("invalid Unicode string length")
	}
	units := make([]uint16, (len(b)-4)/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(b[4+2*i:])
	}
	for i := 0; i < len(units); i++ {
		if units[i] >= 0xd800 && units[i] <= 0xdbff {
			if i+1 == len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
				return "", fmt.Errorf("unpaired Unicode surrogate")
			}
			i++
		} else if units[i] >= 0xdc00 && units[i] <= 0xdfff {
			return "", fmt.Errorf("unpaired Unicode surrogate")
		}
	}
	return string(utf16.Decode(units)), nil
}
func decodeServiceParameter(c *MOMServiceCommand, dctx DispatchContext, definition serviceDefinition, class, name string, b []byte, params map[core.ParameterHandle][]byte) error {
	switch name {
	case "HLAobjectClass", "HLAinteractionClass", "HLAobjectInstance":
		return decodeServiceHandle(c, dctx, class, name, params)
	case "HLAattributeList":
		return decodeServiceAttributes(c, b)
	case "HLAactive", "HLAsuccessIndicator":
		return decodeServiceBoolean(c, name, b)
	case "HLAlabel", "HLAtransportation":
		return decodeServiceText(c, name, b)
	case "HLAtag", parameterTimeStamp, "HLAlookahead":
		return decodeServiceOpaque(c, definition, name, b)
	case "HLAresignAction", "HLAsendOrder":
		return decodeServiceEnumeration(c, dctx, class, name, params)
	}
	return nil
}

func decodeServiceHandle(c *MOMServiceCommand, dctx DispatchContext, class, name string, params map[core.ParameterHandle][]byte) error {
	h, err := parameterHandle(dctx, class, name, params)
	if err != nil {
		return err
	}
	switch name {
	case "HLAobjectClass":
		c.ObjectClass = core.ObjectClassHandle(h)
	case "HLAinteractionClass":
		c.InteractionClass = core.InteractionClassHandle(h)
	default:
		c.Object = core.ObjectHandle(h)
	}
	return nil
}

func decodeServiceAttributes(c *MOMServiceCommand, b []byte) error {
	c.AttributesPresent = true
	if len(b) < 4 || uint64(binary.BigEndian.Uint32(b))*12 != (uint64(len(b))-4) {
		return malformed("HLAattributeList: invalid framed handle list")
	}
	seen := map[core.AttributeHandle]bool{}
	for offset := 4; offset < len(b); offset += 12 {
		if binary.BigEndian.Uint32(b[offset:]) != 8 {
			return malformed("HLAattributeList: invalid handle frame")
		}
		h := core.AttributeHandle(binary.BigEndian.Uint64(b[offset+4:]))
		if h == 0 || seen[h] {
			return malformed("HLAattributeList: zero or duplicate attribute")
		}
		seen[h] = true
		c.Attributes = append(c.Attributes, h)
	}
	return nil
}

func decodeServiceBoolean(c *MOMServiceCommand, name string, b []byte) error {
	v, _, err := decodeHLAswitch(b)
	if err != nil {
		return malformed("%s: %v", name, err)
	}
	if name == "HLAactive" {
		c.Active = &v
	} else {
		c.Success = v
	}
	return nil
}

func decodeServiceText(c *MOMServiceCommand, name string, b []byte) error {
	v, err := decodeServiceString(b)
	if err != nil {
		return malformed("%s: %v", name, err)
	}
	if name == "HLAlabel" {
		c.Label = v
	} else {
		c.TransportationName = v
	}
	return nil
}

func decodeServiceOpaque(c *MOMServiceCommand, definition serviceDefinition, name string, b []byte) error {
	if len(b) < 4 || uint64(binary.BigEndian.Uint32(b)) != (uint64(len(b))-4) {
		return malformed("%s: invalid opaque array", name)
	}
	if name == "HLAtag" {
		c.Tag = slices.Clone(b[4:])
		return nil
	}
	if name == parameterTimeStamp && len(b) == 4 && definition.leaf == "HLAdeleteObjectInstance" {
		return nil
	}
	if len(b) != 12 {
		return malformed("%s: expected eight scalar bytes", name)
	}
	value := new([8]byte)
	copy(value[:], b[4:])
	if name == parameterTimeStamp {
		c.Time = value
	} else {
		c.Lookahead = value
	}
	return nil
}

func decodeServiceEnumeration(c *MOMServiceCommand, dctx DispatchContext, class, name string, params map[core.ParameterHandle][]byte) error {
	v, err := parameterInteger(dctx, class, name, params)
	if err != nil {
		return err
	}
	if name == "HLAsendOrder" {
		if v < 0 || v > 1 {
			return malformed("HLAsendOrder: invalid enumeration")
		}
		c.Order = core.OrderReceive
		if v == 1 {
			c.Order = core.OrderTimestamp
		}
	} else {
		actions := []core.ResignAction{0, core.ResignActionUnconditionallyDivestAttributes, core.ResignActionDeleteObjects, core.ResignActionCancelPendingOwnership, core.ResignActionDeleteThenDivest, core.ResignActionCancelThenDelete, core.ResignActionNoAction}
		if v < 1 || v > 6 {
			return malformed("HLAresignAction: invalid enumeration")
		}
		c.ResignAction = actions[v]
	}
	return nil
}

func (d *Dispatcher) delegatedResignContext(ctx context.Context, dctx DispatchContext, command MOMServiceCommand) (context.Context, *delegatedResignOutcome) {
	if command.Service != "resignFederationExecution" || dctx.MOM == nil {
		return ctx, nil
	}
	state := &delegatedResignOutcome{dispatcher: d, dctx: dctx, command: command}
	return context.WithValue(ctx, delegatedResignKey{}, state), state
}

func recordDelegatedService(dctx DispatchContext, command MOMServiceCommand, leaf string, invocation ServiceInvocation, err error) (ServiceInvocation, error) {
	if invocation.Service == "" {
		invocation.Service = serviceName(leaf)
	}
	invocation.Success = err == nil
	if err != nil && invocation.Exception == "" {
		invocation.Exception = err.Error()
	}
	if countErr := dctx.MOM.RecordServiceInvocation(dctx.Federation, command.Target, invocation); countErr != nil {
		return invocation, errors.Join(err, countErr)
	}
	return invocation, nil
}
func validateServiceSubscription(dctx DispatchContext, command MOMServiceCommand) error {
	if command.Service == "subscribeInteractionClass" && dctx.MOM != nil {
		return core.ValidateMOMReportSubscription(dctx.FOM, dctx.MOM, dctx.Federation, command.Target, command.InteractionClass)
	}
	return nil
}
