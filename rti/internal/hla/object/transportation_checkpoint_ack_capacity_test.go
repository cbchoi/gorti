package object

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestTransportationCheckpointUnconsumedAcknowledgementsRemainBounded(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	foreign := scope
	foreign.Member.Federation = testForeign
	transportationOK(t, f.decl.PublishInteractionClass(ctx, testForeign, 1, 7))
	requests := make([]TransportationCheckpointRestore, 4096)
	for i := range requests {
		requests[i] = TransportationCheckpointRestore{Scope: scope, Binding: TransportationCheckpointBinding{
			Interaction: 7, Transportation: core.TransportTypeBestEffort, Activated: true,
		}}
	}
	p, err := f.r.PrepareTransportationCheckpoint(ctx, requests, f.r.ExportState("fed"), f.owners.ExportState("fed"), &transportationFOM{})
	transportationOK(t, err)
	defer p.Rollback()
	if _, err := f.r.PrepareInteractionTransportationChange(ctx, foreign, 7, core.TransportTypeReliable); !errors.Is(err, core.ErrTransportationPendingLimit) {
		t.Fatal("unconsumed acknowledgement leases escaped capacity accounting", err)
	}
	commit, err := f.r.PrepareImportState("fed", f.r.ExportState("fed"))
	transportationOK(t, err)
	commit()
	p.Commit()
	if _, err := f.r.PrepareInteractionTransportationChange(ctx, foreign, 7, core.TransportTypeReliable); !errors.Is(err, core.ErrTransportationPendingLimit) {
		t.Fatal("installed unconsumed acknowledgements escaped capacity accounting", err)
	}
	p.Rollback()
	_, err = f.r.PrepareInteractionTransportationChange(ctx, foreign, 7, core.TransportTypeReliable)
	transportationOK(t, err)
}
