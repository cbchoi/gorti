package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
	timedomain "github.com/cbchoi/gorti/rti/internal/hla/time"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func momCompletionReviewServer(t *testing.T, names ...core.FederationName) (context.Context, *rtid, *stdgrpc.ClientConn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	dir := t.TempDir()
	srv, conn, stop := restartCheckpointServer(t, dir, 63)
	t.Cleanup(func() {
		srv.sessions.Close()
		for _, name := range names {
			_ = srv.momMgr.FederationDestroyed(context.Background(), name)
		}
		stop()
	})
	return ctx, srv, conn, dir
}

func momCompletionReviewModule(t *testing.T, class string) *rtiv1.FOMModule {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "pkg", "fom", "exporter", "testdata", "retained.xml"))
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("<name>Parent</name>"), []byte("<name>"+class+"</name>"))
	return &rtiv1.FOMModule{Path: class + ".xml", Xml: data}
}

func momCompletionReviewCreate(ctx context.Context, t *testing.T, conn *stdgrpc.ClientConn, fed, class string) uint64 {
	t.Helper()
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{
		WireVersion: 1, FederationName: fed, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, class)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return created.FederationGeneration
}

func momCompletionReviewJoin(ctx context.Context, t *testing.T, conn *stdgrpc.ClientConn, fed, name, host string, generation uint64) *rtiv1.JoinFederationResponse {
	t.Helper()
	if host != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "gorti-federate-host-bin", host)
	}
	joined, err := rtiv1.NewFederationServiceClient(conn).JoinFederation(ctx, &rtiv1.JoinFederationRequest{
		WireVersion: 1, FederationName: fed, FederateName: name, ExpectedFederationGeneration: &generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	return joined
}

func momCompletionReviewQuery(ctx context.Context, t *testing.T, conn *stdgrpc.ClientConn, fed string) *rtiv1.QueryFederationAttributesResponse {
	t.Helper()
	// This is the existing read-only, named-federation API, not a new caller-auth contract.
	result, err := rtiv1.NewMomServiceClient(conn).QueryFederationAttributes(ctx, &rtiv1.QueryFederationAttributesRequest{WireVersion: 1, FederationName: fed})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func momCompletionReviewSave(ctx context.Context, t *testing.T, conn *stdgrpc.ClientConn, fed, label string, handle uint64) {
	t.Helper()
	saves := rtiv1.NewSavepointServiceClient(conn)
	if _, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: handle, Label: label}); err != nil {
		t.Fatal(err)
	}
	response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: handle}
	if _, err := saves.FederateSaveBegun(ctx, response); err != nil {
		t.Fatal(err)
	}
	if _, err := saves.FederateSaveComplete(ctx, response); err != nil {
		t.Fatal(err)
	}
}

