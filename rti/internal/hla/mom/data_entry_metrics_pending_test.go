package mom_test

import (
	"context"
	"encoding/binary"
	"testing"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

type pendingEntryOutbox struct{}

func (pendingEntryOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

func pendingEntryManager(t *testing.T, enabled bool) *mom.Manager {
	t.Helper()
	m, err := mom.New(mom.Options{Outbox: pendingEntryOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetRuntimeMetricsEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDataEntryMetricsEnabled(enabled); err != nil {
		t.Fatal(err)
	}
	if err := m.SetTrafficAccumulator(mom.NewTrafficAccumulator()); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateJoined(context.Background(), "f", 7, "seven", "test"); err != nil {
		t.Fatal(err)
	}
	return m
}

func pendingEntryMetric(t *testing.T, m *mom.Manager, name string, want uint32) {
	t.Helper()
	values, err := m.FederateMetricValues(context.Background(), "f", 7)
	if err != nil {
		t.Fatal(err)
	}
	encoded := values.Values[name]
	if len(encoded) != 4 || binary.BigEndian.Uint32(encoded) != want || values.Unavailable[name] != "" {
		t.Errorf("%s: encoded=%x unavailable=%q want=%d", name, encoded, values.Unavailable[name], want)
	}
}

func TestMOMDataEntryMetricsPendingOutcomeSnapshot(t *testing.T) {
	for _, service := range []string{"receiveInteraction", "reflectAttributeValues"} {
		t.Run(service, func(t *testing.T) {
			m := pendingEntryManager(t, true)
			entry := mom.DataCallbackEntry{Service: service, Transportation: core.TransportTypeReliable}
			metric := "HLAinteractionsReceived"
			var event *rtiv1.FederateEvent
			if service == "receiveInteraction" {
				entry.InteractionClass = 9
				event = &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{InteractionClassHandle: 9}}}
			} else {
				entry.Object, entry.ObjectClass, metric = 5, 9, "HLAreflectionsReceived"
				event = &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Reflect{Reflect: &rtiv1.ReflectAttributeValues{ObjectHandle: 5, ObjectClassHandle: 9}}}
			}
			entries := 0
			receipts, err := grpcadapter.NewCallbackReceipts(grpcadapter.CallbackReceiptOptions{
				Authenticate: func(_ context.Context, fed core.FederationName, h core.FederateHandle) error {
					if fed != "f" || h != 7 {
						return core.ErrFederateNotJoined
					}
					return nil
				},
				Pending: m.CallbackPending,
				ObserveEntry: func(_ context.Context, fed core.FederationName, h core.FederateHandle, _ *rtiv1.FederateEvent) error {
					if err := m.RecordDataCallbackEntry(fed, h, entry); err != nil {
						return err
					}
					entries++
					return nil
				},
				Observe: func(_ context.Context, fed core.FederationName, h core.FederateHandle, _ *rtiv1.FederateEvent, _ bool, _ string) error {
					return m.RecordCallbackCompletion(fed, h, mom.CallbackCompletion{Invocation: mom.CallbackInvocation{Service: service, Object: entry.Object, ObjectClass: entry.ObjectClass}, DataCountedAtEntry: true})
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
			pendingEntryMetric(t, m, metric, 0)
			pendingEntryMetric(t, m, "HLAobjectInstancesReflected", 0)
			if err := receipts.ReportEntry(context.Background(), "f", 7, ticket); err != nil {
				t.Fatal(err)
			}
			pendingEntryMetric(t, m, metric, 1)
			if service == "reflectAttributeValues" {
				pendingEntryMetric(t, m, "HLAobjectInstancesReflected", 1)
			}
			if err := receipts.ReportEntry(context.Background(), "f", 7, ticket); err != nil {
				t.Fatal(err)
			}
			if entries != 1 {
				t.Fatal("duplicate ENTRY applied", entries)
			}
			pendingEntryMetric(t, m, metric, 1)
			if err := receipts.Report(context.Background(), "f", 7, ticket, true, ""); err != nil {
				t.Fatal(err)
			}
			pendingEntryMetric(t, m, metric, 1)
		})
	}
}

func TestMOMDataEntryMetricsLegacyAndControlPendingRemainUnavailable(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		m := pendingEntryManager(t, enabled)
		m.CallbackPending("f", 7, 1)
		values, err := m.FederateMetricValues(context.Background(), "f", 7)
		if err != nil {
			t.Fatal(err)
		}
		names := []string{"HLAobjectInstancesDiscovered", "HLAobjectInstancesRemoved"}
		if !enabled {
			names = append(names, "HLAinteractionsReceived", "HLAreflectionsReceived", "HLAobjectInstancesReflected")
		}
		for _, name := range names {
			if _, exists := values.Values[name]; exists || values.Unavailable[name] != "callback receipts outstanding" {
				t.Errorf("enabled=%t %s legacy/control pending coverage=%v", enabled, name, values)
			}
		}
	}
}

func TestMOMDataEntryMetricsFailuresOverridePendingExemption(t *testing.T) {
	m := pendingEntryManager(t, true)
	m.CallbackPending("f", 7, 1)
	cause := mom.ErrCounterOverflow
	m.DataCallbackEntryUnavailable("f", 7, mom.DataCallbackEntry{Service: "reflectAttributeValues"}, cause)
	values, err := m.FederateMetricValues(context.Background(), "f", 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"HLAreflectionsReceived", "HLAobjectInstancesReflected"} {
		if _, exists := values.Values[name]; exists || values.Unavailable[name] != cause.Error() {
			t.Fatal("failure precedence lost", name, values)
		}
	}
	pendingEntryMetric(t, m, "HLAinteractionsReceived", 0)
}

func TestMOMDataEntryMetricsCannotChangeAfterJoin(t *testing.T) {
	m := pendingEntryManager(t, false)
	if err := m.SetDataEntryMetricsEnabled(true); err == nil {
		t.Fatal("changed boundary after join")
	}
}
