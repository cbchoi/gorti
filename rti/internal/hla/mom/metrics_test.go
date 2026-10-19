package mom

import (
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type metricOutbox struct{}

func (metricOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

func TestMOMMetricsActualInvocationAndPendingCoverage(t *testing.T) {
	m, _ := New(Options{Outbox: metricOutbox{}})
	if err := m.SetRuntimeMetricsEnabled(true); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.FederateJoined(ctx, "f", 1, "a", "t"); err != nil {
		t.Fatal(err)
	}
	for _, success := range []bool{false, true, true} {
		if err := m.RecordServiceInvocation("f", 1, ServiceInvocation{Service: "updateAttributeValues", Success: success}); err != nil {
			t.Fatal(err)
		}
	}
	m.CallbackPending("f", 1, 1)
	got, err := m.FederateMetricValues(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(got.Values["HLAupdatesSent"]) != 2 {
		t.Fatal("failed service counted", got)
	}
	if _, ok := got.Values["HLAreflectionsReceived"]; ok || got.Unavailable["HLAreflectionsReceived"] == "" {
		t.Fatal("pending callback fabricated0", got)
	}
	if err := m.RecordCallbackInvocation("f", 1, CallbackInvocation{Service: "reflectAttributeValues", Object: 5, ObjectClass: 7}); err != nil {
		t.Fatal(err)
	}
	m.CallbackPending("f", 1, -1)
	if err := m.RecordCallbackInvocation("f", 1, CallbackInvocation{Service: "reflectAttributeValues", Object: 5, ObjectClass: 7}); err != nil {
		t.Fatal(err)
	}
	got, err = m.FederateMetricValues(ctx, "f", 1)
	if err != nil || binary.BigEndian.Uint32(got.Values["HLAreflectionsReceived"]) != 2 || binary.BigEndian.Uint32(got.Values["HLAobjectInstancesReflected"]) != 1 {
		t.Fatal("callback actual/distinct metrics", got, err)
	}
	if _, ok := got.Values["HLAfederateHost"]; ok {
		t.Fatal("host fabricated")
	}
}

func TestMOMMetricsOverflowAndLegacyBaseline(t *testing.T) {
	m, _ := New(Options{Outbox: metricOutbox{}})
	_ = m.SetRuntimeMetricsEnabled(true)
	ctx := context.Background()
	_ = m.FederateJoined(ctx, "f", 1, "a", "t")
	m.mu.Lock()
	if m.fed["f"].federates[1].runtimeMetrics == nil {
		m.mu.Unlock()
		t.Fatal("runtime source not initialized")
	}
	m.fed["f"].federates[1].runtimeMetrics.Counters["HLAupdatesSent"] = math.MaxInt32
	m.mu.Unlock()
	if err := m.RecordServiceInvocation("f", 1, ServiceInvocation{Service: "updateAttributeValues", Success: true}); err == nil {
		t.Fatal("overflow accepted")
	}
	got, err := m.FederateMetricValues(ctx, "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Values["HLAupdatesSent"]; ok || got.Unavailable["HLAupdatesSent"] == "" {
		t.Fatal("overflow fabricated numeric count")
	}
	m.mu.Lock()
	m.fed["f"].federates[1].runtimeMetrics = nil
	m.mu.Unlock()
	got, err = m.FederateMetricValues(ctx, "f", 1)
	if err != nil || got.Unavailable["HLAobjectInstancesRegistered"] == "" {
		t.Fatal("missing baseline fabricated0", got, err)
	}
}
