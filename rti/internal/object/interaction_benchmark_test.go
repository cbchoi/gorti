package object

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
)

type benchmarkEventLog struct{}

func (benchmarkEventLog) Append(context.Context, core.FederationName, core.EventRecord) error {
	return nil
}
func (benchmarkEventLog) Sync(context.Context, core.FederationName) error { return nil }
func (benchmarkEventLog) OpenReader(context.Context, string) (core.EventLogReader, error) {
	return nil, nil
}

type benchmarkOutbox struct{}

func (benchmarkOutbox) Send(
	context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent,
) error {
	return nil
}
func (benchmarkOutbox) Reserve(
	context.Context, core.FederationName, []core.OutboxDelivery,
) (core.OutboxReservation, error) {
	return benchmarkOutboxReservation{}, nil
}

type benchmarkOutboxReservation struct{}

func (benchmarkOutboxReservation) Commit() error { return nil }
func (benchmarkOutboxReservation) Release()      {}

type benchmarkTSOGate struct{}

func (benchmarkTSOGate) ShouldDeliverNow(
	core.FederationName, core.FederateHandle, core.LogicalTime,
) bool {
	return false
}
func (benchmarkTSOGate) BufferTSO(
	context.Context, core.FederationName, core.FederateHandle, core.LogicalTime, core.OutboundEvent,
) error {
	return nil
}
func (benchmarkTSOGate) BufferTSOWithRetraction(
	context.Context,
	core.FederationName,
	core.FederateHandle,
	core.LogicalTime,
	core.OutboundEvent,
	core.FederateHandle,
	uint64,
) error {
	return nil
}
func (benchmarkTSOGate) RetractMessage(core.FederationName, core.FederateHandle, uint64) int {
	return 0
}
func (benchmarkTSOGate) ReserveTSO(
	_ core.FederationName, deliveries []core.TSOBufferedDelivery,
) core.TSOBufferReservation {
	return benchmarkTSOReservation{buffered: deliveries}
}

type benchmarkTSOReservation struct {
	buffered []core.TSOBufferedDelivery
}

func (benchmarkTSOReservation) Immediate() []core.TSOBufferedDelivery  { return nil }
func (r benchmarkTSOReservation) Buffered() []core.TSOBufferedDelivery { return r.buffered }
func (benchmarkTSOReservation) Commit(context.Context)                 {}
func (benchmarkTSOReservation) Release()                               {}

type benchmarkManagementDispatch struct{}

func (benchmarkManagementDispatch) IsManagerClass(string) bool { return false }
func (benchmarkManagementDispatch) Dispatch(
	context.Context,
	core.FederationName,
	string,
	core.FederateHandle,
	map[core.ParameterHandle][]byte,
	core.FOMHandle,
	core.FOMHandleNameLookup,
) error {
	return nil
}

func benchmarkInteractionRegistry(b *testing.B, tso bool) *Registry {
	b.Helper()
	declarations := declaration.New()
	outbox := benchmarkOutbox{}
	options := Options{
		EventLog:           benchmarkEventLog{},
		Declarations:       declarations,
		Outbox:             outbox,
		FOMs:               &strictInteractionFOMRepo{handle: &strictInteractionFOM{}},
		Clock:              core.NewFakeClock(time.Unix(0, 0)),
		ManagementDispatch: benchmarkManagementDispatch{},
	}
	if tso {
		options.TSOGate = benchmarkTSOGate{}
	}
	registry, err := New(options)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	if err := declarations.PublishInteractionClass(ctx, "benchmark", 1, 7); err != nil {
		b.Fatal(err)
	}
	if err := declarations.SubscribeInteractionClass(ctx, "benchmark", 2, 7); err != nil {
		b.Fatal(err)
	}
	return registry
}

func BenchmarkSendInteractionRegistryRO(b *testing.B) {
	benchmarkSendInteractionRegistry(b, false)
}

func BenchmarkSendInteractionRegistryTSOReserved(b *testing.B) {
	benchmarkSendInteractionRegistry(b, true)
}

func benchmarkSendInteractionRegistry(b *testing.B, tso bool) {
	registry := benchmarkInteractionRegistry(b, tso)
	ctx := context.Background()
	parameters := map[core.ParameterHandle][]byte{1: []byte("0123456789abcdef")}
	var timestamp *core.LogicalTime
	if tso {
		value := core.LogicalTime(1)
		timestamp = &value
	}
	if err := registry.SendInteraction(ctx, "benchmark", 1, 7, parameters, timestamp); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := registry.SendInteraction(ctx, "benchmark", 1, 7, parameters, timestamp); err != nil {
			b.Fatal(err)
		}
	}
}
