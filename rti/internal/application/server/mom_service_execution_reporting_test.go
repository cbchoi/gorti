package server

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestMOMServiceRPCReportsActualArgumentsAndStandardException(t *testing.T) {
	for _, tc := range []struct {
		service   string
		params    map[string][]byte
		success   bool
		exception string
		arguments uint32
	}{
		{momServiceEnableTimeRegulation, map[string][]byte{"HLAlookahead": catalogRPCHandle(0x3ff0000000000000)}, true, "", 1},
		{momServiceDisableTimeRegulation, nil, false, "TimeRegulationIsNotEnabled", 0},
	} {
		t.Run(tc.service, func(t *testing.T) {
			r := newMOMExecutionRPC(t, momServiceEnableAsynchronousDelivery)
			r.f.deps.Manager.SetServiceReporting("catalog-provider", r.f.target, true)
			r.f.deps.Manager.SetExceptionReporting("catalog-provider", r.f.target, true)
			r.f.dispatcher.SetCatalogSources(mom.CatalogSources{ReportRecipients: func(context.Context, core.FederationName, core.FederateHandle, string, string) ([]core.FederateHandle, error) {
				return []core.FederateHandle{r.f.sender}, nil
			}})
			var reports []mom.ResponseInteraction
			r.f.dispatcher.SetEmitter(func(_ context.Context, _ core.FederationName, _ core.FederateHandle, report mom.ResponseInteraction, _ core.FOMHandle, _ core.FOMHandleNameLookup) error {
				reports = append(reports, report)
				return nil
			})
			req := r.request(t, tc.service, r.f.target, tc.params)
			r.publish(t, req)
			err := r.send(req, r.senderToken)
			if (err == nil) != tc.success {
				t.Fatal(err)
			}
			var invocation, exception *mom.ResponseInteraction
			for i := range reports {
				switch reports[i].ClassName {
				case mom.ClassReportServiceInvocation:
					invocation = &reports[i]
				case mom.ClassReportException:
					exception = &reports[i]
				}
			}
			if invocation == nil {
				t.Fatal("actual delegated service invocation report missing")
			}
			if got := catalogRPCString(t, invocation.Params["HLAservice"]); got != tc.service {
				t.Fatal(got)
			}
			if got := binary.BigEndian.Uint32(invocation.Params["HLAsuppliedArguments"]); got != tc.arguments {
				t.Fatalf("arguments=%d want=%d", got, tc.arguments)
			}
			if got := catalogRPCString(t, invocation.Params["HLAexception"]); got != tc.exception {
				t.Fatalf("exception=%q want=%q", got, tc.exception)
			}
			if !tc.success && (exception == nil || catalogRPCString(t, exception.Params["HLAexception"]) != tc.exception) {
				t.Fatal("standard exception report missing")
			}
		})
	}
}

func TestMOMServiceExecutorEarlyErrorsRetainExceptionIdentity(t *testing.T) {
	f := newMOMExecutionFixture(t, momServiceEnableTimeRegulation)
	f.command.Target = 999999
	inv, err := f.executor.ExecuteMOMService(context.Background(), "catalog-provider", f.sender, f.command)
	if !errors.Is(err, core.ErrFederateNotJoined) || inv.Success || inv.Exception != testNotExecutionMember {
		t.Fatalf("outcome=%+v err=%v", inv, err)
	}
}

func TestMOMServiceExecutorReportsSelectedCheckpointService(t *testing.T) {
	for _, service := range []string{momServiceFederateSaveComplete, momServiceFederateRestoreComplete} {
		t.Run(service, func(t *testing.T) {
			f := newMOMExecutionFixture(t, service)
			f.command.Success = false
			f.command.SuppliedArguments = []string{"HLAsuccessIndicator=0x00000000"}
			inv, err := f.executor.ExecuteMOMService(context.Background(), "catalog-provider", f.sender, f.command)
			want := "federateSaveNotComplete"
			if service == momServiceFederateRestoreComplete {
				want = "federateRestoreNotComplete"
			}
			if err != nil || inv.Service != want || len(inv.SuppliedArguments) != 0 || !inv.Success {
				t.Fatalf("actual selected service=%+v err=%v", inv, err)
			}
		})
	}
}
