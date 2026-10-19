package sync

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func controlRestoreCaptured(callback CallbackState) RestoredCallback {
	return RestoredCallback{Kind: callback.Kind.observedKind(), Recipient: callback.Recipient,
		Sequence: callback.Sequence, Label: callback.Label, OutcomeOnly: callback.Entered,
		Tag: append([]byte(nil), callback.Tag...), Required: append([]core.FederateHandle(nil), callback.Required...),
		Failed: append([]core.FederateHandle(nil), callback.Failed...), Reason: callback.Reason}
}

func TestControlRestoreCapturedSynchronizationDoesNotReannounce(t *testing.T) {
	ctx := context.Background()
	sourceOut := &completionOutbox{}
	source := strictCompletionManager(t, sourceOut, 1, 2)
	mustCompletion(t, source.RegisterBy(ctx, "f", 1, "captured", []byte{0, 255}, nil))
	image := source.ExportState("f")
	targetOut := &completionOutbox{}
	target := strictCompletionManager(t, targetOut, 1, 2)
	commit, err := target.PrepareImport("f", image)
	mustCompletion(t, err)
	commit()
	var captured []RestoredCallback
	for _, callback := range image.Obligations {
		captured = append(captured, controlRestoreCaptured(callback))
	}
	mustCompletion(t, ValidateRestoredCallbacks(image, captured))
	mustCompletion(t, target.ReconcileRestoredCallbacks("f", captured))
	// The authoritative transport restore already owns these three carriers.
	for _, callback := range image.Obligations {
		delivery := callback.delivery()
		mustCompletion(t, targetOut.Send(ctx, "f", delivery.Recipient, delivery.Event))
	}
	mustCompletion(t, target.RetryPending(ctx, "f"))
	if got := len(targetOut.sent()); got != len(image.Obligations) {
		t.Fatalf("domain restore duplicated captured carriers: got %d, want %d", got, len(image.Obligations))
	}
	if err := target.Achieve(ctx, "f", 1, "captured"); err == nil {
		t.Fatal("suppression invented announcement entry")
	}
	if len(target.ExportState("f").Obligations) != len(image.Obligations) {
		t.Fatal("suppression discharged invocation obligations")
	}
}

func TestControlRestoreReconciliationLeavesUncapturedRetryable(t *testing.T) {
	ctx := context.Background()
	source := strictCompletionManager(t, &completionOutbox{}, 1, 2)
	mustCompletion(t, source.RegisterBy(ctx, "f", 1, "partial", []byte{0, 255}, nil))
	image := source.ExportState("f")
	out := &completionOutbox{fail: true}
	target := strictCompletionManager(t, out, 1, 2)
	commit, err := target.PrepareImport("f", image)
	mustCompletion(t, err)
	commit()
	captured := []RestoredCallback{controlRestoreCaptured(image.Obligations[0])}
	mustCompletion(t, target.ReconcileRestoredCallbacks("f", captured))
	mustCompletion(t, target.ReconcileRestoredCallbacks("f", captured))
	if err := target.RetryPending(ctx, "f"); err == nil {
		t.Fatal("retry fixture did not reject admission")
	}
	state := target.ExportState("f")
	if len(state.Pending) != 2 || len(state.Obligations) != 3 || state.NextSequence != image.NextSequence {
		t.Fatal("partial suppression lost uncaptured obligations")
	}
	out.setFail(false)
	mustCompletion(t, target.RetryPending(ctx, "f"))
	for _, delivery := range out.sent() {
		if delivery.Event.Seq() == captured[0].Sequence {
			t.Fatal("captured callback returned to retry")
		}
	}
	if len(out.sent()) != 2 {
		t.Fatal("uncaptured callbacks were not retried once")
	}
}

func TestControlRestoreReconciliationRejectsMismatchAtomically(t *testing.T) {
	ctx := context.Background()
	source := strictCompletionManager(t, &completionOutbox{}, 1, 2)
	mustCompletion(t, source.RegisterBy(ctx, "f", 1, "immutable", []byte{0, 255}, nil))
	image := source.ExportState("f")
	for name, mutate := range map[string]func(*RestoredCallback){
		"kind":      func(c *RestoredCallback) { c.Kind = core.SyncCallbackSynchronized },
		"label":     func(c *RestoredCallback) { c.Label = "other" },
		"recipient": func(c *RestoredCallback) { c.Recipient = 99 },
		"sequence":  func(c *RestoredCallback) { c.Sequence = image.NextSequence + 1 },
		"phase":     func(c *RestoredCallback) { c.OutcomeOnly = true },
		"tag":       func(c *RestoredCallback) { c.Tag = []byte("changed") },
		"required":  func(c *RestoredCallback) { c.Required = []core.FederateHandle{99} },
		"failed":    func(c *RestoredCallback) { c.Failed = []core.FederateHandle{99} },
		"reason":    func(c *RestoredCallback) { c.Reason = RegistrationFailureSetMemberNotJoined },
	} {
		t.Run(name, func(t *testing.T) {
			target := strictCompletionManager(t, &completionOutbox{}, 1, 2)
			commit, err := target.PrepareImport("f", image)
			mustCompletion(t, err)
			commit()
			before := target.ExportState("f")
			captured := controlRestoreCaptured(image.Obligations[1])
			mutate(&captured)
			rows := []RestoredCallback{controlRestoreCaptured(image.Obligations[0]), captured}
			if ValidateRestoredCallbacks(image, rows) == nil || target.ReconcileRestoredCallbacks("f", rows) == nil {
				t.Fatal("mismatched recovery accepted")
			}
			if !reflect.DeepEqual(before, target.ExportState("f")) {
				t.Fatal("rejected reconciliation changed pending or invocation state")
			}
		})
	}
	captured := controlRestoreCaptured(image.Obligations[0])
	if ValidateRestoredCallbacks(image, []RestoredCallback{captured, captured}) == nil || ValidateRestoredCallbacks(nil, []RestoredCallback{captured}) == nil {
		t.Fatal("duplicate or missing obligation accepted")
	}
}

