package object

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/eventlog"
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

func benchmarkAuditPluginEventLog(b *testing.B) core.EventLog {
	b.Helper()
	writer, err := eventlog.NewMultiplexWriter(eventlog.MultiplexOptions{
		Clock: core.NewFakeClock(time.Unix(0, 0)),
		Factory: func(opts eventlog.WriterOptions) (*eventlog.Writer, error) {
			opts.Sink = io.Discard
			return eventlog.NewWriter(opts)
		},
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := writer.Close(); err != nil {
			b.Errorf("close event log: %v", err)
		}
	})
	return writer
}

func benchmarkAuditPluginProfiles(
	b *testing.B,
	run func(*testing.B, core.EventLog),
) {
	b.Helper()
	for _, profile := range []struct {
		name     string
		eventLog func(*testing.B) core.EventLog
	}{
		{name: "hla-core", eventLog: func(*testing.B) core.EventLog { return nil }},
		{name: "noop", eventLog: func(*testing.B) core.EventLog { return benchmarkEventLog{} }},
		{name: "audit-plugin", eventLog: benchmarkAuditPluginEventLog},
	} {
		b.Run(profile.name, func(b *testing.B) {
			run(b, profile.eventLog(b))
		})
	}
}

func benchmarkInteractionRegistry(b *testing.B, tso bool, eventLog core.EventLog) *Registry {
	b.Helper()
	declarations := declaration.New()
	outbox := benchmarkOutbox{}
	options := Options{
		EventLog:           eventLog,
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
	benchmarkAuditPluginProfiles(b, func(b *testing.B, eventLog core.EventLog) {
		benchmarkSendInteractionRegistry(b, false, eventLog)
	})
}

func BenchmarkSendInteractionRegistryTSOReserved(b *testing.B) {
	benchmarkAuditPluginProfiles(b, func(b *testing.B, eventLog core.EventLog) {
		benchmarkSendInteractionRegistry(b, true, eventLog)
	})
}

func BenchmarkUpdateAttributesRegistryRO(b *testing.B) {
	benchmarkAuditPluginProfiles(b, benchmarkUpdateAttributesRegistryRO)
}

func benchmarkUpdateAttributesRegistryRO(b *testing.B, eventLog core.EventLog) {
	declarations := declaration.New()
	registry, err := New(Options{
		EventLog:     eventLog,
		Declarations: declarations,
		Outbox:       benchmarkOutbox{},
		FOMs:         &stubFOMRepo{},
		Clock:        core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	attributes := []core.AttributeHandle{1}
	if err := declarations.PublishObjectClassAttributes(ctx, "benchmark", 1, 7, attributes); err != nil {
		b.Fatal(err)
	}
	if err := declarations.SubscribeObjectClassAttributes(ctx, "benchmark", 2, 7, attributes); err != nil {
		b.Fatal(err)
	}
	objectHandle, _, err := registry.Register(ctx, "benchmark", 1, 7, "benchmark-object")
	if err != nil {
		b.Fatal(err)
	}
	values := map[core.AttributeHandle][]byte{1: []byte("0123456789abcdef")}
	if err := registry.UpdateAttributes(ctx, "benchmark", 1, objectHandle, values, nil); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := registry.UpdateAttributes(ctx, "benchmark", 1, objectHandle, values, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkSendInteractionRegistry(b *testing.B, tso bool, eventLog core.EventLog) {
	registry := benchmarkInteractionRegistry(b, tso, eventLog)
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
