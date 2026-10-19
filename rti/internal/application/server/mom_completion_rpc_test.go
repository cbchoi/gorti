package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	"google.golang.org/grpc/metadata"
)

func TestMOMCompletionSourcesProductionRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	srv, conn, _ := restartCheckpointServer(t, t.TempDir(), 41)
	defer srv.sessions.Close()
	const fed core.FederationName = "completed-sources"
	defer func() {
		if err := srv.momMgr.FederationDestroyed(context.Background(), fed); err != nil {
			t.Error(err)
		}
	}()
	xml, err := os.ReadFile(filepath.Join("..", "..", "..", "pkg", "fom", "exporter", "testdata", "retained.xml"))
	if err != nil {
		t.Fatal(err)
	}
	federations := rtiv1.NewFederationServiceClient(conn)
	created, err := federations.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{{Path: "retained.xml", Xml: xml}}})
	if err != nil {
		t.Fatal(err)
	}
	joined, err := federations.JoinFederation(metadata.AppendToOutgoingContext(ctx, "gorti-federate-host-bin", "local-test.example"), &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: string(fed), FederateName: "member", ExpectedFederationGeneration: &created.FederationGeneration})
	if err != nil {
		t.Fatal(err)
	}
	moms := rtiv1.NewMomServiceClient(conn)
	query := func() *rtiv1.QueryFederationAttributesResponse {
		t.Helper()
		result, err := moms.QueryFederationAttributes(ctx, &rtiv1.QueryFederationAttributesRequest{WireVersion: 1, FederationName: string(fed)})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	initial := query()
	if value, ok := initial.AttributeValues["HLAlastSaveName"]; !ok || catalogRPCString(t, value) != "" {
		t.Fatalf("fresh save name unavailable: %v", initial)
	}
	if !bytes.Equal(initial.AttributeValues["HLAlastSaveTime"], make([]byte, 4)) {
		t.Fatalf("no-save time not undefined: %v", initial)
	}
	fdd := catalogRPCString(t, initial.AttributeValues["HLAcurrentFDD"])
	if !strings.Contains(fdd, "1.0000000000000000000000000001") || !strings.Contains(fdd, "HLAmanager") {
		t.Fatal("FDD lost retained values or embedded MIM")
	}
	attributes, err := moms.QueryFederateAttributes(ctx, &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: joined.FederateHandle})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"HLAtimeGrantedTime", "HLAtimeAdvancingTime"} {
		if len(attributes.AttributeValues[name]) != 4 || attributes.UnavailableAttributes[name] != "" {
			t.Fatalf("elapsed source not wired: %s: %v", name, attributes)
		}
	}
	if binary.BigEndian.Uint32(attributes.AttributeValues["HLAtimeAdvancingTime"]) != 0 {
		t.Fatal("newly joined member is advancing")
	}
	if catalogRPCString(t, attributes.AttributeValues["HLAfederateHost"]) != "local-test.example" {
		t.Fatal("host missing")
	}
	saves := rtiv1.NewSavepointServiceClient(conn)
	if _, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: joined.FederateHandle, Label: testCheckpointLabel}); err != nil {
		t.Fatal(err)
	}
	response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: joined.FederateHandle}
	if _, err := saves.FederateSaveBegun(ctx, response); err != nil {
		t.Fatal(err)
	}
	if _, err := saves.FederateSaveComplete(ctx, response); err != nil {
		t.Fatal(err)
	}
	saved := query()
	if catalogRPCString(t, saved.AttributeValues["HLAlastSaveName"]) != testCheckpointLabel {
		t.Fatal("completed save is absent from MOM")
	}
	if !bytes.Equal(saved.AttributeValues["HLAlastSaveTime"], make([]byte, 4)) {
		t.Fatal("immediate save fabricated a timestamp")
	}

	model, err := srv.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class, ok := model.LookupObjectClass("HLAmanager.HLAfederation")
	if !ok {
		t.Fatal("MOM federation class missing")
	}
	fddHandle, ok := model.LookupAttribute(class, "HLAcurrentFDD")
	if !ok {
		t.Fatal("FDD attribute missing")
	}
	stream, err := rtiv1.NewStreamServiceClient(conn).Events(ctx, &rtiv1.EventsRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: joined.FederateHandle})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.declMgr.SubscribeObjectClassAttributes(ctx, fed, core.FederateHandle(joined.FederateHandle), class, []core.AttributeHandle{fddHandle}); err != nil {
		t.Fatal(err)
	}
	if err := srv.momMgr.(*mom.Manager).RefreshMetrics(ctx, fed, 0); err != nil {
		t.Fatal(err)
	}
	for {
		event, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if reflect := event.GetReflect(); reflect != nil {
			if value, exists := reflect.Attributes[uint64(fddHandle)]; exists {
				if catalogRPCString(t, value) != fdd {
					t.Fatal("MOM callback differs from query")
				}
				break
			}
		}
	}
}

func TestMOMIncompleteFDDRemainsUnavailable(t *testing.T) {
	ctx := context.Background()
	srv, conn, _ := restartCheckpointServer(t, t.TempDir(), 42)
	defer srv.sessions.Close()
	defer func() {
		if err := srv.momMgr.FederationDestroyed(ctx, "incomplete"); err != nil {
			t.Error(err)
		}
	}()
	_, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "incomplete", FomModules: []*rtiv1.FOMModule{{Path: "minimal.xml", Xml: []byte(`<objectModel/>`)}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := rtiv1.NewMomServiceClient(conn).QueryFederationAttributes(ctx, &rtiv1.QueryFederationAttributesRequest{WireVersion: 1, FederationName: "incomplete"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result.AttributeValues["HLAcurrentFDD"]; exists || result.UnavailableAttributes["HLAcurrentFDD"] == "" {
		t.Fatalf("incomplete FDD advertised as complete: %v", result)
	}
	if _, exists := result.AttributeValues["HLAlastSaveName"]; !exists {
		t.Fatal("FDD unavailable disabled unrelated save metrics")
	}
}
