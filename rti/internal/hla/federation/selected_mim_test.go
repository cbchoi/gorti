package federation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

type reservedMIMRepository struct {
	*stubFOMRepo
	selectedLoads int
}

func (r *reservedMIMRepository) LoadWithMIM(ctx context.Context, modules []core.FOMModule, _ *core.FOMModule) (core.FOMHandle, error) {
	r.selectedLoads++
	return r.Load(ctx, modules)
}

func TestIEEE2010CreateRejectsReservedMIMBeforeLoadingOrPublication(t *testing.T) {
	repository := &reservedMIMRepository{stubFOMRepo: newStubFOMRepo()}
	opts := validOptions()
	opts.FOMs = repository
	manager, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	setupCalls := 0
	err = manager.CreateFederationWithCheckedSetup(context.Background(), core.CreateFederationRequest{
		Name: "reserved", MIMModule: &core.FOMModule{Path: "HLAstandardMIM", XML: []byte("source")},
	}, func(uint64) error { setupCalls++; return nil })
	if err == nil || !strings.Contains(err.Error(), "HLAstandardMIM") {
		t.Fatalf("reserved MIM accepted or misdiagnosed: %v", err)
	}
	if repository.selectedLoads != 0 || setupCalls != 0 {
		t.Fatalf("rejection occurred after work: loads=%d setup=%d", repository.selectedLoads, setupCalls)
	}
	if _, exists := manager.GenerationFor("reserved"); exists {
		t.Fatal("rejected create published an execution")
	}
	if err := manager.CreateFederation(context.Background(), core.CreateFederationRequest{Name: "reserved"}); err != nil {
		t.Fatal(err)
	}
	if generation, _ := manager.GenerationFor("reserved"); generation != 0 {
		t.Fatalf("rejected input consumed a generation: %d", generation)
	}
}

func TestSelectedMIMRepositoryWithoutCapabilityRejectsBeforeCreate(t *testing.T) {
	manager, err := federation.New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := manager.CreateFederation(ctx, core.CreateFederationRequest{Name: "selected", MIMModule: &core.FOMModule{Path: "selected.xml", XML: []byte("source")}}); err == nil {
		t.Fatal("unsupported repository silently ignored selection")
	}
	if _, exists := manager.GenerationFor("selected"); exists {
		t.Fatal("unsupported repository created federation")
	}
	if err := manager.CreateFederation(ctx, core.CreateFederationRequest{Name: "selected"}); err != nil {
		t.Fatal(err)
	}
}
