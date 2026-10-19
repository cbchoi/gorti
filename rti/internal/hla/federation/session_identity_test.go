package federation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

func TestSessionIdentityDoesNotResignSameNameRejoin(t *testing.T) {
	ctx := context.Background()
	m, err := federation.New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CreateFederation(ctx, core.CreateFederationRequest{Name: "identity"}); err != nil {
		t.Fatal(err)
	}
	var old core.MemberIdentity
	h, err := m.JoinFederationWithSetup(ctx, core.JoinFederationRequest{Federation: "identity", FederateName: "worker"}, func(id core.MemberIdentity) { old = id })
	if err != nil {
		t.Fatal(err)
	}
	if old.Handle != h || old.Incarnation == 0 || old.Name != "worker" {
		t.Fatalf("identity: %+v", old)
	}
	if err := m.ResignFederation(ctx, "identity", h, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	next, err := m.JoinFederation(ctx, core.JoinFederationRequest{Federation: "identity", FederateName: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ResignFederationIdentity(ctx, old, core.ResignActionNoAction); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("stale identity: %v", err)
	}
	if err := m.ValidateMember("identity", next); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticResignDirectiveValidatedAndResetOnRejoin(t *testing.T) {
	ctx := context.Background()
	m, _ := federation.New(validOptions())
	if err := m.CreateFederation(ctx, core.CreateFederationRequest{Name: "auto"}); err != nil {
		t.Fatal(err)
	}
	h, err := m.JoinFederation(ctx, core.JoinFederationRequest{Federation: "auto", FederateName: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetAutomaticResignDirective("auto", h, core.ResignActionCancelThenDelete); err != nil {
		t.Fatal(err)
	}
	if err := m.SetAutomaticResignDirective("auto", h, core.ResignAction(255)); !errors.Is(err, core.ErrResignActionUnsupported) {
		t.Fatalf("bad action: %v", err)
	}
	action, err := m.GetAutomaticResignDirective("auto", h)
	if err != nil || action != core.ResignActionCancelThenDelete {
		t.Fatalf("action %v: %v", action, err)
	}
	if err := m.ResignFederation(ctx, "auto", h, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	h, err = m.JoinFederation(ctx, core.JoinFederationRequest{Federation: "auto", FederateName: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	action, err = m.GetAutomaticResignDirective("auto", h)
	if err != nil || action != core.ResignActionNoAction {
		t.Fatalf("fresh action %v: %v", action, err)
	}
}
