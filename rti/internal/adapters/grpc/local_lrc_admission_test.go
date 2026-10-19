package grpc

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type participantLRCAdmission struct {
	legacyCalls  int
	participants []core.FederateHandle
	releases     int
}

func (a *participantLRCAdmission) AcquireService(core.FederationName) (func(), error) {
	a.legacyCalls++
	return nil, core.ErrSaveAlreadyInProgress
}
func (*participantLRCAdmission) AcquireCheckpoint(core.FederationName) func() { return nil }
func (a *participantLRCAdmission) AcquireFederateService(_ core.FederationName, h core.FederateHandle) (func(), error) {
	a.participants = append(a.participants, h)
	return func() { a.releases++ }, nil
}

func TestLocalLRCUsesParticipantAdmissionWithoutWeakeningMembership(t *testing.T) {
	ctx := context.Background()
	const fed core.FederationName = "participant-admission"
	members, h, generation := newLeaseTestManager(t, fed)
	admission := &participantLRCAdmission{}
	s := newLocalLRCService(&Server{objService: newObjectService(&stubObjectRegistry{}), membership: members, checkpointAdmission: admission})
	if err := s.validateOpen(ctx, fed, h, generation); err != nil {
		t.Fatal(err)
	}
	if err := s.validateOpen(ctx, fed, h, generation+1); err == nil {
		t.Fatal("participant admission bypassed generation validation")
	}
	f := newLocalLRCFrameFencer(members, fed, h)
	f.admission = admission
	lease, err := f.acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lease.end()
	if err := members.ResignFederation(ctx, fed, h, core.ResignActionNoAction); err != nil {
		t.Fatal(err)
	}
	if _, err := f.acquire(ctx); err == nil {
		t.Fatal("participant admission bypassed membership validation")
	}
	if admission.legacyCalls != 0 || len(admission.participants) != 4 || admission.releases != 4 {
		t.Fatalf("admission calls/release: %+v", admission)
	}
	for _, got := range admission.participants {
		if got != h {
			t.Fatal("wrong admission participant")
		}
	}
}