func momCompletionReviewSubscription(ctx context.Context, t *testing.T, srv *rtid, conn *stdgrpc.ClientConn, fed string, handle uint64, names ...string) (<-chan []core.OutboundEvent, map[string]core.AttributeHandle) {
	t.Helper()
	model, err := srv.foms.Get(ctx, core.FederationName(fed))
	if err != nil {
		t.Fatal(err)
	}
	class, ok := model.LookupObjectClass("HLAmanager.HLAfederation")
	if !ok {
		t.Fatal("MOM federation class missing")
	}
	attributes := map[string]core.AttributeHandle{}
	var handles []uint64
	for _, name := range names {
		h, ok := model.LookupAttribute(class, name)
		if !ok {
			t.Fatal("MOM attribute missing:", name)
		}
		attributes[name] = h
		handles = append(handles, uint64(h))
	}
	channel, release, err := srv.outbox.Subscribe(ctx, core.FederationName(fed), core.FederateHandle(handle))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	_, err = rtiv1.NewDeclarationServiceClient(conn).SubscribeObjectClassAttributes(ctx, &rtiv1.SubObjAttrsRequest{
		WireVersion: 1, FederationName: fed, FederateHandle: handle, ObjectClassHandle: uint64(class), AttributeHandles: handles,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Prove the configured fanout works, then discard setup traffic. Assertions below
	// must be satisfied by actual lifecycle actions, never another manual refresh.
	if err := srv.momMgr.(*mom.Manager).RefreshMetrics(ctx, core.FederationName(fed), 0); err != nil {
		t.Fatal(err)
	}
	initial := momCompletionReviewReflections(t, channel)
	for name, attr := range attributes {
		if _, exists := initial[attr]; !exists {
			t.Fatal("subscription setup did not publish", name)
		}
	}
	return channel, attributes
}

func momCompletionReviewReflections(t *testing.T, channel <-chan []core.OutboundEvent) map[core.AttributeHandle][]byte {
	t.Helper()
	values := map[core.AttributeHandle][]byte{}
	// The fixture uses batch size one; completed RPC hooks enqueue synchronously.
	for _, event := range drainScheduledEvents(channel) {
		wire, err := grpcadapter.EncodeOutboundEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		if reflect := wire.GetReflect(); reflect != nil {
			for attr, value := range reflect.Attributes {
				values[core.AttributeHandle(attr)] = value
			}
		}
	}
	return values
}

func TestMOMCompletionReviewCreateHistoryFailureRollsBack(t *testing.T) {
	const fed core.FederationName = "review-create-failure"
	ctx, srv, conn, dir := momCompletionReviewServer(t, fed)
	blocker := filepath.Join(dir, "history")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{
		WireVersion: 1, FederationName: string(fed), FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "FailedCandidate")},
	})
	if err == nil {
		t.Fatal("history storage failure did not reject federation creation")
	}
	if _, exists := srv.fedMgr.GenerationFor(fed); exists {
		t.Error("failed create left a live federation")
	}
	if _, err := srv.foms.Get(ctx, fed); !errors.Is(err, core.ErrFederationNotFound) {
		t.Errorf("failed create left a FOM: %v", err)
	}
	if _, exists := srv.momMgr.QueryFederation(fed); exists {
		t.Error("failed create left a MOM federation")
	}
	if result := momCompletionReviewQuery(ctx, t, conn, string(fed)); len(result.AttributeValues) != 0 || len(result.FomModuleNames) != 0 {
		t.Error("failed create leaked query values:", result)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	momCompletionReviewCreate(ctx, t, conn, string(fed), "RecoveredCandidate")
	result := momCompletionReviewQuery(ctx, t, conn, string(fed))
	fdd := catalogRPCString(t, result.AttributeValues["HLAcurrentFDD"])
	if !strings.Contains(fdd, "<name>RecoveredCandidate</name>") || strings.Contains(fdd, "<name>FailedCandidate</name>") {
		t.Fatal("retry published stale failed FOM")
	}
	if got := catalogRPCString(t, result.AttributeValues["HLAlastSaveName"]); got != "" {
		t.Fatalf("fresh retry history = %q", got)
	}
}

func TestMOMCompletionReviewProvidersAreFederationScoped(t *testing.T) {
	const left, right core.FederationName = "review-left", "review-right"
	ctx, srv, conn, _ := momCompletionReviewServer(t, left, right)
	leftGeneration := momCompletionReviewCreate(ctx, t, conn, string(left), "ReviewAlpha")
	rightGeneration := momCompletionReviewCreate(ctx, t, conn, string(right), "ReviewBeta")
	a := momCompletionReviewJoin(ctx, t, conn, string(left), "member", "alpha.example", leftGeneration)
	b := momCompletionReviewJoin(ctx, t, conn, string(right), "member", "beta.example", rightGeneration)
	if a.FederateHandle != b.FederateHandle {
		t.Fatal("fixture must exercise equal numeric handles in different federations")
	}
	tm := srv.timeMgr.(*timedomain.Manager)
	for i, name := range []core.FederationName{left, right} {
		state, err := tm.ExportState(name, nil)
		if err != nil || len(state.Federates) != 1 || state.Federates[0].Elapsed == nil {
			t.Fatalf("join did not establish elapsed origin in %s: %+v %v", name, state, err)
		}
		// Deterministic distinct histories detect a provider keyed only by handle.
		state.Federates[0].Elapsed.Advancing = time.Duration(11+18*i) * time.Millisecond
		if err := tm.ImportState(name, state, nil); err != nil {
			t.Fatal(err)
		}
	}
	momCompletionReviewSave(ctx, t, conn, string(left), "left-only", a.FederateHandle)
	for _, tc := range []struct {
		fed, ownClass, otherClass, host, save string
		advancing                             uint32
	}{
		{string(left), "ReviewAlpha", "ReviewBeta", "alpha.example", "left-only", 11},
		{string(right), "ReviewBeta", "ReviewAlpha", "beta.example", "", 29},
	} {
		result := momCompletionReviewQuery(ctx, t, conn, tc.fed)
		fdd := catalogRPCString(t, result.AttributeValues["HLAcurrentFDD"])
		if !strings.Contains(fdd, "<name>"+tc.ownClass+"</name>") || strings.Contains(fdd, "<name>"+tc.otherClass+"</name>") {
			t.Errorf("FDD provider crossed federation boundary for %s", tc.fed)
		}
		if got := catalogRPCString(t, result.AttributeValues["HLAlastSaveName"]); got != tc.save {
			t.Errorf("history for %s = %q, want %q", tc.fed, got, tc.save)
		}
		for read := 0; read < 2; read++ {
			attrs, err := rtiv1.NewMomServiceClient(conn).QueryFederateAttributes(ctx, &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: tc.fed, FederateHandle: a.FederateHandle})
			if err != nil {
				t.Fatal(err)
			}
			if got := catalogRPCString(t, attrs.AttributeValues["HLAfederateHost"]); got != tc.host {
				t.Errorf("host for %s = %q, want %q", tc.fed, got, tc.host)
			}
			encoded := attrs.AttributeValues["HLAtimeAdvancingTime"]
			if len(encoded) != 4 || binary.BigEndian.Uint32(encoded) != tc.advancing {
				t.Errorf("elapsed history crossed federation or query consumed it: %s %x", tc.fed, encoded)
			}
		}
	}
}

