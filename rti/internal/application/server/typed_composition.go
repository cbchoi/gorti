package server

import (
	"context"
	"fmt"
	"reflect"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func supportsIntegerTimeStrategies(lbts timepkg.LBTSStrategy, grant timepkg.GrantStrategy) bool {
	// The CLI resolves even omitted research settings to concrete defaults.
	// Recognize the actual standard implementation, not a spoofable Name value;
	// Integer still receives its own exact generic defaults, never these floats.
	return (lbts == nil || reflect.TypeOf(lbts) == reflect.TypeOf(timepkg.DefaultLBTSStrategy())) &&
		(grant == nil || reflect.TypeOf(grant) == reflect.TypeOf(timepkg.DefaultGrantStrategy()))
}

func newIntegerTimeManager(clock core.Clock, outbox core.Outbox, journal core.EventLog, management *mom.Manager) (*timepkg.ManagerOf[int64], error) {
	return timepkg.NewOf[int64](timepkg.OptionsOf[int64]{
		Clock: clock, Outbox: outbox, EventLog: journal,
		OnTimeStateChanged: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, regulating, constrained bool, lookahead, current int64) {
			observeIntegerTime(ctx, management, fed, h, regulating, constrained, lookahead, current)
		},
	})
}

func observeIntegerTime(ctx context.Context, management *mom.Manager, fed core.FederationName, h core.FederateHandle, regulating, constrained bool, lookahead, current int64) {
	value, err := core.ScalarTimeValue(current)
	if err != nil {
		return
	}
	interval, err := core.ScalarIntervalValue(lookahead)
	if err != nil {
		return
	}
	_ = management.TimeStateChangedValue(ctx, fed, h, regulating, constrained, interval, value)
}

func newIntegerObjectRegistry(source object.Options, manager *timepkg.ManagerOf[int64]) (*object.RegistryOf[int64], error) {
	if manager == nil {
		return nil, fmt.Errorf("rtid: Integer64 time manager required")
	}
	registry, err := object.NewOf[int64](object.OptionsOf[int64]{
		EventLog: source.EventLog, Declarations: source.Declarations, Outbox: source.Outbox,
		Codec: source.Codec, FOMs: source.FOMs, Clock: source.Clock,
		ManagementDispatch: source.ManagementDispatch, Federations: source.Federations, Orders: source.Orders,
		TSOGate: manager, Retractions: manager, TSOValidator: manager,
		OnRegister: source.OnRegister, OnObjectRegistered: source.OnObjectRegistered, OnDelete: source.OnDelete,
		OnUpdateSent: source.OnUpdateSent, OnInteractionSent: source.OnInteractionSent,
		OnReflectDelivered: source.OnReflectDelivered, OnInteractionDelivered: source.OnInteractionDelivered,
		OnObjectTraffic: source.OnObjectTraffic, OnObjectInstanceTraffic: source.OnObjectInstanceTraffic, OnInteractionTraffic: source.OnInteractionTraffic,
		OnObjectSentTransport: source.OnObjectSentTransport, OnInteractionSentTransport: source.OnInteractionSentTransport,
		Ownership: source.Ownership, DDM: source.DDM, RegistrationRegions: source.RegistrationRegions,
	})
	if err != nil {
		return nil, err
	}
	manager.SetDeletionLifecycle(registry)
	manager.SetQueuedMessageFilter(registry)
	return registry, nil
}

type runtimeTimeLifecycle struct {
	implementations core.FederationTimeImplementation
	floating        *timepkg.Manager
	integer         *timepkg.ManagerOf[int64]
}

func (t *runtimeTimeLifecycle) isInteger(fed core.FederationName) bool {
	name, exists := t.implementations.LogicalTimeImplementationFor(fed)
	return exists && name == core.ScalarRepresentation[int64]().String()
}
func (t *runtimeTimeLifecycle) OnFederateJoined(fed core.FederationName, h core.FederateHandle) error {
	if t.isInteger(fed) {
		return t.integer.OnFederateJoined(fed, h)
	}
	return t.floating.OnFederateJoined(fed, h)
}
func (t *runtimeTimeLifecycle) OnFederateResign(ctx context.Context, fed core.FederationName, h core.FederateHandle) {
	if t.isInteger(fed) {
		t.integer.OnFederateResign(ctx, fed, h)
		return
	}
	t.floating.OnFederateResign(ctx, fed, h)
}
func (t *runtimeTimeLifecycle) PendingDeliveryRetries() []core.FederationName {
	return append(t.floating.PendingDeliveryRetries(), t.integer.PendingDeliveryRetries()...)
}
func (t *runtimeTimeLifecycle) RetryPendingDeliveries(ctx context.Context, fed core.FederationName) error {
	if t.isInteger(fed) {
		return t.integer.RetryPendingDeliveries(ctx, fed)
	}
	return t.floating.RetryPendingDeliveries(ctx, fed)
}
