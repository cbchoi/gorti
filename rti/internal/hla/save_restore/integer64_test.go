package savepoint

import (
	"bytes"
	"context"
	"math"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestInteger64SavePreservesExactScheduledTime(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	out := &fakeOutbox{}
	const target int64 = 1<<53 + 17
	var reserved int64
	mgr, err := NewOf(OptionsOf[int64]{Outbox: out, BundleStore: store,
		Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
		ScheduledTime: func(_ core.FederationName, value int64, _ []core.FederateHandle) (func() (bool, error), func(), error) {
			reserved = value
			return func() (bool, error) { return true, nil }, func() {}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	saveTime := target
	if err = mgr.RequestFederationSave(ctx, "integer", "exact", &saveTime); err != nil {
		t.Fatal(err)
	}
	if reserved != target {
		t.Fatalf("reserved %d, want %d", reserved, target)
	}
	if err = mgr.ProgressScheduledSave(ctx, "integer"); err != nil {
		t.Fatal(err)
	}
	if err = mgr.FederateSaveBegun(ctx, "integer", 1); err != nil {
		t.Fatal(err)
	}
	if err = mgr.FederateSaveComplete(ctx, "integer", 1); err != nil {
		t.Fatal(err)
	}
	manifest, err := mgr.LoadManifest("integer", "exact")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SaveTime == nil || *manifest.SaveTime != target {
		t.Fatalf("saved time = %v", manifest.SaveTime)
	}
	if manifest.LogicalTimeImplementationName != "HLAinteger64Time" {
		t.Fatalf("factory = %s", manifest.LogicalTimeImplementationName)
	}
}

func TestInteger64BundlePreservesFinalAndLargeTime(t *testing.T) {
	for _, value := range []int64{1<<53 + 1, math.MaxInt64} {
		var data bytes.Buffer
		if err := WriteBundleOf(&data, ManifestOf[int64]{SaveTime: &value, LogicalTimeImplementationName: "HLAinteger64Time"}, nil); err != nil {
			t.Fatal(err)
		}
		manifest, _, err := ReadBundleOf[int64](&data)
		if err != nil {
			t.Fatal(err)
		}
		if manifest.SaveTime == nil || *manifest.SaveTime != value {
			t.Fatal("integer timestamp lost precision")
		}
	}
}

func TestInteger64SaveRejectsMixedFactoryBeforeSnapshotPreparation(t *testing.T) {
	mgr, err := NewOf(OptionsOf[int64]{Outbox: &fakeOutbox{}, BundleStore: newMemStore()})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "HLAfloat64Time", "custom"} {
		if err = mgr.validateRestoreTimeImplementation("integer", ManifestOf[int64]{LogicalTimeImplementationName: name}); err == nil {
			t.Fatalf("accepted saved representation %q", name)
		}
	}
	if err = mgr.validateRestoreTimeImplementation("integer", ManifestOf[int64]{LogicalTimeImplementationName: "HLAinteger64Time"}); err != nil {
		t.Fatal(err)
	}
}

func TestScalarTimeValidationKeepsIntegerFinalFinite(t *testing.T) {
	final := int64(math.MaxInt64)
	if err := (SaveHistoryOf[int64]{Known: true, HasSave: true, Name: "last", Time: &final}).Validate(); err != nil {
		t.Fatal(err)
	}
	negative := int64(-1)
	if err := (SaveHistoryOf[int64]{Known: true, HasSave: true, Time: &negative}).Validate(); err == nil {
		t.Fatal("negative integer time accepted")
	}
}
