package ownership_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
)

type localDeleteOutbox struct{}

func (localDeleteOutbox) Send(context.Context, core.FederationName, core.FederateHandle, core.OutboundEvent) error {
	return nil
}

func TestLocalDeleteOwnershipAndPendingAcquisition(t *testing.T) {
	m, err := ownership.New(ownership.Options{Outbox: localDeleteOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	m.RegisterInitialOwnership("fed", 1, 7, []core.AttributeHandle{1})
	if _, err := m.PrepareLocalDelete("fed", 1, 7); !errors.Is(err, core.ErrFederateOwnsAttributes) {
		t.Fatalf("owner: %v", err)
	}
	if err := m.Acquire(context.Background(), "fed", 2, 7, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PrepareLocalDelete("fed", 2, 7); !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
		t.Fatalf("pending acquisition: %v", err)
	}
	release, err := m.PrepareLocalDelete("fed", 3, 7)
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
	owners, release := m.PrepareAttributeOwners("fed", 7, []core.AttributeHandle{1, 2})
	if owners[1] != 1 || owners[2] != 0 {
		t.Fatalf("owners: %v", owners)
	}
	owners[1] = 9
	release()
	release()
	owner, exists := m.QueryOwnership("fed", 7, 1)
	if !exists || owner != 1 {
		t.Fatal("snapshot aliases live ownership")
	}
}
