package m11spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	mompkg "github.com/cbchoi/gorti/rti/internal/mom"
)

func newTestMOMManager(t *testing.T) *mompkg.Manager {
	t.Helper()
	mgr, err := mompkg.New(mompkg.Options{
		Outbox: newFakeOutbox(),
	})
	if err != nil && !errors.Is(err, mompkg.ErrNotImplemented) {
		t.Logf("mom.New returned: %v (expected during pre-dispatch)", err)
	}
	return mgr
}

// TestSpec_M11_FederationCreated_RegistersHLAfederation: after the RTI
// notifies the MOM manager that a federation was created, the
// HLAfederation MOM instance exists with the federation's name + FOM
// module names populated.
//
// Implements: FR-MOM-1.
func TestSpec_M11_FederationCreated_RegistersHLAfederation(t *testing.T) {
	mgr := newTestMOMManager(t)
	if mgr == nil {
		t.Skip("mom.Manager not yet wired (M11 RED state)")
	}
	err := mgr.FederationCreated(context.Background(), "demo", []core.FOMModule{
		{Path: "test.xml", XML: []byte(`<?xml version="1.0"?><objectModel/>`)},
	})
	if errors.Is(err, mompkg.ErrNotImplemented) {
		t.Skip("FederationCreated not yet implemented")
	}
	if err != nil {
		t.Fatalf("FederationCreated: %v", err)
	}
	attrs, ok := mgr.QueryFederationAttributes("demo")
	if !ok {
		t.Fatalf("QueryFederationAttributes(demo) = !ok; want HLAfederation MOM instance")
	}
	if attrs.Name != "demo" {
		t.Errorf("HLAfederationName = %q, want %q", attrs.Name, "demo")
	}
	if len(attrs.FOMModuleNames) != 1 {
		t.Errorf("HLAFOMmoduleDesignatorList length = %d, want 1", len(attrs.FOMModuleNames))
	}
}