func TestControlRestoreEnteredOutcomesDoNotRequireNewEntry(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []callbackKind{callbackAnnounce, callbackSynchronized, callbackRegistrationSucceeded, callbackRegistrationFailed} {
		t.Run(string(rune('0'+kind)), func(t *testing.T) {
			sourceOut := &completionOutbox{}
			source := strictCompletionManager(t, sourceOut, 1)
			mustCompletion(t, source.RegisterBy(ctx, "f", 1, "outcome", []byte("tag"), nil))
			if kind == callbackRegistrationFailed {
				mustCompletion(t, source.RegisterBy(ctx, "f", 1, "outcome", nil, nil))
			}
			if kind == callbackSynchronized {
				enterCompletionAnnouncements(t, source, sourceOut)
				mustCompletion(t, source.AchieveWith(ctx, "f", 1, "outcome", false))
			}
			var chosen CallbackState
			for _, callback := range source.ExportState("f").Obligations {
				if callback.Kind == kind {
					chosen = callback
				}
			}
			if chosen.Sequence == 0 {
				t.Fatal("required control category not emitted")
			}
			mustCompletion(t, source.ObserveSyncCallbackEntry(ctx, "f", chosen.Recipient, kind.observedKind(), chosen.Label, chosen.Sequence))
			image := source.ExportState("f")
			var captured []RestoredCallback
			for _, callback := range image.Obligations {
				captured = append(captured, controlRestoreCaptured(callback))
			}
			mustCompletion(t, ValidateRestoredCallbacks(image, captured))
			targetOut := &completionOutbox{}
			target := strictCompletionManager(t, targetOut, 1)
			commit, err := target.PrepareImport("f", image)
			mustCompletion(t, err)
			commit()
			mustCompletion(t, target.ReconcileRestoredCallbacks("f", captured))
			mustCompletion(t, target.RetryPending(ctx, "f"))
			if len(targetOut.sent()) != 0 {
				t.Fatal("executed callback body was reannounced")
			}
			mustCompletion(t, target.ObserveSyncCallback(ctx, "f", chosen.Recipient, kind.observedKind(), chosen.Label, chosen.Sequence))
			mustCompletion(t, target.ObserveSyncCallback(ctx, "f", chosen.Recipient, kind.observedKind(), chosen.Label, chosen.Sequence))
			for _, callback := range target.ExportState("f").Obligations {
				if callback.Sequence == chosen.Sequence {
					t.Fatal("outcome-only report did not discharge entered obligation")
				}
			}
		})
	}
}

func TestControlRestoreRetiredLabelDoesNotAffectReusedEpoch(t *testing.T) {
	ctx := context.Background()
	sourceOut := &completionOutbox{}
	source := strictCompletionManager(t, sourceOut, 1)
	mustCompletion(t, source.RegisterBy(ctx, "f", 1, "reused", []byte("old"), nil))
	enterCompletionAnnouncements(t, source, sourceOut)
	mustCompletion(t, source.Achieve(ctx, "f", 1, "reused"))
	sequence := terminalIdentities(sourceOut, "reused")[1]
	reportCompletion(t, source, 1, core.SyncCallbackSynchronized, "reused", sequence)
	var captured []RestoredCallback
	for _, callback := range source.ExportState("f").Obligations {
		captured = append(captured, controlRestoreCaptured(callback))
	}
	mustCompletion(t, source.RegisterBy(ctx, "f", 1, "reused", []byte("new"), nil))
	image := source.ExportState("f")
	mustCompletion(t, ValidateRestoredCallbacks(image, captured))
	out := &completionOutbox{}
	target := strictCompletionManager(t, out, 1)
	commit, err := target.PrepareImport("f", image)
	mustCompletion(t, err)
	commit()
	mustCompletion(t, target.ReconcileRestoredCallbacks("f", captured))
	mustCompletion(t, target.RetryPending(ctx, "f"))
	if len(out.sent()) != 2 {
		t.Fatal("old epoch reannounced or new epoch suppressed")
	}
	for _, recovered := range captured {
		if !recovered.OutcomeOnly {
			mustCompletion(t, target.ObserveSyncCallbackEntry(ctx, "f", recovered.Recipient, recovered.Kind, recovered.Label, recovered.Sequence))
		}
		mustCompletion(t, target.ObserveSyncCallback(ctx, "f", recovered.Recipient, recovered.Kind, recovered.Label, recovered.Sequence))
	}
	state := target.ExportState("f")
	if len(state.Obligations) != 2 || len(state.Points) != 1 || state.Points[0].Invocations[0].AnnounceEntered || string(state.Points[0].Tag) != "new" || state.Points[0].Epoch != image.Points[0].Epoch {
		t.Fatal("old recovery outcome changed newer same-label epoch")
	}
}
