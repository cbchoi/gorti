package object

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestTransportationCheckpointStrictRequiredJSON(t *testing.T) {
	valid := `{"interaction":7,"attributes":[],"actual_attributes":[],"transportation":1,"activated":true}`
	for _, bad := range []string{
		strings.Replace(valid, `,"activated":true`, "", 1),
		strings.Replace(valid, `"activated":true`, `"activated":null`, 1),
		strings.Replace(valid, `"attributes":[]`, `"attributes":null`, 1),
		strings.Replace(valid, `"actual_attributes":[]`, `"actual_attributes":null`, 1),
		strings.Replace(valid, `"activated":true`, `"activated":true,"activated":false`, 1),
		strings.Replace(valid, `"activated":true`, `"activated":true,"unknown":1`, 1),
	} {
		var b TransportationCheckpointBinding
		if err := json.Unmarshal([]byte(bad), &b); err == nil {
			t.Fatalf("accepted ambiguous binding %s", bad)
		}
	}
	var b TransportationCheckpointBinding
	transportationOK(t, json.Unmarshal([]byte(valid), &b))
	if !b.Activated {
		t.Fatal("activation phase lost")
	}
	raw, err := json.Marshal(TransportationCheckpointBinding{Interaction: 7, Transportation: core.TransportTypeReliable})
	transportationOK(t, err)
	if bytes.Contains(raw, []byte("null")) || !bytes.Contains(raw, []byte(`"attributes":[]`)) || !bytes.Contains(raw, []byte(`"actual_attributes":[]`)) {
		t.Fatal(string(raw))
	}
}

func TestTransportationCheckpointPrecommitLeaseSurvivesImport(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	change, err := f.r.PrepareAttributeTransportationChange(ctx, scope, f.obj, []core.AttributeHandle{1}, core.TransportTypeBestEffort)
	transportationOK(t, err)
	b := f.r.ExportTransportationCheckpoint("fed")[string(change.Ticket)].Binding
	state, owned := f.r.ExportState("fed"), f.owners.ExportState("fed")
	fresh := scope
	fresh.Member.Generation++
	fresh.Member.Incarnation++
	fresh.CallbackEpoch++
	p, err := f.r.PrepareTransportationCheckpoint(ctx, []TransportationCheckpointRestore{{Scope: fresh, Binding: b}}, state, owned, &transportationFOM{})
	transportationOK(t, err)
	defer p.Rollback()
	restored := p.Changes()[0]
	if _, err := f.r.ActivateTransportationChange(ctx, fresh, restored.Ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
		t.Fatal("unpublished lease activated", err)
	}
	commit, err := f.r.PrepareImportState("fed", state)
	transportationOK(t, err)
	commit()
	f.checkAttribute(t, 1, core.TransportTypeReliable)
	p.Commit()
	f.checkAttribute(t, 1, core.TransportTypeReliable)
	_, err = f.r.ActivateTransportationChange(ctx, fresh, restored.Ticket)
	transportationOK(t, err)
	f.checkAttribute(t, 1, core.TransportTypeBestEffort)
	if _, err := f.r.ActivateTransportationChange(ctx, scope, change.Ticket); !errors.Is(err, core.ErrTransportationTicketInvalid) {
		t.Fatal("source ticket survived", err)
	}
}

func TestTransportationCheckpointReplacedLeaseRollback(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	b := TransportationCheckpointBinding{Interaction: 7, Transportation: core.TransportTypeReliable}
	request := []TransportationCheckpointRestore{{Scope: scope, Binding: b}}
	prepare := func(request []TransportationCheckpointRestore) *TransportationCheckpointReservation {
		p, err := f.r.PrepareTransportationCheckpoint(ctx, request, f.r.ExportState("fed"), f.owners.ExportState("fed"), &transportationFOM{})
		transportationOK(t, err)
		return p
	}
	first := prepare(request)
	first.Commit()
	other := scope
	other.Member.Federation = testOther
	second := prepare([]TransportationCheckpointRestore{{Scope: other, Binding: b}})
	defer second.Release()
	first.Rollback()
	f.r.transports.mu.RLock()
	retained := transportationCheckpointLeasedTickets(f.r.transports)[string(second.Changes()[0].Ticket)] != nil
	f.r.transports.mu.RUnlock()
	if !retained {
		t.Fatal("old rollback erased newer capacity lease")
	}
}

func TestTransportationCheckpointActivatedAcknowledgementCapacityBounded(t *testing.T) {
	f := newTransportationFixture(t)
	ctx, scope := context.Background(), transportationScope()
	requests := make([]TransportationCheckpointRestore, 4096)
	for i := range requests {
		requests[i] = TransportationCheckpointRestore{Scope: scope, Binding: TransportationCheckpointBinding{Interaction: 7, Transportation: core.TransportTypeBestEffort, Activated: true}}
	}
	state, owned := f.r.ExportState("fed"), f.owners.ExportState("fed")
	p, err := f.r.PrepareTransportationCheckpoint(ctx, requests, state, owned, &transportationFOM{})
	transportationOK(t, err)
	defer p.Rollback()
	foreign := scope
	foreign.Member.Federation = testForeign
	one := []TransportationCheckpointRestore{{Scope: foreign, Binding: requests[0].Binding}}
	if _, err := f.r.PrepareTransportationCheckpoint(ctx, one, state, owned, &transportationFOM{}); !errors.Is(err, core.ErrTransportationPendingLimit) {
		t.Fatal("activated foreign lease exceeded retained-ticket limit", err)
	}
	commit, err := f.r.PrepareImportState("fed", state)
	transportationOK(t, err)
	commit()
	p.Commit()
	if _, err := f.r.PrepareTransportationCheckpoint(ctx, one, state, owned, &transportationFOM{}); !errors.Is(err, core.ErrTransportationPendingLimit) {
		t.Fatal("unconsumed acknowledgements did not constrain live capacity", err)
	}
	if got := f.r.InteractionTransportType("fed", 1, 7); got != core.TransportTypeReliable {
		t.Fatal("acknowledgement installation applied effect", got)
	}
}
