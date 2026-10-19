package grpc

import (
	"context"
	"encoding/binary"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

type momMetricTestOutbox struct{}

func (momMetricTestOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

func TestMOMAdapterSeparatesActualCallbackMetricFromLegacyAcceptance(t *testing.T) {
	ctx := context.Background()
	manager, _ := mom.New(mom.Options{Outbox: momMetricTestOutbox{}})
	_ = manager.SetRuntimeMetricsEnabled(true)
	_ = manager.FederateJoined(ctx, "f", 1, "a", "t")
	manager.CallbackPending("f", 1, 1)
	service := newMomService(manager)
	response, err := service.QueryFederateAttributes(ctx, &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1})
	if err != nil || response.GetUnavailableAttributes()["HLAreflectionsReceived"] == "" {
		t.Fatal("pending callback not explicitly unavailable", response, err)
	}
	_ = manager.RecordCallbackInvocation("f", 1, mom.CallbackInvocation{Service: "reflectAttributeValues", Object: 3, ObjectClass: 7})
	manager.CallbackPending("f", 1, -1)
	response, err = service.QueryFederateAttributes(ctx, &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: "f", FederateHandle: 1})
	if err != nil || len(response.GetAttributeValues()["HLAreflectionsReceived"]) != 4 || binary.BigEndian.Uint32(response.GetAttributeValues()["HLAreflectionsReceived"]) != 1 || response.ReflectionsReceived != 0 {
		t.Fatal("actual callback aliased legacy accepted counter", response, err)
	}
	if response.GetUnavailableAttributes()["HLAfederateHost"] == "" {
		t.Fatal("missing host invented")
	}
	federation, err := service.QueryFederationAttributes(ctx, &rtiv1.QueryFederationAttributesRequest{WireVersion: 1, FederationName: "f"})
	if err != nil || len(federation.GetAttributeValues()["HLAfederationName"]) == 0 || federation.GetUnavailableAttributes()["HLAcurrentFDD"] == "" {
		t.Fatal("federation measurement maps", federation, err)
	}
}
