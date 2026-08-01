package federate

import (
	"context"
	"testing"
	"time"
)

func TestP0DestroyFederationAfterAllFederatesResign(t *testing.T) {
	rtid := newTestRtid(t)
	connection := rtid.connect(t)
	t.Cleanup(func() { _ = connection.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	spec := FederationSpec{
		Name:       "destroy-p0",
		FOMModules: []FOMModule{{Path: "destroy.xml", XML: []byte(interactionStreamFOM)}},
	}
	first, err := connection.JoinFederation(ctx, spec, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := connection.JoinFederation(ctx, spec, "second")
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.DestroyFederation(ctx, spec.Name); err == nil {
		t.Fatal("destroy with joined federates succeeded")
	}
	if err := first.Resign(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.Resign(ctx); err != nil {
		t.Fatal(err)
	}
	if err := connection.DestroyFederation(ctx, spec.Name); err != nil {
		t.Fatalf("DestroyFederation: %v", err)
	}
	if err := connection.DestroyFederation(ctx, spec.Name); err == nil {
		t.Fatal("duplicate destroy succeeded")
	}

	recreated, err := connection.JoinFederation(ctx, spec, "recreated")
	if err != nil {
		t.Fatalf("same-name recreation: %v", err)
	}
	if err := recreated.Resign(ctx); err != nil {
		t.Fatal(err)
	}
	if err := connection.DestroyFederation(ctx, spec.Name); err != nil {
		t.Fatal(err)
	}
}

func TestP0DestroyFederationRejectsClosedConnection(t *testing.T) {
	connection := &Connection{}
	if err := connection.DestroyFederation(context.Background(), "closed"); err == nil {
		t.Fatal("closed connection destroy succeeded")
	}
}
