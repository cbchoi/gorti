package savebundle

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

var _ savepoint.ManagerSnapshotter = Retractions{}
var _ savepoint.PreparedRestoreSnapshotter = Retractions{}
var _ savepoint.RetractionHistoryPreview = Retractions{}

func TestRetractionSnapshotRoundTripMergesAndProvesEmpty(t *testing.T) {
	time, _, _ := newTimeStateCodec(t)
	codec := Retractions{Manager: time.Manager}
	ctx := context.Background()
	empty, err := codec.Marshal("empty")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(empty, []byte(`"complete":true`)) {
		t.Fatalf("empty completeness missing: %s", empty)
	}
	if next, err := codec.PreviewNextRetractionHandle("empty", empty, 1); err != nil || next != 1 {
		t.Fatalf("empty hint %d %v", next, err)
	}
	accept := func(id uint64) {
		t.Helper()
		r, err := time.Manager.ReserveRetractionID(ctx, "fed", 1, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	accept(7)
	saved, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	again, err := codec.Marshal("fed")
	if err != nil || !bytes.Equal(saved, again) {
		t.Fatalf("nondeterministic snapshot: %s %v", again, err)
	}
	accept(23)
	if next, err := codec.PreviewNextRetractionHandle("fed", saved, 1); err != nil || next != 24 {
		t.Fatalf("merged preview %d %v", next, err)
	}
	prepared, err := codec.PrepareRestoreState("fed", saved)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{7, 23} {
		if _, err := time.Manager.ReserveRetractionID(ctx, "fed", 1, id); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
			t.Fatalf("ID %d reused: %v", id, err)
		}
	}
	accept(3)
}

func TestRetractionSnapshotRejectsLegacyAndMalformedHistory(t *testing.T) {
	time, _, _ := newTimeStateCodec(t)
	codec := Retractions{Manager: time.Manager}
	for _, input := range []string{
		``, `null`, `{}`, `{"version":1}`, `{"version":1,"history":null}`,
		`{"version":1,"history":{"producers":[]}}`,
		`{"version":2,"history":{"complete":true}}`,
		`{"version":1,"history":{"complete":true,"producers":[{"producer":0,"accepted":[]}]}}`,
		`{"version":1,"history":{"complete":true,"producers":[{"producer":1,"accepted":[{"first":0,"last":1}]}]}}`,
		`{"version":1,"history":{"complete":true,"producers":[{"producer":1,"accepted":[{"first":9,"last":7}]}]}}`,
		`{"version":1,"history":{"complete":true,"producers":[{"producer":1,"accepted":[{"first":7,"last":9},{"first":8,"last":10}]}]}}`,
		`{"version":1,"history":{"complete":true,"producers":[{"producer":1},{"producer":1}]}}`,
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := codec.PrepareRestoreState("fed", []byte(input)); err == nil {
				t.Fatal("invalid ledger accepted")
			}
		})
	}
}
