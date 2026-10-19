package federate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"google.golang.org/grpc"
)

func TestIEEE2010SelectedMIMReservedDesignatorBeforeRPC(t *testing.T) {
	client := &selectedMIMClient{}
	connection := &Connection{cc: &grpc.ClientConn{}, fed: client}
	_, err := connection.CreateFederation(context.Background(), FederationSpec{
		Name: "reserved", MIMModule: &FOMModule{Path: "HLAstandardMIM", XML: mim.StandardMIMBytes()},
	})
	if err == nil || !strings.Contains(err.Error(), "HLAstandardMIM") {
		t.Fatalf("reserved designator accepted or misdiagnosed: %v", err)
	}
	if client.created != nil || connection.cachedMIM("reserved") != nil {
		t.Fatal("invalid selection reached RPC or SDK cache")
	}
	if _, err := prepareMIMModule(&FOMModule{Path: "HLAstandardMIM.xml", XML: mim.StandardMIMBytes()}); err != nil {
		t.Fatalf("filename with suffix is not the reserved designator: %v", err)
	}
}

func TestSelectedMIMExplicitFileInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user-selected.xml")
	source := mim.StandardMIMBytes()
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	module, err := LoadMIMModule(path)
	if err != nil || module.Path != path || !bytes.Equal(module.XML, source) {
		t.Fatalf("local MIM upload: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareMIMModule(module); err != nil {
		t.Fatal("uploaded source depended on remaining file")
	}
	if _, err := LoadMIMModule(path); err == nil {
		t.Fatal("missing local MIM accepted")
	}
	for _, module := range []*FOMModule{{Path: "empty"}, {XML: source}, {Path: " \t", XML: source}, {Path: "bad", XML: []byte("<objectModel>")}} {
		if _, err := prepareMIMModule(module); err == nil {
			t.Fatal("invalid explicit module accepted")
		}
	}
}

type selectedMIMClient struct {
	compensatingFederationClient
	createErr error
	created   *rtiv1.CreateFederationRequest
	joined    *rtiv1.JoinFederationRequest
}

func (c *selectedMIMClient) CreateFederation(_ context.Context, request *rtiv1.CreateFederationRequest, _ ...grpc.CallOption) (*rtiv1.CreateFederationResponse, error) {
	c.created = request
	return &rtiv1.CreateFederationResponse{FederationGeneration: 1}, c.createErr
}

func (c *selectedMIMClient) JoinFederation(_ context.Context, request *rtiv1.JoinFederationRequest, _ ...grpc.CallOption) (*rtiv1.JoinFederationResponse, error) {
	c.joined = request
	return &rtiv1.JoinFederationResponse{FederateHandle: 1, FomView: &rtiv1.FOMView{Revision: 1, MimModule: request.GetExpectedMimModule()}}, nil
}

func TestSelectedMIMGoSDKCacheAndJoinPrecondition(t *testing.T) {
	ctx := context.Background()
	client := &selectedMIMClient{}
	connection := &Connection{cc: &grpc.ClientConn{}, fed: client, stream: &callbackOpenFailureClient{batchErr: errors.New("expected callback initialization stop")}}
	source := mim.StandardMIMBytes()
	spec := FederationSpec{Name: "selected", MIMModule: &FOMModule{Path: "selected.xml", XML: append([]byte(nil), source...)}}
	if _, err := connection.CreateFederation(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if client.created.GetWireVersion() != 2 || client.created.GetMimModule() == nil || len(client.created.GetFomModules()) != 0 {
		t.Fatal("create lost separate role/V2")
	}
	spec.MIMModule.XML[0] = '!'
	if !bytes.Equal(connection.cachedMIM("selected").XML, source) {
		t.Fatal("caller mutated cached MIM")
	}
	client.createErr = errors.New("rejected create")
	if _, err := connection.CreateFederation(ctx, FederationSpec{Name: "selected", MIMModule: &FOMModule{Path: "other.xml", XML: source}}); err == nil {
		t.Fatal("failed create hidden")
	}
	if connection.cachedMIM("selected").Path != "selected.xml" {
		t.Fatal("failed create replaced known MIM")
	}
	client.createErr = nil
	if _, err := connection.JoinFederation(ctx, FederationSpec{Name: "selected"}, "member"); err == nil {
		t.Fatal("expected callback initialization failure")
	}
	if client.joined == nil || client.joined.GetWireVersion() != 2 || client.joined.GetExpectedMimModule().GetPath() != "selected.xml" || !bytes.Equal(client.joined.GetExpectedMimModule().GetXml(), source) {
		t.Fatal("join did not send cached precondition")
	}
	if client.resignCalls != 1 {
		t.Fatal("callback failure did not compensate joined member")
	}
	if _, err := connection.CreateFederation(ctx, FederationSpec{Name: "legacy"}); err != nil {
		t.Fatal(err)
	}
	if client.created.GetWireVersion() != 1 || client.created.GetMimModule() != nil {
		t.Fatal("legacy create profile changed")
	}
}
