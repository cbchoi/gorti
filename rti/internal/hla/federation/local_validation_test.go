package federation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

func TestLocalValidationSnapshotLeaseExcludesOperations(t *testing.T) {
	m, err := federation.New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.CreateFederation(ctx, core.CreateFederationRequest{Name: "fed"}); err != nil {
		t.Fatal(err)
	}
	h, err := m.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateName: "member"})
	if err != nil {
		t.Fatal(err)
	}
	port, ok := any(m).(core.MemberSnapshotLeaser)
	if !ok {
		t.Fatal("federation manager lacks an exclusive local validation snapshot lease")
	}
	g, _ := m.GenerationFor("fed")
	if _, err := port.AcquireMemberSnapshot(ctx, "fed", h, g+1); !errors.Is(err, core.ErrFederationGenerationMismatch) {
		t.Fatalf("stale generation: %v", err)
	}
	if _, err := port.AcquireMemberSnapshot(ctx, "fed", 0, g); !errors.Is(err, core.ErrFederateNotJoined) {
		t.Fatalf("invalid member: %v", err)
	}
	release, err := port.AcquireMemberSnapshot(ctx, "fed", h, g)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		end, err := m.AcquireMember("fed", h)
		if end != nil {
			end()
		}
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("ordinary operation crossed snapshot: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	release()
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot release did not unblock operation")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := port.AcquireMemberSnapshot(canceled, "fed", h, g); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled snapshot: %v", err)
	}
}
