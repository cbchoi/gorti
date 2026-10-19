package server

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

type conveyedLeaseProbe struct {
	mu     sync.Mutex
	leases map[core.FederateHandle]map[core.DDMRegionHandleCore]bool
}

func (p *conveyedLeaseProbe) ConveyCopies(_ context.Context, _ core.FederationName, h core.FederateHandle, copies []core.ConveyedRegionCopy) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.leases == nil {
		p.leases = make(map[core.FederateHandle]map[core.DDMRegionHandleCore]bool)
	}
	if p.leases[h] == nil {
		p.leases[h] = make(map[core.DDMRegionHandleCore]bool)
	}
	for _, regionCopy := range copies {
		p.leases[h][regionCopy.Designator] = false
	}
	return nil
}

func (p *conveyedLeaseProbe) EnterConveyedRegions(_ context.Context, _ core.FederationName, h core.FederateHandle, handles []core.DDMRegionHandleCore) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, handle := range handles {
		if _, found := p.leases[h][handle]; !found {
			return core.ErrRegionNotFound
		}
	}
	for _, handle := range handles {
		p.leases[h][handle] = true
	}
	return nil
}

func (p *conveyedLeaseProbe) ReleaseConveyedRegions(_ context.Context, _ core.FederationName, h core.FederateHandle, handles []core.DDMRegionHandleCore) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, handle := range handles {
		delete(p.leases[h], handle)
	}
	return nil
}

func conveyedCallbackFixture(t *testing.T, capacity int) (*conveyedCallbackOutbox, *grpcadapter.CallbackReceipts, *delivery.MultiOutbox, *conveyedLeaseProbe, *object.CallbackEvent) {
	t.Helper()
	probe := &conveyedLeaseProbe{}
	entry := conveyedCallbackEntryObserver(probe, func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
		return nil
	})
	outcome := conveyedCallbackOutcomeObserver(probe, func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
		return nil
	})
	receipts, err := grpcadapter.NewCallbackReceipts(grpcadapter.CallbackReceiptOptions{
		MaxPending: capacity, Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		ObserveEntry: entry, ObserveRequired: outcome,
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			return errors.New("optional telemetry unavailable")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := delivery.NewWithBatch(8, 1, 0)
	for _, h := range []core.FederateHandle{1, 2} {
		raw.Bind("f", h)
	}
	t.Cleanup(func() { raw.UnbindFederation("f") })
	regionCopy, err := core.NewRegionRealizationSnapshot(3, []core.RegionDimensionRange{{Dimension: 1, Bounds: core.DDMRange{Lower: 10, Upper: 20}}})
	if err != nil {
		t.Fatal(err)
	}
	event := &object.CallbackEvent{Sequence: 7, Kind: object.CallbackReceive, InteractionClass: 9, Metadata: &core.ObjectCallbackMetadata{Regions: []core.DDMRegionHandleCore{3}, ConveyedRegions: []core.ConveyedRegionCopy{{Designator: 101, Realization: regionCopy}}}}
	return newConveyedCallbackOutbox(raw, func() *grpcadapter.CallbackReceipts { return receipts }, func() conveyedRegionLifecycle { return probe }), receipts, raw, probe, event
}

func TestConveyedCallbackAtomicReservationAndActualInvocationLifetime(t *testing.T) {
	ctx := context.Background()
	out, receipts, raw, probe, event := conveyedCallbackFixture(t, 8)
	ch, cancel, err := raw.Subscribe(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cancel() }()
	if err := out.Send(ctx, "f", 1, event); err != nil {
		t.Fatal(err)
	}
	if entered := probe.leases[1][101]; entered {
		t.Fatal("queue admission entered callback")
	}
	batch := <-ch
	wire, err := grpcadapter.EncodeOutboundEvent(batch[0])
	if err != nil || len(wire.CallbackReceipt) != 32 {
		t.Fatal("required copied-region receipt missing", err)
	}
	if err := receipts.ReportEntry(ctx, "f", 1, wire.CallbackReceipt); err != nil {
		t.Fatal(err)
	}
	if !probe.leases[1][101] {
		t.Fatal("entry did not activate copied-region queries")
	}
	if err := receipts.Report(ctx, "f", 1, wire.CallbackReceipt, false, "ambassador error"); err != nil {
		t.Fatal(err)
	}
	if len(probe.leases[1]) != 0 {
		t.Fatal("callback outcome did not release query lease")
	}
	if err := receipts.Report(ctx, "f", 1, wire.CallbackReceipt, false, "ambassador error"); err != nil {
		t.Fatal(err)
	}
}

func TestConveyedCallbackFailedAllRecipientReservationReleasesLeases(t *testing.T) {
	out, _, raw, probe, event := conveyedCallbackFixture(t, 1)
	_, err := out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: event}, {Recipient: 2, Event: event}})
	if err == nil {
		t.Fatal("accepted partial recipient reservation")
	}
	for _, leases := range probe.leases {
		if len(leases) != 0 {
			t.Fatal("failed reservation retained query lease")
		}
	}
	for _, stat := range raw.OutboxStats() {
		if stat.QueueDepth != 0 {
			t.Fatal("failed reservation published callback")
		}
	}
}
