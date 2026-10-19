package savebundle

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

type syncFailureOutbox struct{ failed [][]core.FederateHandle }

func (o *syncFailureOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, event core.OutboundEvent) error {
	if e, ok := event.(*syncpkg.SynchronizedEvent); ok {
		o.failed = append(o.failed, e.FailedToSync())
	}
	return nil
}

func TestSynchronizationSnapshotPreservesFailedAchievement(t *testing.T) {
	ctx := context.Background()
	out := &syncFailureOutbox{}
	a, err := syncpkg.New(syncpkg.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Register(ctx, "f", "point", nil, []core.FederateHandle{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err = a.AchieveWith(ctx, "f", 1, "point", false); err != nil {
		t.Fatal(err)
	}
	data, err := (Synchronization{Manager: a}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	b, err := syncpkg.New(syncpkg.Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	if err = (Synchronization{Manager: b}).Unmarshal("f", data); err != nil {
		t.Fatal(err)
	}
	if err = b.Achieve(ctx, "f", 2, "point"); err != nil {
		t.Fatal(err)
	}
	if len(out.failed) != 2 || !reflect.DeepEqual(out.failed[0], []core.FederateHandle{1}) || !reflect.DeepEqual(out.failed[1], []core.FederateHandle{1}) {
		t.Fatalf("restored terminal failed participants: %v", out.failed)
	}
}

func TestSynchronizationLegacyUnprovenFailureHistoryRejected(t *testing.T) {
	m, err := syncpkg.New(syncpkg.Options{Outbox: &syncFailureOutbox{}})
	if err != nil {
		t.Fatal(err)
	}
	data := graphProto(t, &rtiv1.SyncManagerState{Points: []*rtiv1.SyncPointStateEntry{{Label: "p", State: rtiv1.SyncPointSnapshotState_SYNC_POINT_SNAPSHOT_STATE_ANNOUNCED, Required: []uint64{1, 2}, Achieved: []uint64{1}}}})
	if _, err := (Synchronization{Manager: m}).PrepareRestore("f", data); err == nil {
		t.Fatal("legacy achieved participant silently assumed successful")
	}
}
