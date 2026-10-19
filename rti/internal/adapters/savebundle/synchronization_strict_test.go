package savebundle

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type strictSyncSnapshotOutbox struct{ deliveries []core.OutboxDelivery }

func (o *strictSyncSnapshotOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, e core.OutboundEvent) error {
	o.deliveries = append(o.deliveries, core.OutboxDelivery{Recipient: h, Event: e})
	return nil
}
func (o *strictSyncSnapshotOutbox) Reserve(_ context.Context, _ core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	return &strictSyncSnapshotReservation{out: o, deliveries: append([]core.OutboxDelivery(nil), deliveries...)}, nil
}

type strictSyncSnapshotReservation struct {
	out        *strictSyncSnapshotOutbox
	deliveries []core.OutboxDelivery
	done       bool
}

func (r *strictSyncSnapshotReservation) Commit() error {
	if !r.done {
		r.done = true
		r.out.deliveries = append(r.out.deliveries, r.deliveries...)
	}
	return nil
}
func (r *strictSyncSnapshotReservation) Release() { r.done = true }

func strictSyncSnapshotFixture(t *testing.T, h core.FederateHandle) (*syncpkg.Manager, *strictSyncSnapshotOutbox) {
	t.Helper()
	out := &strictSyncSnapshotOutbox{}
	m, err := syncpkg.New(syncpkg.Options{Outbox: out, RequireInvocations: true, Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{h} }})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterBy(context.Background(), "f", h, "p", []byte("exact-tag"), nil); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterBy(context.Background(), "f", h, "p", nil, nil); err != nil {
		t.Fatal(err)
	}
	return m, out
}

func TestStrictSynchronizationSharedAdapterPreservesAdmittedConfirmations(t *testing.T) {
	ctx := context.Background()
	m, beforeOut := strictSyncSnapshotFixture(t, 1)
	data, err := (Synchronization{Manager: m}).Marshal("f")
	if err != nil || !bytes.HasPrefix(data, []byte(SynchronizationCheckpointPrefix)) {
		t.Fatal("old composition lost strict DTO", err)
	}
	nextOut := &strictSyncSnapshotOutbox{}
	next, err := syncpkg.New(syncpkg.Options{Outbox: nextOut, RequireInvocations: true, Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} }})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := (StrictSynchronization{Manager: next}).PrepareRestore("f", data)
	if err != nil {
		t.Fatal(err)
	}
	if next.ExportState("f") != nil {
		t.Fatal("restore mutated before commit")
	}
	commit()
	if err := next.RetryPending(ctx, "f"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeOut.deliveries, nextOut.deliveries) {
		t.Fatal("callback identities/kinds/payloads lost across persistence")
	}
	if err := next.Achieve(ctx, "f", 1, "p"); err == nil {
		t.Fatal("requeue fabricated invocation entry")
	}
	if _, err := (StrictSynchronization{Manager: next}).PrepareRestore("f", append(data, []byte("{}")...)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	duplicate := []byte(SynchronizationCheckpointPrefix + `{"Version":3,"version":3}`)
	if _, err := (StrictSynchronization{Manager: next}).PrepareRestore("f", duplicate); err == nil {
		t.Fatal("duplicate JSON alias accepted")
	}
}

func TestStrictSynchronizationRestoreGraphAndCrossGenerationRebind(t *testing.T) {
	ctx := context.Background()
	const source core.FederateHandle = 1<<32 | 1
	const target core.FederateHandle = 9<<32 | 7
	m, _ := strictSyncSnapshotFixture(t, source)
	data, err := (StrictSynchronization{Manager: m}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	repo := composition.NewFOMRepository()
	model, err := repo.Load(ctx, []core.FOMModule{{Path: "sync.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	generation := uint64(1)
	manifest := savepoint.Manifest{Federation: "f", FederationGeneration: &generation, Federates: []core.FederateHandle{source}, FederateNames: []string{"one"}, ManagerSnapshots: map[string][]byte{
		"object": nil, "ownership": nil, "ddm": nil, "mom": nil,
		"sync": data, "declaration": graphJSON(t, declarationSnapshot{Version: 3}), "time": graphJSON(t, timeSnapshot{Version: 1}),
		"retractions": graphJSON(t, retractionSnapshot{Version: 1, History: &timepkg.RetractionHistory{Complete: true}}),
	}}
	graph := RestoreGraph{}
	if err := graph.Validate("f", core.CheckpointManifest(manifest), model); err != nil {
		t.Fatalf("strict graph rejected: %v", err)
	}
	references, err := graph.References(core.CheckpointManifest(manifest))
	if err != nil || !reflect.DeepEqual(references, []core.FederateHandle{source}) {
		t.Fatalf("strict references: %v %v", references, err)
	}
	rewritten, err := (Synchronization{}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) {
		if h != source {
			t.Fatalf("unexpected source %d", h)
		}
		return target, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := readStrictSynchronization(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("f")
	if state.NextEpoch != before.NextEpoch || state.NextSequence != before.NextSequence || state.Points[0].Required[0] != target || state.Points[0].Invocations[0].Recipient != target {
		t.Fatal("rebind changed callback identity or missed participant")
	}
	for i, callback := range state.Obligations {
		if callback.Recipient != target || callback.Sequence != before.Obligations[i].Sequence || callback.Epoch != before.Obligations[i].Epoch {
			t.Fatal("obligation identity/rebind mismatch")
		}
	}
	manifest.ManagerSnapshots["sync"] = rewritten
	if err := graph.Validate("f", core.CheckpointManifest(manifest), model); err == nil {
		t.Fatal("foreign callback recipient accepted against saved roster")
	}
	manifest.Federates = []core.FederateHandle{target}
	generation = 9
	if err := graph.Validate("f", core.CheckpointManifest(manifest), model); err != nil {
		t.Fatal("rewritten graph rejected", err)
	}
}
