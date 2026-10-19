package server

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timedomain "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func requireMOMROLength(t *testing.T, manager *mom.Manager, h core.FederateHandle, want uint32) {
	t.Helper()
	actual, err := manager.FederateMetricValues(context.Background(), "catalog-provider", h)
	if err != nil {
		t.Fatal(err)
	}
	value := actual.Values["HLAROlength"]
	if len(value) != 4 || binary.BigEndian.Uint32(value) != want || actual.Unavailable["HLAROlength"] != "" {
		t.Fatalf("HLAROlength=%v unavailable=%q, want %d", value, actual.Unavailable["HLAROlength"], want)
	}
}

func TestMOMROQueueCountsOnlyTimeWaitingMessages(t *testing.T) {
	deps, _, sender, recipient, out := newMOMCatalogDeps(t)
	ctx := context.Background()
	const fed core.FederationName = "catalog-provider"
	manager, err := timedomain.New(timedomain.Options{Clock: core.NewManualClock(time.Unix(0, 0)), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	if err := configureMOMMetrics(deps.Manager, momMetricDependencies{Catalog: deps, Time: manager}); err != nil {
		t.Fatal(err)
	}
	if err := manager.EnableConstrained(ctx, fed, recipient); err != nil {
		t.Fatal(err)
	}
	// These transport-ready events must not be mistaken for waiting RO messages.
	stamp := core.LogicalTime(5)
	for _, event := range []core.OutboundEvent{
		&timedomain.TimeAdvanceGrant{Time: 0},
		&object.CallbackEvent{Kind: object.CallbackReceive, LogicalTime: &stamp},
	} {
		if err := out.Send(ctx, fed, recipient, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.BufferTSO(ctx, fed, recipient, stamp, &object.CallbackEvent{Kind: object.CallbackReceive, LogicalTime: &stamp}); err != nil {
		t.Fatal(err)
	}
	if actual := manager.Metrics(fed, recipient); actual.TSOQueued != 1 {
		t.Fatalf("test did not establish a waiting TSO message: %+v", actual)
	}
	requireMOMROLength(t, deps.Manager, recipient, 0)
	reservation := manager.ReserveTSO(fed, []core.MessageDelivery{{Recipient: recipient, Sender: sender, ReceiveOrder: true, Event: &object.CallbackEvent{Kind: object.CallbackReceive}}})
	if len(reservation.Buffered()) != 1 || len(reservation.Immediate()) != 0 {
		reservation.Release()
		t.Fatal("idle constrained recipient did not queue RO")
	}
	reservation.Commit(ctx)
	requireMOMROLength(t, deps.Manager, recipient, 1)
	before := len(out.events)
	if err := manager.EnableAsynchronousDelivery(ctx, fed, recipient); err != nil {
		t.Fatal(err)
	}
	if len(out.events) != before+1 {
		t.Fatal("RO message was not released to the transport")
	}
	requireMOMROLength(t, deps.Manager, recipient, 0)
	if actual := manager.Metrics(fed, recipient); actual.TSOQueued != 1 {
		t.Fatalf("asynchronous delivery released TSO: %+v", actual)
	}
}

func TestMOMROQueueTimeSourceTakesPrecedence(t *testing.T) {
	deps, _, _, recipient, out := newMOMCatalogDeps(t)
	manager, err := timedomain.New(timedomain.Options{Clock: core.NewManualClock(time.Unix(0, 0)), Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	fallbackCalls := 0
	if err := configureMOMMetrics(deps.Manager, momMetricDependencies{
		Catalog: deps, Time: manager,
		ROQueue: func(context.Context, core.FederationName, core.FederateHandle) (uint64, error) {
			fallbackCalls++
			return 0, errors.New("legacy transport count must not override time queue")
		},
	}); err != nil {
		t.Fatal(err)
	}
	requireMOMROLength(t, deps.Manager, recipient, 0)
	if fallbackCalls != 0 {
		t.Fatal("authoritative time metrics used a second queue source")
	}
}

func TestMOMROQueueCustomSourceWithoutTimeManager(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "count", true: "error"}[failed], func(t *testing.T) {
			deps, _, _, recipient, _ := newMOMCatalogDeps(t)
			sourceError := errors.New("custom RO source unavailable")
			if err := configureMOMMetrics(deps.Manager, momMetricDependencies{
				Catalog: deps,
				ROQueue: func(context.Context, core.FederationName, core.FederateHandle) (uint64, error) {
					if failed {
						return 0, sourceError
					}
					return 7, nil
				},
			}); err != nil {
				t.Fatal(err)
			}
			if failed {
				_, err := deps.Manager.FederateMetricValues(context.Background(), "catalog-provider", recipient)
				if !errors.Is(err, sourceError) {
					t.Fatalf("source error=%v, want %v", err, sourceError)
				}
				return
			}
			requireMOMROLength(t, deps.Manager, recipient, 7)
		})
	}
}