// TestSpec_M11_FederateJoined_RegistersHLAfederate: after a federate
// joins, the HLAfederate MOM instance exists with handle + name + type
// attributes populated, AND HLAfederation.HLAfederatesInFederation
// includes the new federate's handle.
//
// Implements: FR-MOM-1.
func TestSpec_M11_FederateJoined_RegistersHLAfederate(t *testing.T) {
	mgr := newTestMOMManager(t)
	if mgr == nil {
		t.Skip("mom.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.FederationCreated(ctx, "demo", nil); err != nil {
		if errors.Is(err, mompkg.ErrNotImplemented) {
			t.Skip()
		}
		t.Fatalf("FederationCreated: %v", err)
	}
	if err := mgr.FederateJoined(ctx, "demo", 1, "alice", "RoleA"); err != nil {
		if errors.Is(err, mompkg.ErrNotImplemented) {
			t.Skip()
		}
		t.Fatalf("FederateJoined: %v", err)
	}

	fedAttrs, ok := mgr.QueryFederateAttributes("demo", 1)
	if !ok {
		t.Fatalf("QueryFederateAttributes(demo, 1) = !ok")
	}
	if fedAttrs.Name != "alice" {
		t.Errorf("HLAfederateName = %q, want alice", fedAttrs.Name)
	}
	if fedAttrs.Type != "RoleA" {
		t.Errorf("HLAfederateType = %q, want RoleA", fedAttrs.Type)
	}

	federationAttrs, ok := mgr.QueryFederationAttributes("demo")
	if !ok {
		t.Fatalf("QueryFederationAttributes(demo) = !ok")
	}
	found := false
	for _, h := range federationAttrs.FederateHandles {
		if h == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("HLAfederation.HLAfederatesInFederation = %v, want to include handle 1",
			federationAttrs.FederateHandles)
	}
}

// TestSpec_M11_FederateResigned_RemovesHLAfederate: after a federate
// resigns, the HLAfederate MOM instance is removed and
// HLAfederation.HLAfederatesInFederation no longer includes the handle.
//
// Implements: FR-MOM-1.
func TestSpec_M11_FederateResigned_RemovesHLAfederate(t *testing.T) {
	mgr := newTestMOMManager(t)
	if mgr == nil {
		t.Skip("mom.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.FederationCreated(ctx, "demo", nil); err != nil {
		if errors.Is(err, mompkg.ErrNotImplemented) {
			t.Skip()
		}
		t.Fatalf("FederationCreated: %v", err)
	}
	if err := mgr.FederateJoined(ctx, "demo", 1, "alice", "RoleA"); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}
	if err := mgr.FederateResigned(ctx, "demo", 1); err != nil {
		if errors.Is(err, mompkg.ErrNotImplemented) {
			t.Skip()
		}
		t.Fatalf("FederateResigned: %v", err)
	}

	if _, ok := mgr.QueryFederateAttributes("demo", 1); ok {
		t.Errorf("HLAfederate(demo, 1) still queryable after resign")
	}
	federationAttrs, _ := mgr.QueryFederationAttributes("demo")
	for _, h := range federationAttrs.FederateHandles {
		if h == 1 {
			t.Errorf("HLAfederation.HLAfederatesInFederation still includes resigned handle 1")
		}
	}
}

// TestSpec_M11_TimeStateChanged_UpdatesAttributes: after EnableRegulation,
// HLAfederate.HLAtimeRegulating reads true and HLAlookahead carries the
// declared lookahead.
//
// Implements: FR-MOM-1, FR-MOM-2.
func TestSpec_M11_TimeStateChanged_UpdatesAttributes(t *testing.T) {
	mgr := newTestMOMManager(t)
	if mgr == nil {
		t.Skip("mom.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.FederationCreated(ctx, "demo", nil); err != nil {
		if errors.Is(err, mompkg.ErrNotImplemented) {
			t.Skip()
		}
		t.Fatalf("FederationCreated: %v", err)
	}
	if err := mgr.FederateJoined(ctx, "demo", 1, "alice", "RoleA"); err != nil {
		t.Fatalf("FederateJoined: %v", err)
	}
	if err := mgr.TimeStateChanged(ctx, "demo", 1, true, false, core.LogicalTime(1.5), core.LogicalTime(0)); err != nil {
		if errors.Is(err, mompkg.ErrNotImplemented) {
			t.Skip()
		}
		t.Fatalf("TimeStateChanged: %v", err)
	}
	attrs, _ := mgr.QueryFederateAttributes("demo", 1)
	if !attrs.TimeRegulating {
		t.Errorf("HLAtimeRegulating = false, want true")
	}
	if attrs.Lookahead != core.LogicalTime(1.5) {
		t.Errorf("HLAlookahead = %v, want 1.5", attrs.Lookahead)
	}
}

// TestSpec_M11_FederationDestroyed_RemovesHLAfederation: after a
// federation is destroyed, the HLAfederation MOM instance is gone.
//
// Implements: FR-MOM-1.
func TestSpec_M11_FederationDestroyed_RemovesHLAfederation(t *testing.T) {
	mgr := newTestMOMManager(t)
	if mgr == nil {
		t.Skip("mom.Manager not yet wired")
	}
	ctx := context.Background()
	if err := mgr.FederationCreated(ctx, "demo", nil); err != nil {
		if errors.Is(err, mompkg.ErrNotImplemented) {
			t.Skip()
		}
		t.Fatalf("FederationCreated: %v", err)
	}
	if err := mgr.FederationDestroyed(ctx, "demo"); err != nil {
		if errors.Is(err, mompkg.ErrNotImplemented) {
			t.Skip()
		}
		t.Fatalf("FederationDestroyed: %v", err)
	}
	if _, ok := mgr.QueryFederationAttributes("demo"); ok {
		t.Errorf("HLAfederation(demo) still queryable after destroy")
	}
}
