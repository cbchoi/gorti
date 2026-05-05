package federation

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// snapFakeFOMs is a permissive FOM repo for snapshot tests.
type snapFakeFOMs struct{}

func (snapFakeFOMs) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return snapFakeFOM{}, nil
}
func (snapFakeFOMs) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return snapFakeFOM{}, nil
}

type snapFakeFOM struct{}

func (snapFakeFOM) IsValid() bool { return true }
func (snapFakeFOM) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 1, true
}
func (snapFakeFOM) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (snapFakeFOM) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (snapFakeFOM) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

func TestManager_Snapshot_ListsFederationsAndFederates(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{
		Clock: core.NewFakeClock(time.Unix(0, 0)),
		FOMs:  snapFakeFOMs{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "demo", Mode: core.ModeVerbose}); err != nil {
		t.Fatalf("CreateFederation: %v", err)
	}
	if _, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "demo", FederateName: "alpha"}); err != nil {
		t.Fatalf("JoinFederation alpha: %v", err)
	}
	if _, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "demo", FederateName: "beta"}); err != nil {
		t.Fatalf("JoinFederation beta: %v", err)
	}

	rosters := mgr.Snapshot()
	if got := len(rosters); got != 1 {
		t.Fatalf("Snapshot len = %d, want 1", got)
	}
	r := rosters[0]
	if r.Name != "demo" {
		t.Errorf("Name = %q, want demo", r.Name)
	}
	if r.Mode != core.ModeVerbose {
		t.Errorf("Mode = %v, want Verbose", r.Mode)
	}
	want := []core.FederateInfo{
		{Handle: 1, Name: "alpha"},
		{Handle: 2, Name: "beta"},
	}
	if !reflect.DeepEqual(r.Federates, want) {
		t.Errorf("Federates = %v, want %v", r.Federates, want)
	}
}

func TestManager_Snapshot_NoFederations_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{
		Clock: core.NewFakeClock(time.Unix(0, 0)),
		FOMs:  snapFakeFOMs{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := mgr.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot empty manager = %v, want empty", got)
	}
}