func TestMOMCompletionReviewAbortPreservesCompletedHistory(t *testing.T) {
	const fed core.FederationName = "review-abort"
	ctx, _, conn, _ := momCompletionReviewServer(t, fed)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "ReviewSaved")
	member := momCompletionReviewJoin(ctx, t, conn, string(fed), "member", "", generation)
	momCompletionReviewSave(ctx, t, conn, string(fed), "committed", member.FederateHandle)
	before := momCompletionReviewQuery(ctx, t, conn, string(fed))
	saves := rtiv1.NewSavepointServiceClient(conn)
	if _, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: member.FederateHandle, Label: "aborted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := saves.AbortFederationSave(ctx, &rtiv1.AbortFederationSaveRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: &member.FederateHandle}); err != nil {
		t.Fatal(err)
	}
	after := momCompletionReviewQuery(ctx, t, conn, string(fed))
	for _, name := range []string{"HLAlastSaveName", "HLAlastSaveTime"} {
		if !bytes.Equal(before.AttributeValues[name], after.AttributeValues[name]) || after.UnavailableAttributes[name] != "" {
			t.Error("abort changed completed history:", name, after)
		}
	}
	if catalogRPCString(t, after.AttributeValues["HLAlastSaveName"]) != "committed" || catalogRPCString(t, after.AttributeValues["HLAAnnexSaveName"]) != "" {
		t.Fatal("abort retained wrong last/next label")
	}
	momCompletionReviewSave(ctx, t, conn, string(fed), "after-abort", member.FederateHandle)
	if catalogRPCString(t, momCompletionReviewQuery(ctx, t, conn, string(fed)).AttributeValues["HLAlastSaveName"]) != "after-abort" {
		t.Fatal("abort prevented subsequent history publication")
	}
}

