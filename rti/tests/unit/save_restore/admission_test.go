package save_restore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestCheckpointAdmissionDrainsAndRejectsActiveProtocol(t *testing.T) {
	m, _ := manager(t, newStore(t), nil)
	service, err := m.AcquireService("fed")
	if err != nil {
		t.Fatal(err)
	}
	entered, done := make(chan struct{}), make(chan error, 1)
	go func() {
		release := m.AcquireCheckpoint("fed")
		defer release()
		close(entered)
		done <- m.RequestFederationSave(context.Background(), "fed", "checkpoint", nil)
	}()
	select {
	case <-entered:
		service()
		t.Fatal("checkpoint did not wait for admitted service")
	case <-time.After(20 * time.Millisecond):
	}
	service()
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
	if release, err := m.AcquireService("fed"); !errors.Is(err, core.ErrSaveAlreadyInProgress) {
		if release != nil {
			release()
		}
		t.Fatalf("active save admission=%v", err)
	}
	other, err := m.AcquireService("other")
	if err != nil {
		t.Fatal(err)
	}
	other()
	control := m.AcquireCheckpoint("fed")
	if err := m.AbortSave(context.Background(), "fed"); err != nil {
		control()
		t.Fatal(err)
	}
	control()
	release, err := m.AcquireService("fed")
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestCheckpointAdmissionRestoreRemainsClosedUntilLastReport(t *testing.T) {
	members := []core.FederateHandle{1, 2}
	m, _ := strictManager(t, &preparedState{value: liveStateValue}, map[string][]byte{"state": []byte(savedStateValue)}, &members)
	ctx := context.Background()
	if err := m.RequestFederationRestore(ctx, "fed", savedStateValue); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateRestoreComplete(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	if release, err := m.AcquireService("fed"); !errors.Is(err, core.ErrRestoreAlreadyInProgress) {
		if release != nil {
			release()
		}
		t.Fatalf("pending restore admission=%v", err)
	}
	if err := m.FederateRestoreComplete(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	release, err := m.AcquireService("fed")
	if err != nil {
		t.Fatal(err)
	}
	release()
}

const savedStateValue = "saved"

const liveStateValue = "live"
