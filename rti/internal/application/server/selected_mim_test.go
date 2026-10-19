package server

import (
	"bytes"
	"context"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/federate"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// Construct the additive field by wire number so the regression also executes
// against the pre-selection descriptor and demonstrates silent input loss.
func TestSelectedMIMInvalidCreateIsAtomic(t *testing.T) {
	srv, conn, _ := scheduledServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := rtiv1.NewFederationServiceClient(conn)
	for _, tc := range []struct{ name, xml string }{
		{"empty", ""}, {"malformed", "<objectModel>"},
		{"missing-services", "<objectModel/>"},
		{"inconsistent-service", strings.Replace(string(mim.StandardMIMBytes()), "<name>HLAfederateHandle</name>", "<name>missingHandle</name>", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "invalid-mim-" + tc.name
			request := &rtiv1.CreateFederationRequest{WireVersion: 2, FederationName: name}
			module, err := proto.Marshal(&rtiv1.FOMModule{Path: "chosen.xml", Xml: []byte(tc.xml)})
			if err != nil {
				t.Fatal(err)
			}
			wire := protowire.AppendBytes(protowire.AppendTag(nil, 8, protowire.BytesType), module)
			if err := (proto.UnmarshalOptions{Merge: true}).Unmarshal(wire, request); err != nil {
				t.Fatal(err)
			}
			if _, err := client.CreateFederation(ctx, request); err == nil {
				t.Error("explicit invalid MIM was silently accepted")
			}
			if _, exists := srv.fedMgr.GenerationFor(core.FederationName(name)); exists {
				t.Error("invalid MIM published a federation")
			}
			if _, err := srv.foms.Get(ctx, core.FederationName(name)); err == nil {
				t.Error("invalid MIM published a repository entry")
			}
		})
	}
}

func TestSelectedMIMJoinExpectationRejectsEmbeddedRole(t *testing.T) {
	srv, conn, _ := scheduledServer(t)
	defer srv.sessions.Close()
	ctx := context.Background()
	before, err := srv.foms.FOMView(ctx, "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	client := rtiv1.NewFederationServiceClient(conn)
	_, err = client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 2, FederationName: "scheduled", FederateName: "must-not-join", ExpectedMimModule: &rtiv1.FOMModule{Path: "explicit.xml", Xml: mim.StandardMIMBytes()}, AdditionalFomModules: []*rtiv1.FOMModule{{Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Rejected</name></objectClass></objectClass></objects></objectModel>`)}}})
	if err == nil {
		t.Fatal("implicit MIM satisfied explicit expectation")
	}
	after, err := srv.foms.FOMView(ctx, "scheduled")
	if err != nil || !reflect.DeepEqual(before, after) || len(srv.fedMgr.MembersOf("scheduled")) != 2 {
		t.Fatalf("mismatched role changed state: %v", err)
	}
}

func TestSelectedMIMLiveGoSDKCreateJoinSaveRestoreAndMOM(t *testing.T) {
	srv, wire, _ := scheduledServer(t)
	defer srv.sessions.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.grpcS.Serve(listener) }()
	t.Cleanup(func() { _ = listener.Close(); <-done })
	sdk, err := federate.Connect(ctx, listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer sdk.Close()
	const name = "selected-mim-live"
	defer func() {
		if err := srv.momMgr.FederationDestroyed(context.Background(), name); err != nil {
			t.Error(err)
		}
	}()
	source := []byte(strings.Replace(string(mim.StandardMIMBytes()), "This object class is the root class of all MOM object classes", "live selected MIM marker", 1))
	selected := &federate.FOMModule{Path: "client-only:/selected MIM.xml", XML: source}
	fomSource, err := os.ReadFile("../../../pkg/fom/exporter/testdata/retained.xml")
	if err != nil {
		t.Fatal(err)
	}
	spec := federate.FederationSpec{Name: name, MIMModule: selected, FOMModules: []federate.FOMModule{{Path: "retained.xml", XML: fomSource}}}
	if _, err := sdk.CreateFederation(ctx, spec); err != nil {
		t.Fatal(err)
	}
	// Joining by name uses the SDK's successfully created explicit selection.
	member, err := sdk.JoinFederation(ctx, federate.FederationSpec{Name: name}, "member")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := member.Resign(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	if module, err := member.SelectedMIM(ctx); err != nil || module == nil || module.Path != selected.Path || !bytes.Equal(module.XML, source) {
		t.Fatalf("SDK selected source: %+v %v", module, err)
	}
	client := rtiv1.NewFederationServiceClient(wire)
	moms := rtiv1.NewMomServiceClient(wire)
	assertMOM := func(fed, designator, marker string) string {
		t.Helper()
		result, err := moms.QueryFederationAttributes(ctx, &rtiv1.QueryFederationAttributesRequest{WireVersion: 1, FederationName: fed})
		if err != nil {
			t.Fatal(err)
		}
		if got := catalogRPCString(t, result.AttributeValues["HLAMIMdesignator"]); got != designator {
			t.Fatalf("MOM designator %q, want %q", got, designator)
		}
		if marker == "" {
			return ""
		}
		fdd := catalogRPCString(t, result.AttributeValues["HLAcurrentFDD"])
		if !strings.Contains(fdd, marker) {
			t.Fatal("MOM current FDD replaced selected MIM")
		}
		return fdd
	}
	assertMOM("scheduled", "HLAstandardMIM", "")
	before := assertMOM(name, selected.Path, "live selected MIM marker")
	viewBefore, err := srv.foms.FOMView(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		xml        []byte
	}{
		{"path", "different.xml", source},
		{"source", selected.Path, bytes.ReplaceAll(source, []byte("live selected MIM marker"), []byte("different selected MIM marker"))},
	} {
		badSpec := federate.FederationSpec{Name: name, MIMModule: &federate.FOMModule{Path: tc.path, XML: tc.xml}, AdditionalFOMModules: []federate.FOMModule{{Path: "must-not-commit.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Rejected</name></objectClass></objectClass></objects></objectModel>`)}}}
		if joined, err := sdk.JoinFederation(ctx, badSpec, "mismatch-"+tc.name); err == nil {
			_ = joined.Resign(ctx)
			t.Fatal("mismatched selected MIM joined")
		}
		viewAfter, err := srv.foms.FOMView(ctx, name)
		if err != nil || !reflect.DeepEqual(viewBefore, viewAfter) {
			t.Fatalf("rejected join changed revision/handles/source: %v", err)
		}
		if len(srv.fedMgr.MembersOf(name)) != 1 {
			t.Fatal("rejected MIM expectation changed roster")
		}
		if actual, err := srv.foms.CurrentFDD(ctx, name); err != nil || string(actual) != before {
			t.Fatalf("rejected MIM expectation changed FDD: %v", err)
		}
	}
	saves := rtiv1.NewSavepointServiceClient(wire)
	h := member.Handle()
	if _, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: name, FederateHandle: h, Label: "selected"}); err != nil {
		t.Fatal(err)
	}
	response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: name, FederateHandle: h}
	if _, err := saves.FederateSaveBegun(ctx, response); err != nil {
		t.Fatal(err)
	}
	if _, err := saves.FederateSaveComplete(ctx, response); err != nil {
		t.Fatal(err)
	}
	extra, err := client.JoinFederation(ctx, &rtiv1.JoinFederationRequest{WireVersion: 1, FederationName: name, FederateName: "extender", AdditionalFomModules: []*rtiv1.FOMModule{{Path: "later.xml", Xml: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Later</name></objectClass></objectClass></objects></objectModel>`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if extra.FederateHandle != h+1 {
		t.Fatal("rejected MIM expectation consumed handles")
	}
	if extra.FomView.MimModule == nil || !bytes.Equal(extra.FomView.MimModule.Xml, source) || len(extra.FomView.Modules) != 2 {
		t.Fatal("join lost selected role")
	}
	if _, err := client.ResignFederation(ctx, &rtiv1.ResignFederationRequest{WireVersion: 1, FederationName: name, FederateHandle: extra.FederateHandle, Action: rtiv1.ResignAction_RESIGN_ACTION_NO_ACTION}); err != nil {
		t.Fatal(err)
	}
	joined := assertMOM(name, selected.Path, "<name>Later</name>")
	if _, err := saves.RequestFederationRestore(ctx, &rtiv1.RequestFederationRestoreRequest{WireVersion: 1, FederationName: name, FederateHandle: h, Label: "selected"}); err != nil {
		t.Fatal(err)
	}
	if pending, err := srv.foms.CurrentFDD(ctx, name); err != nil || string(pending) != joined {
		t.Fatalf("prepared restore leaked: %v", err)
	}
	if _, err := saves.FederateRestoreComplete(ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1, FederationName: name, FederateHandle: h}); err != nil {
		t.Fatal(err)
	}
	if restored := assertMOM(name, selected.Path, "live selected MIM marker"); restored != before {
		t.Fatal("restore did not recover exact saved current FDD")
	}
	view, err := srv.foms.FOMView(ctx, name)
	if err != nil || view.MIMModule == nil || view.MIMModule.Path != selected.Path || !bytes.Equal(view.MIMModule.XML, source) || len(view.Modules) != 1 {
		t.Fatalf("restore selected source: %v", err)
	}
}
