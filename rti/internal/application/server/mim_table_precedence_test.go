package server

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/mom"
)

func TestMIMTablePrecedencePendingSaveMetrics(t *testing.T) {
	const fed core.FederationName = "table-save-name"
	ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
	generation := momCompletionReviewCreate(ctx, t, conn, string(fed), "TableSave")
	member := momCompletionReviewJoin(ctx, t, conn, string(fed), "member", "", generation)
	check := func(next, last string) {
		t.Helper()
		// Inspect the real server's metric provider: RPC queries are intentionally
		// unavailable during saving, and reflections to saving members are suppressed.
		state, err := srv.momMgr.(*mom.Manager).FederationMetricValues(ctx, fed)
		if err != nil {
			t.Fatal(err)
		}
		if got := catalogRPCString(t, state.Values["HLAAnnexSaveName"]); got != next {
			t.Fatalf("next-save label = %q, want %q", got, next)
		}
		if got := catalogRPCString(t, state.Values["HLAlastSaveName"]); got != last {
			t.Fatalf("last-save label = %q, want %q", got, last)
		}
		if _, duplicate := state.Values["HLAnextSaveName"]; duplicate {
			t.Fatal("MOM contains duplicate legacy metric")
		}
	}
	check("", "")
	saves := rtiv1.NewSavepointServiceClient(conn)
	if _, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: member.FederateHandle, Label: testPendingLabel}); err != nil {
		t.Fatal(err)
	}
	check(testPendingLabel, "")
	if _, err := saves.AbortFederationSave(ctx, &rtiv1.AbortFederationSaveRequest{WireVersion: 1, FederationName: string(fed), FederateHandle: &member.FederateHandle}); err != nil {
		t.Fatal(err)
	}
	check("", "")
	momCompletionReviewSave(ctx, t, conn, string(fed), "committed", member.FederateHandle)
	check("", "committed")
}
