package server

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestMOMServiceResignActualLifecycleReportsBeforeMOMRetirement(t *testing.T) {
	f := newMOMExecutionFixture(t, momServiceResignFederationExecution)
	ctx := context.Background()
	const fed core.FederationName = "catalog-provider"
	var hookError error
	var retained bool
	manager, err := federation.New(federation.Options{Clock: core.NewManualClock(time.Unix(0, 0)), FOMs: f.deps.FOMs,
		OnFederateResignOutcome: func(ctx context.Context, fed core.FederationName, target core.FederateHandle, _ core.ResignAction, outcome error) {
			_, retained = f.deps.Manager.QueryFederateAttributes(fed, target)
			hookError = f.dispatcher.ObserveMOMResignOutcome(ctx, fed, target, outcome, grpcadapter.MOMServiceExceptionName(outcome))
		},
		OnFederateResigned: func(ctx context.Context, fed core.FederationName, target core.FederateHandle) {
			if err := f.deps.Manager.FederateResigned(ctx, fed, target); err != nil {
				t.Error(err)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.CreateFederation(ctx, core.CreateFederationRequest{Name: fed}); err != nil {
		t.Fatal(err)
	}
	sender, err := manager.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "a"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := manager.JoinFederation(ctx, core.JoinFederationRequest{Federation: fed, FederateName: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if sender != f.sender || target != f.target {
		t.Fatal("fixture handles differ")
	}
	executor := f.executor.(*momServiceExecutor)
	executor.deps.Federations = manager
	executor.deps.Resign = manager.ResignFederationIdentity
	executor.deps.Admission = func(_ context.Context, fed core.FederationName, sender, target core.FederateHandle, _ string) (func(), error) {
		if _, err := manager.MemberIdentity(fed, sender); err != nil {
			return nil, err
		}
		if _, err := manager.MemberIdentity(fed, target); err != nil {
			return nil, err
		}
		return func() {}, nil
	}
	f.deps.Manager.SetServiceReporting(fed, target, true)
	f.dispatcher.SetCatalogSources(mom.CatalogSources{ReportRecipients: func(context.Context, core.FederationName, core.FederateHandle, string, string) ([]core.FederateHandle, error) {
		return []core.FederateHandle{sender}, nil
	}})
	var reports []mom.ResponseInteraction
	f.dispatcher.SetEmitter(func(_ context.Context, _ core.FederationName, _ core.FederateHandle, report mom.ResponseInteraction, _ core.FOMHandle, _ core.FOMHandleNameLookup) error {
		reports = append(reports, report)
		return nil
	})
	fom, err := f.deps.FOMs.Get(ctx, fed)
	if err != nil {
		t.Fatal(err)
	}
	class := mom.ServiceClassPrefix + "HLAresignFederationExecution"
	h, _ := fom.LookupInteractionClass(class)
	targetParameter, _ := fom.LookupParameter(h, "HLAfederate")
	actionParameter, _ := fom.LookupParameter(h, "HLAresignAction")
	if err := f.dispatcher.Dispatch(ctx, fed, class, sender, map[core.ParameterHandle][]byte{targetParameter: catalogRPCHandle(uint64(target)), actionParameter: catalogRPCInt(6)}, fom, fom.(core.FOMHandleNameLookup)); err != nil {
		t.Fatal(err)
	}
	if hookError != nil || !retained {
		t.Fatalf("hook err=%v MOM retained=%v", hookError, retained)
	}
	if _, joined := f.deps.Manager.QueryFederateAttributes(fed, target); joined {
		t.Fatal("target MOM was not retired")
	}
	if len(reports) != 1 || reports[0].ClassName != mom.ClassReportServiceInvocation || binary.BigEndian.Uint32(reports[0].Params["HLAsuppliedArguments"]) != 1 {
		t.Fatalf("resign report missing or duplicated: %+v", reports)
	}
}
