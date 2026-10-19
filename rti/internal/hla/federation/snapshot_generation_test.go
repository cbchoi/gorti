package federation_test

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

func TestSnapshotCapturesMemberGenerationAcrossRecreation(t *testing.T) {
	opts := validOptions()
	opts.GenerationEpoch = 9
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var previous core.FederationRoster
	for generation := uint64(9); generation < 12; generation++ {
		if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "fed"}); err != nil {
			t.Fatal(err)
		}
		handle, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateName: "member"})
		if err != nil {
			t.Fatal(err)
		}
		rosters := mgr.Snapshot()
		if len(rosters) != 1 || rosters[0].Generation != generation {
			t.Fatalf("generation %d snapshot = %+v", generation, rosters)
		}
		if len(rosters[0].Federates) != 1 || rosters[0].Federates[0].Handle != handle || uint64(handle)>>32 != generation {
			t.Fatalf("snapshot members do not match execution: %+v", rosters[0])
		}
		if previous.Name != "" && previous.Generation != generation-1 {
			t.Fatalf("previous snapshot generation changed: %+v", previous)
		}
		previous = rosters[0]
		if err := mgr.ResignFederation(ctx, "fed", handle, core.ResignActionNoAction); err != nil {
			t.Fatal(err)
		}
		if err := mgr.DestroyFederation(ctx, "fed"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSnapshotGenerationMatchesMembersDuringLifecycleChurn(t *testing.T) {
	opts := validOptions()
	opts.GenerationEpoch = 9
	mgr, err := federation.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			if err := mgr.CreateFederation(ctx, core.CreateFederationRequest{Name: "fed"}); err != nil {
				done <- err
				return
			}
			handle, err := mgr.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateName: "member"})
			if err != nil {
				done <- err
				return
			}
			if err := mgr.ResignFederation(ctx, "fed", handle, core.ResignActionNoAction); err != nil {
				done <- err
				return
			}
			if err := mgr.DestroyFederation(ctx, "fed"); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	var mismatch *core.FederationRoster
	for {
		for _, roster := range mgr.Snapshot() {
			for _, member := range roster.Federates {
				if uint64(member.Handle)>>32 != roster.Generation {
					snapshot := roster
					mismatch = &snapshot
				}
			}
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			if mismatch != nil {
				t.Fatalf("mixed generation/member snapshot: %+v", *mismatch)
			}
			return
		default:
		}
	}
}
