package federation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

func TestCreateCheckedSetupFailureConsumesGenerationAndLeavesNoExecution(t *testing.T) {
	mgr, err := federation.New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	creator, ok := any(mgr).(interface {
		CreateFederationWithCheckedSetup(context.Context, core.CreateFederationRequest, func(uint64) error) error
	})
	if !ok {
		t.Fatal("fallible setup is not supported")
	}
	want := errors.New("history persistence failed")
	var failedGeneration uint64
	err = creator.CreateFederationWithCheckedSetup(context.Background(), core.CreateFederationRequest{Name: "f"}, func(g uint64) error {
		failedGeneration = g
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("create=%v", err)
	}
	if _, exists := mgr.GenerationFor("f"); exists {
		t.Fatal("failed execution remains visible")
	}
	if _, err := mgr.JoinFederation(context.Background(), core.JoinFederationRequest{Federation: "f", FederateName: "a"}); !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatalf("join=%v", err)
	}
	if err := mgr.CreateFederation(context.Background(), core.CreateFederationRequest{Name: "f"}); err != nil {
		t.Fatal(err)
	}
	if next, _ := mgr.GenerationFor("f"); next <= failedGeneration {
		t.Fatalf("generation reused: %d -> %d", failedGeneration, next)
	}
}
