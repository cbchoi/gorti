package server

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	"github.com/cbchoi/gorti/rti/pkg/encoding"
)

func TestProductionTimeSelectionMatchesLifecycleMOMAndCheckpoint(t *testing.T) {
	for _, selection := range []string{"", float64TimeName} {
		t.Run("selected-"+selection, func(t *testing.T) {
			const fed core.FederationName = "time-selection"
			ctx, srv, conn, _ := momCompletionReviewServer(t, fed)
			client := rtiv1.NewFederationServiceClient(conn)
			created, err := client.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: string(fed), LogicalTimeImplementationName: selection, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
			if err != nil {
				t.Fatal(err)
			}
			if created.LogicalTimeImplementationName != float64TimeName {
				t.Fatalf("create time=%q", created.LogicalTimeImplementationName)
			}
			listed, err := client.ListFederations(ctx, &rtiv1.ListFederationsRequest{WireVersion: 1})
			if err != nil || len(listed.Federations) != 1 || listed.Federations[0].LogicalTimeImplementationName != created.LogicalTimeImplementationName {
				t.Fatalf("list=%v,%v", listed, err)
			}
			joined := momCompletionReviewJoin(ctx, t, conn, string(fed), "participant", "", created.FederationGeneration)
			if joined.LogicalTimeImplementationName != created.LogicalTimeImplementationName {
				t.Fatalf("join time=%q", joined.LogicalTimeImplementationName)
			}
			query := momCompletionReviewQuery(ctx, t, conn, string(fed))
			encoded := query.AttributeValues["HLAtimeImplementationName"]
			decoded, count, err := (encoding.HLAunicodeString{}).Decode(encoded)
			if err != nil || count != len(encoded) || decoded != created.LogicalTimeImplementationName {
				t.Fatalf("MOM time=%v, count=%d, %v", decoded, count, err)
			}
			momCompletionReviewSave(ctx, t, conn, string(fed), "selected", joined.FederateHandle)
			manifest, err := srv.saveMgr.(*savepoint.Manager).LoadManifest(fed, "selected")
			if err != nil || manifest.LogicalTimeImplementationName != created.LogicalTimeImplementationName {
				t.Fatalf("checkpoint time=%q, %v", manifest.LogicalTimeImplementationName, err)
			}
		})
	}
}
