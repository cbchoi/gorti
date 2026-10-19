package object

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestRTIObjectDiscoveryRecordsOnlyExistingRTIObjects(t *testing.T) {
	ctx := context.Background()
	r, declarations, _ := newTestRegistry(t, nil)
	const fed core.FederationName = "rti-discovery"
	for _, owner := range []core.FederateHandle{internalProducer, 2} {
		if err := declarations.PublishObjectClassAttributes(ctx, fed, owner, 1, []core.AttributeHandle{1}); err != nil {
			t.Fatal(err)
		}
	}
	mom, _, err := r.Register(ctx, fed, internalProducer, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := r.Register(ctx, fed, 2, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	before := r.ExportState(fed).Discoveries
	for _, tc := range []struct {
		federation core.FederationName
		member     core.FederateHandle
		object     core.ObjectHandle
		want       error
	}{
		{"missing", 3, mom, core.ErrObjectNotFound},
		{fed, 3, 999, core.ErrObjectNotFound},
		{fed, 3, user, core.ErrObjectNotFound},
		{fed, 0, mom, core.ErrFederateNotJoined},
		{fed, internalProducer, mom, core.ErrFederateNotJoined},
	} {
		if err := r.RecordRTIObjectDiscovery(tc.federation, tc.member, tc.object); !errors.Is(err, tc.want) {
			t.Fatalf("record(%q,%d,%d) = %v, want %v", tc.federation, tc.member, tc.object, err, tc.want)
		}
	}
	if got := r.ExportState(fed).Discoveries; !slices.Equal(got, before) {
		t.Fatalf("rejected discovery changed knowledge: %+v", got)
	}
	for range 2 {
		if err := r.RecordRTIObjectDiscovery(fed, 3, mom); err != nil {
			t.Fatal(err)
		}
	}
	state := r.ExportState(fed)
	if len(state.Discoveries) != len(before)+1 || !slices.Contains(state.Discoveries, DiscoveryState{Object: mom, Federate: 3}) {
		t.Fatalf("non-idempotent discovery state: %+v", state.Discoveries)
	}
	if _, err := r.PrepareImportState(fed, state); err != nil {
		t.Fatalf("recorded state is not restorable: %v", err)
	}
}
