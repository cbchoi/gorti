package server

import (
	"context"
	"errors"
	"math"
	"testing"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

type entryCounterOutbox struct{}

func (entryCounterOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

func entryObserverManager(t *testing.T) (*mom.Manager, *mom.TrafficAccumulator) {
	t.Helper()
	m, err := mom.New(mom.Options{Outbox: entryCounterOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	a := mom.NewTrafficAccumulator()
	if err := m.SetTrafficAccumulator(a); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRuntimeMetricsEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateJoined(context.Background(), "f", 7, "seven", "test"); err != nil {
		t.Fatal(err)
	}
	return m, a
}

func entryInteractionEvent() *rtiv1.FederateEvent {
	return &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{
		InteractionClassHandle: 9,
		Metadata:               &rtiv1.ObjectCallbackMetadata{TransportationType: rtiv1.TransportationType_TRANSPORTATION_TYPE_RELIABLE.Enum()},
	}}}
}

func entryReceivedCount(t *testing.T, a *mom.TrafficAccumulator) uint32 {
	t.Helper()
	rows, err := a.Snapshot(context.Background(), "f", 7, mom.InteractionsReceived)
	if err != nil {
		t.Fatal(err)
	}
	return rows[0].Counts[9]
}

func TestMOMEntryObserverLifecycleFailureRetryAndReceiptDuplicate(t *testing.T) {
	m, a := entryObserverManager(t)
	transient := errors.New("lifecycle retry")
	previousCalls := 0
	entry := momDataCallbackEntryObserver(m, func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
		previousCalls++
		if entryReceivedCount(t, a) != 0 {
			t.Fatal("counter ran before lifecycle observer")
		}
		if previousCalls == 1 {
			return transient
		}
		return nil
	})
	// This explicitly records successful accounting; callback kind alone is not proof.
	counted := false
	receipts, err := grpcadapter.NewCallbackReceipts(grpcadapter.CallbackReceiptOptions{
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		ObserveEntry: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) error {
			if err := entry(ctx, fed, h, event); err != nil {
				return err
			}
			counted = momEntryCountedData(event)
			return nil
		},
		Observe: func(_ context.Context, fed core.FederationName, h core.FederateHandle, _ *rtiv1.FederateEvent, _ bool, _ string) error {
			return m.RecordCallbackCompletion(fed, h, mom.CallbackCompletion{Invocation: mom.CallbackInvocation{Service: "receiveInteraction"}, DataCountedAtEntry: counted})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ticket, commit, err := receipts.PrepareRequired(context.Background(), "f", 7, entryInteractionEvent())
	if err != nil {
		t.Fatal(err)
	}
	commit(true)
	if err := receipts.ReportEntry(context.Background(), "f", 7, ticket); !errors.Is(err, transient) {
		t.Fatal(err)
	}
	if counted || entryReceivedCount(t, a) != 0 {
		t.Fatal("failed ENTRY counted")
	}
	for i := 0; i < 2; i++ {
		if err := receipts.ReportEntry(context.Background(), "f", 7, ticket); err != nil {
			t.Fatal(err)
		}
	}
	if previousCalls != 2 || !counted || entryReceivedCount(t, a) != 1 {
		t.Fatal("retry or duplicate ENTRY accounting", previousCalls, counted)
	}
	if err := receipts.Report(context.Background(), "f", 7, ticket, false, "user callback threw"); err != nil {
		t.Fatal(err)
	}
	if err := receipts.Report(context.Background(), "f", 7, ticket, false, "user callback threw"); err != nil {
		t.Fatal(err)
	}
	if entryReceivedCount(t, a) != 1 {
		t.Fatal("completion double counted")
	}
}

func TestMOMEntryObserverMissingTransportDoesNotBlockLifecycle(t *testing.T) {
	m, a := entryObserverManager(t)
	calls := 0
	entry := momDataCallbackEntryObserver(m, func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
		calls++
		return nil
	})
	event := entryInteractionEvent()
	event.GetReceive().Metadata = nil
	if err := entry(context.Background(), "f", 7, event); err != nil {
		t.Fatal("telemetry blocked callback ENTRY", err)
	}
	if calls != 1 {
		t.Fatal("required lifecycle observer did not run")
	}
	if _, err := a.Snapshot(context.Background(), "f", 7, mom.InteractionsReceived); !errors.Is(err, mom.ErrCounterDataUnavailable) {
		t.Fatal("missing transport invented reliable/zero", err)
	}
}

func TestMOMEntryObserverControlForwardingIsNotReceivedTraffic(t *testing.T) {
	m, a := entryObserverManager(t)
	calls := 0
	entry := momDataCallbackEntryObserver(m, func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
		calls++
		return nil
	})
	event := &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: &rtiv1.SynchronizationPointAnnounced{Label: testReadyLabel}}}
	if err := entry(context.Background(), "f", 7, event); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || momEntryCountedData(event) || entryReceivedCount(t, a) != 0 {
		t.Fatal("control counted as received data")
	}
}

