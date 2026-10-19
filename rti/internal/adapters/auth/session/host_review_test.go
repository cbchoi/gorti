package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestHostReviewOldSessionCannotChangeReplacement(t *testing.T) {
	m, mm, oldCtx, token := setup(t)
	m.RegisterWithHost(mm.id, token, "original.example")
	oldID := mm.id
	closeOld, err := m.Open(oldCtx, "f", oldID.Handle)
	if err != nil {
		t.Fatal(err)
	}
	mm.gate.Lock()
	mm.mu.Lock()
	mm.id.Handle++
	mm.id.Incarnation++
	next := mm.id
	mm.mu.Unlock()
	nextToken, err := session.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	m.RegisterWithHost(next, nextToken, "replacement.example")
	mm.gate.Unlock()
	closeOld()
	m.ClearIdentity(oldID)
	release, err := mm.AcquireMember("f", next.Handle)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := m.Authenticate(oldCtx, "f", next.Handle); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("old token accepted: %v", err)
	}
	newCtx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(session.MetadataKey, string(nextToken)))
	if err := m.Authenticate(newCtx, "f", next.Handle); err != nil {
		t.Fatal(err)
	}
	if host, known, err := m.Host("f", next.Handle); err != nil || !known || host != "replacement.example" {
		t.Fatalf("replacement corrupted: %q %v %v", host, known, err)
	}
}

func TestHostReviewLeaseFencesRejoinAndConcurrentQueries(t *testing.T) {
	m, mm, _, token := setup(t)
	m.RegisterWithHost(mm.id, token, "original.example")
	release, err := mm.AcquireMember("f", mm.id.Handle)
	if err != nil {
		t.Fatal(err)
	}
	started, replaced := make(chan struct{}), make(chan struct{})
	go func() {
		close(started)
		mm.gate.Lock()
		defer mm.gate.Unlock()
		mm.mu.Lock()
		mm.id.Incarnation++
		id := mm.id
		mm.mu.Unlock()
		m.RegisterWithHost(id, token, "")
		close(replaced)
	}()
	<-started
	if host, known, err := m.Host("f", 1); err != nil || !known || host != "original.example" {
		t.Fatalf("leased source changed: %q %v %v", host, known, err)
	}
	select {
	case <-replaced:
		t.Fatal("rejoin passed live membership lease")
	default:
	}
	release()
	select {
	case <-replaced:
	case <-time.After(time.Second):
		t.Fatal("host lookup left a lock held")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				release, err := mm.AcquireMember("f", 1)
				if err != nil {
					t.Error(err)
					return
				}
				host, known, err := m.Host("f", 1)
				release()
				if err != nil || known || host != "" {
					t.Errorf("unreported host inherited: %q %v %v", host, known, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestHostReviewIdentityNotHandleAcrossRestoreAndGeneration(t *testing.T) {
	m, mm, _, token := setup(t)
	m.RegisterWithHost(mm.id, token, "live.example")
	oldID := mm.id
	mm.gate.Lock()
	mm.mu.Lock()
	mm.id.Handle = 8
	mm.mu.Unlock()
	mm.gate.Unlock()
	if _, _, err := m.Host("f", oldID.Handle); err == nil {
		t.Fatal("old pre-restore handle still readable")
	}
	release, err := mm.AcquireMember("f", 8)
	if err != nil {
		t.Fatal(err)
	}
	if host, known, err := m.Host("f", 8); err != nil || !known || host != "live.example" {
		t.Fatalf("live session lost across restore: %q %v %v", host, known, err)
	}
	release()
	mm.gate.Lock()
	mm.mu.Lock()
	mm.id.Generation++
	mm.mu.Unlock()
	mm.gate.Unlock()
	release, err = mm.AcquireMember("f", 8)
	if err != nil {
		t.Fatal(err)
	}
	if host, known, err := m.Host("f", 8); err != nil || known || host != "" {
		t.Fatalf("old execution leaked host: %q %v %v", host, known, err)
	}
	release()
	m.ClearFederation(core.FederationName("f"))
}
