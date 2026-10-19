package sync

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestCompletionRestoreRequeuesAdmittedCallbacksWithOriginalIdentity(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", []byte("tag"), nil))
	mustCompletion(t, m.RegisterBy(ctx, "f", 2, "p", nil, nil))
	image := m.ExportState("f")
	if len(image.Pending) != 0 {
		t.Fatal("accepted callbacks should not remain pending live admission")
	}
	nextOut := &completionOutbox{}
	next := strictCompletionManager(t, nextOut, 1, 2)
	commit, err := next.PrepareImport("f", image)
	mustCompletion(t, err)
	commit()
	mustCompletion(t, next.RetryPending(ctx, "f"))
	if !reflect.DeepEqual(out.sent(), nextOut.sent()) {
		t.Fatalf("admitted but uninvoked callbacks lost on restore: before=%v after=%v", out.sent(), nextOut.sent())
	}
	if got := next.ExportState("f"); got.NextSequence != image.NextSequence || got.NextEpoch != image.NextEpoch {
		t.Fatal("retry minted new callback identities or label epochs")
	}
	if err := next.Achieve(ctx, "f", core.FederateHandle(1), "p"); err == nil {
		t.Fatal("restore/requeue fabricated callback entry")
	}
}

func TestCompletionRestoreKeepsConfirmationAfterLabelRetirement(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", []byte("old"), nil))
	enterCompletionAnnouncements(t, m, out)
	mustCompletion(t, m.Achieve(ctx, "f", 1, "p"))
	terminal := terminalIdentities(out, "p")[1]
	reportCompletion(t, m, 1, core.SyncCallbackSynchronized, "p", terminal)
	if m.QueryState("f", "p") != StateUnknown {
		t.Fatal("terminal invocation failed to retire point")
	}
	image := m.ExportState("f")
	if len(image.Obligations) != 2 {
		t.Fatal("registration confirmation or entered announcement lost on retirement")
	}
	nextOut := &completionOutbox{}
	next := strictCompletionManager(t, nextOut, 1)
	commit, err := next.PrepareImport("f", image)
	mustCompletion(t, err)
	commit()
	mustCompletion(t, next.RetryPending(ctx, "f"))
	if len(nextOut.sent()) != 2 {
		t.Fatal("retired point obligations not restored")
	}
	mustCompletion(t, next.RegisterBy(ctx, "f", 1, "p", []byte("new"), nil))
	for _, callback := range image.Obligations {
		reportCompletion(t, next, callback.Recipient, callback.Kind.observedKind(), callback.Label, callback.Sequence)
	}
	if got := next.ExportState("f"); len(got.Obligations) != 2 || got.Points[0].Epoch <= image.NextEpoch || got.Points[0].Invocations[0].AnnounceEntered {
		t.Fatal("old epoch reports changed new label or left old obligations")
	}
}

func TestCompletionRestoreRetriesTerminalWithoutRepeatingCompletedRecipients(t *testing.T) {
	ctx := context.Background()
	out := &completionOutbox{}
	m := strictCompletionManager(t, out, 1, 2)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", nil, nil))
	for _, callback := range m.ExportState("f").Obligations {
		reportCompletion(t, m, callback.Recipient, callback.Kind.observedKind(), callback.Label, callback.Sequence)
	}
	mustCompletion(t, m.Achieve(ctx, "f", 1, "p"))
	mustCompletion(t, m.AchieveWith(ctx, "f", 2, "p", false))
	terminal := terminalIdentities(out, "p")
	reportCompletion(t, m, 1, core.SyncCallbackSynchronized, "p", terminal[1])
	mustCompletion(t, m.ObserveSyncCallbackEntry(ctx, "f", 2, core.SyncCallbackSynchronized, "p", terminal[2]))
	image := m.ExportState("f")
	nextOut := &completionOutbox{fail: true}
	next := strictCompletionManager(t, nextOut, 1, 2)
	commit, err := next.PrepareImport("f", image)
	mustCompletion(t, err)
	image.Obligations[0].Failed[0] = 99 // The staged import must remain detached.
	commit()
	if err := next.RetryPending(ctx, "f"); err == nil || len(next.ExportState("f").Pending) != 1 {
		t.Fatal("restore retry capacity failure lost the terminal obligation")
	}
	nextOut.setFail(false)
	mustCompletion(t, next.RetryPending(ctx, "f"))
	deliveries := nextOut.sent()
	if len(deliveries) != 1 || deliveries[0].Recipient != 2 || deliveries[0].Event.Seq() != terminal[2] {
		t.Fatal("completed recipient repeated or callback identity changed")
	}
	if failed := deliveries[0].Event.(*SynchronizedEvent).FailedToSync(); !reflect.DeepEqual(failed, []core.FederateHandle{2}) {
		t.Fatal("immutable callback payload lost on restore")
	}
	if next.QueryState("f", "p") != StateAchieved {
		t.Fatal("retry/entry incorrectly retired label")
	}
	mustCompletion(t, next.ObserveSyncCallback(ctx, "f", 2, core.SyncCallbackSynchronized, "p", terminal[2]))
	if next.QueryState("f", "p") != StateUnknown || len(next.ExportState("f").Obligations) != 0 {
		t.Fatal("completion did not discharge restored terminal obligation")
	}
}

func TestCompletionReportsDoNotInventEntryAndRejectMissingDurableImage(t *testing.T) {
	ctx := context.Background()
	m := strictCompletionManager(t, &completionOutbox{}, 1)
	mustCompletion(t, m.RegisterBy(ctx, "f", 1, "p", nil, nil))
	image := m.ExportState("f")
	for _, callback := range image.Obligations {
		if err := m.ObserveSyncCallback(ctx, "f", callback.Recipient, callback.Kind.observedKind(), callback.Label, callback.Sequence); err == nil {
			t.Fatal("completion without entry accepted")
		}
	}
	if !reflect.DeepEqual(image, m.ExportState("f")) {
		t.Fatal("completion report invented callback entry")
	}
	image.Obligations = nil
	if _, err := m.PrepareImport("f", image); err == nil {
		t.Fatal("strict image without outstanding ledger accepted")
	}
	image = m.ExportState("f")
	image.Version = 2
	if _, err := m.PrepareImport("f", image); err == nil {
		t.Fatal("old strict image fabricated admitted confirmations")
	}
}