func TestMOMCompletionReviewDynamicFDDPublishesToSubscribers(t *testing.T) {
	const fed, other core.FederationName = "review-dynamic", "review-static"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed, other)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "ReviewAlpha")
	otherGeneration := momCompletionReviewCreate(ctx, t, conn, string(other), "ReviewBeta")
	a := momCompletionReviewJoin(ctx, t, conn, string(fed), "observer", "", generation)
	b := momCompletionReviewJoin(ctx, t, conn, string(other), "observer", "", otherGeneration)
	left, leftAttrs := momCompletionReviewSubscription(ctx, t, srv, conn, string(fed), a.FederateHandle, "HLAcurrentFDD")
	right, rightAttrs := momCompletionReviewSubscription(ctx, t, srv, conn, string(other), b.FederateHandle, "HLAcurrentFDD")
	before := momCompletionReviewQuery(ctx, t, conn, string(other)).AttributeValues["HLAcurrentFDD"]
	extra := []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>ReviewAlpha</name><objectClass><name>ReviewExtension</name><sharing>PublishSubscribe</sharing><attribute><name>ReviewAdded</name><dataType>HLAinteger32BE</dataType><transportation>HLAreliable</transportation><order>Receive</order></attribute></objectClass></objectClass></objectClass></objects></objectModel>`)
	_, err := rtiv1.NewFederationServiceClient(conn).JoinFederation(ctx, &rtiv1.JoinFederationRequest{
		WireVersion: 1, FederationName: string(fed), FederateName: "extender", AdditionalFomModules: []*rtiv1.FOMModule{{Path: "extension.xml", Xml: extra}},
		ExpectedFederationGeneration: &generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	current := momCompletionReviewQuery(ctx, t, conn, string(fed)).AttributeValues["HLAcurrentFDD"]
	if !strings.Contains(catalogRPCString(t, current), "<name>ReviewAdded</name>") {
		t.Fatal("successful join is absent from queried FDD")
	}
	if !bytes.Equal(before, momCompletionReviewQuery(ctx, t, conn, string(other)).AttributeValues["HLAcurrentFDD"]) {
		t.Error("dynamic FOM changed another federation's queried FDD")
	}
	if _, exists := momCompletionReviewReflections(t, right)[rightAttrs["HLAcurrentFDD"]]; exists {
		t.Error("dynamic FDD leaked to another federation's subscriber")
	}
	if actual, exists := momCompletionReviewReflections(t, left)[leftAttrs["HLAcurrentFDD"]]; !exists || !bytes.Equal(actual, current) {
		t.Error("successful FOM extension changed query but did not publish current FDD to its subscribed member")
	}
}

func TestMOMCompletionReviewSuccessfulSavePublishesHistory(t *testing.T) {
	const fed core.FederationName = "review-save-publication"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "ReviewSaved")
	member := momCompletionReviewJoin(ctx, t, conn, string(fed), "observer", "", generation)
	channel, attrs := momCompletionReviewSubscription(ctx, t, srv, conn, string(fed), member.FederateHandle, "HLAlastSaveName", "HLAlastSaveTime")
	if _, err := (savebundle.Object{Manager: srv.objReg}).Marshal(fed); err != nil {
		t.Fatalf("MOM subscription left object state that cannot be saved: %v", err)
	}
	if _, err := (savebundle.MOM{Manager: srv.momMgr.(*mom.Manager)}).Marshal(fed); err != nil {
		t.Fatalf("MOM subscription left management state that cannot be saved: %v", err)
	}
	momCompletionReviewSave(ctx, t, conn, string(fed), "published-save", member.FederateHandle)
	query := momCompletionReviewQuery(ctx, t, conn, string(fed))
	if catalogRPCString(t, query.AttributeValues["HLAlastSaveName"]) != "published-save" {
		_, objectErr := (savebundle.Object{Manager: srv.objReg}).Marshal(fed)
		_, momErr := (savebundle.MOM{Manager: srv.momMgr.(*mom.Manager)}).Marshal(fed)
		t.Logf("post-save snapshots: object=%v MOM=%v", objectErr, momErr)
		for {
			select {
			case batch := <-channel:
				for _, event := range batch {
					t.Logf("save callback: %T %+v", event, event)
				}
			default:
				t.Fatal("completed history not available to query")
			}
		}
	}
	values := momCompletionReviewReflections(t, channel)
	for _, name := range []string{"HLAlastSaveName", "HLAlastSaveTime"} {
		if value, exists := values[attrs[name]]; !exists || !bytes.Equal(value, query.AttributeValues[name]) {
			t.Error("successful save changed query but did not publish", name)
		}
	}
}

func TestMOMCompletionReviewLateSubscriptionReceivesHostAndFDD(t *testing.T) {
	const fed core.FederationName = "review-late-subscription"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "ReviewLate")
	member := momCompletionReviewJoin(ctx, t, conn, string(fed), "observer", "late-host.example", generation)
	federation := momCompletionReviewQuery(ctx, t, conn, string(fed))
	federate, err := rtiv1.NewMomServiceClient(conn).QueryFederateAttributes(ctx, &rtiv1.QueryFederateAttributesRequest{
		WireVersion: 1, FederationName: string(fed), FederateHandle: member.FederateHandle,
	})
	if err != nil {
		t.Fatal(err)
	}
	if catalogRPCString(t, federate.AttributeValues["HLAfederateHost"]) != "late-host.example" {
		t.Fatal("joined host is unavailable to query")
	}
	if !strings.Contains(catalogRPCString(t, federation.AttributeValues["HLAcurrentFDD"]), "<name>ReviewLate</name>") {
		t.Fatal("current FDD is unavailable to query")
	}
	model, err := srv.foms.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	channel, release, err := srv.outbox.Subscribe(ctx, fed, core.FederateHandle(member.FederateHandle))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	// Join has already completed. Only the subscription RPC may publish here.
	for _, tc := range []struct {
		class, attribute string
		want             []byte
	}{
		{"HLAmanager.HLAfederate", "HLAfederateHost", federate.AttributeValues["HLAfederateHost"]},
		{"HLAmanager.HLAfederation", "HLAcurrentFDD", federation.AttributeValues["HLAcurrentFDD"]},
	} {
		class, ok := model.LookupObjectClass(tc.class)
		if !ok {
			t.Fatal("MOM class missing:", tc.class)
		}
		attribute, ok := model.LookupAttribute(class, tc.attribute)
		if !ok {
			t.Fatal("MOM attribute missing:", tc.attribute)
		}
		_, err := rtiv1.NewDeclarationServiceClient(conn).SubscribeObjectClassAttributes(ctx, &rtiv1.SubObjAttrsRequest{
			WireVersion: 1, FederationName: string(fed), FederateHandle: member.FederateHandle,
			ObjectClassHandle: uint64(class), AttributeHandles: []uint64{uint64(attribute)},
		})
		if err != nil {
			t.Fatal(err)
		}
		actual, exists := momCompletionReviewReflections(t, channel)[attribute]
		if !exists || !bytes.Equal(actual, tc.want) {
			t.Errorf("late subscription did not receive current %s without manual refresh", tc.attribute)
		}
	}
}