func TestMOMEntryObserverTelemetryFailureDoesNotBlockCallbackBody(t *testing.T) {
	for _, name := range []string{"unknownTransportation", "partitionOverflow", "missingRuntimeBaseline"} {
		t.Run(name, func(t *testing.T) {
			m, a := entryObserverManager(t)
			event := entryInteractionEvent()
			state := m.ExportState("f")
			switch name {
			case "unknownTransportation":
				event.GetReceive().Metadata.TransportationType = rtiv1.TransportationType(0).Enum()
			case "partitionOverflow":
				state.Traffic.Federates[0].Counts = append(state.Traffic.Federates[0].Counts, mom.TrafficCountState{Kind: mom.InteractionsReceived, Class: 9, Count: math.MaxInt32})
			case "missingRuntimeBaseline":
				state.Federates[0].RuntimeMetrics = nil
			}
			commitState, err := m.PrepareImportState("f", state)
			if err != nil {
				t.Fatal(err)
			}
			commitState()
			lifecycle, errorsObserved, bodies := 0, 0, 0
			entry := momDataCallbackEntryObserver(m, func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error {
				lifecycle++
				return nil
			}, func(error) { errorsObserved++ })
			entryHandled := false
			receipts, err := grpcadapter.NewCallbackReceipts(grpcadapter.CallbackReceiptOptions{
				Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
				ObserveEntry: func(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) error {
					if err := entry(ctx, fed, h, event); err != nil {
						return err
					}
					entryHandled = momEntryCountedData(event)
					return nil
				},
				Observe: func(_ context.Context, fed core.FederationName, h core.FederateHandle, _ *rtiv1.FederateEvent, _ bool, _ string) error {
					return m.RecordCallbackCompletion(fed, h, mom.CallbackCompletion{Invocation: mom.CallbackInvocation{Service: "receiveInteraction"}, DataCountedAtEntry: entryHandled})
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			ticket, commit, err := receipts.PrepareRequired(context.Background(), "f", 7, event)
			if err != nil {
				t.Fatal(err)
			}
			commit(true)
			// Follow the SDK gate: body may run only after accepted ENTRY.
			if err := receipts.ReportEntry(context.Background(), "f", 7, ticket); err != nil {
				t.Fatal("telemetry blocked body", err)
			}
			bodies++
			if err := receipts.ReportEntry(context.Background(), "f", 7, ticket); err != nil {
				t.Fatal(err)
			}
			if lifecycle != 1 || errorsObserved != 1 || bodies != 1 || !entryHandled {
				t.Fatal(lifecycle, errorsObserved, bodies, entryHandled)
			}
			if err := receipts.Report(context.Background(), "f", 7, ticket, true, ""); err != nil {
				t.Fatal("completion retried unavailable counter", err)
			}
			if _, err := a.Snapshot(context.Background(), "f", 7, mom.InteractionsReceived); !errors.Is(err, mom.ErrCounterDataUnavailable) {
				t.Fatal("unavailable partition invented zero", err)
			}
			metrics, err := m.FederateMetricValues(context.Background(), "f", 7)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := metrics.Values["HLAinteractionsReceived"]; ok || metrics.Unavailable["HLAinteractionsReceived"] == "" {
				t.Fatal("unavailable aggregate invented numeric value", metrics)
			}
		})
	}
}
