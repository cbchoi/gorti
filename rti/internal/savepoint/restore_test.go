package savepoint

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestRequestRestore_BundleNotFound(t *testing.T) {
	mgr, err := New(Options{Outbox: &fakeOutbox{}, BundleStore: newMemStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = mgr.RequestFederationRestore(context.Background(), "fed", "no-such")
	if !errors.Is(err, ErrSaveBundleNotFound) {
		t.Errorf("err = %v, want ErrSaveBundleNotFound", err)
	}
}

func TestRequestRestore_AlreadyInProgress(t *testing.T) {
	store := newMemStore()
	// Pre-load a valid bundle for "fed"/"lbl".
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.RequestFederationSave(ctx, "fed", "lbl", nil)
	_ = mgr.FederateSaveComplete(ctx, "fed", 1)

	if err := mgr.RequestFederationRestore(ctx, "fed", "lbl"); err != nil {
		t.Fatalf("first RequestFederationRestore: %v", err)
	}
	err = mgr.RequestFederationRestore(ctx, "fed", "lbl")
	if !errors.Is(err, core.ErrRestoreAlreadyInProgress) {
		t.Errorf("err = %v, want ErrRestoreAlreadyInProgress", err)
	}
}

func TestRestore_FederateNotInRestore(t *testing.T) {
	mgr, err := New(Options{Outbox: &fakeOutbox{}, BundleStore: newMemStore()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.FederateRestoreComplete(context.Background(), "fed", 1); !errors.Is(err, core.ErrFederateNotInRestore) {
		t.Errorf("err = %v, want ErrFederateNotInRestore", err)
	}
}

func TestRestore_AggregatesAndCompletes(t *testing.T) {
	store := newMemStore()
	mgr, err := New(Options{
		Outbox:      &fakeOutbox{},
		BundleStore: store,
		Members:     func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2, 3} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	_ = mgr.RequestFederationSave(ctx, "fed", "lbl", nil)
	for _, h := range []core.FederateHandle{1, 2, 3} {
		_ = mgr.FederateSaveComplete(ctx, "fed", h)
	}

	if err := mgr.RequestFederationRestore(ctx, "fed", "lbl"); err != nil {
		t.Fatalf("RequestFederationRestore: %v", err)
	}
	if got := mgr.QueryRestoreState("fed", "lbl"); got != RestoreInitiated {
		t.Errorf("post-request QueryRestoreState = %v, want RestoreInitiated", got)
	}
	for _, h := range []core.FederateHandle{1, 2, 3} {
		if err := mgr.FederateRestoreComplete(ctx, "fed", h); err != nil {
			t.Fatalf("FederateRestoreComplete(%d): %v", h, err)
		}
	}
	if got := mgr.QueryRestoreState("fed", "lbl"); got != RestoreCompleted {
		t.Errorf("post-complete QueryRestoreState = %v, want RestoreCompleted", got)
	}
}

func TestBundleFormat_RoundTrip(t *testing.T) {
	saveTime := core.LogicalTime(7.25)
	in := Manifest{
		Federation: "test-fed",
		Label:      "alpha",
		SaveTime:   &saveTime,
		Federates:  []core.FederateHandle{10, 20, 30},
	}
	eventLog := []byte{0xde, 0xad, 0xbe, 0xef}

	var buf bytes.Buffer
	if err := WriteBundle(&buf, in, eventLog); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	out, log, err := ReadBundle(&buf)
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}
	if out.Federation != in.Federation || out.Label != in.Label {
		t.Errorf("identity mismatch: got (%q,%q), want (%q,%q)",
			out.Federation, out.Label, in.Federation, in.Label)
	}
	if out.SaveTime == nil || *out.SaveTime != saveTime {
		t.Errorf("SaveTime = %v, want %v", out.SaveTime, saveTime)
	}
	if len(out.Federates) != 3 || out.Federates[0] != 10 || out.Federates[2] != 30 {
		t.Errorf("Federates = %v, want [10 20 30]", out.Federates)
	}
	if !bytes.Equal(log, eventLog) {
		t.Errorf("event-log slice mismatch: got %x, want %x", log, eventLog)
	}
}

func TestBundleFormat_TruncatedHeader(t *testing.T) {
	_, _, err := ReadBundle(bytes.NewReader([]byte{0x01, 0x02}))
	if !errors.Is(err, ErrBundleCorrupt) {
		t.Errorf("err = %v, want ErrBundleCorrupt", err)
	}
}

func TestBundleFormat_VersionMismatch(t *testing.T) {
	// Build a bundle with version 0xFF — should fail version check.
	in := Manifest{
		Version:    0xFF,
		Federation: "f",
		Label:      "l",
		Federates:  []core.FederateHandle{1},
	}
	var buf bytes.Buffer
	// We can't use WriteBundle directly because it overrides Version
	// when zero — but a non-zero Version is preserved as-is.
	if err := WriteBundle(&buf, in, nil); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	_, _, err := ReadBundle(&buf)
	if !errors.Is(err, ErrBundleCorrupt) {
		t.Errorf("err = %v, want ErrBundleCorrupt", err)
	}
}
