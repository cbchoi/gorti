package grpc

import (
	"context"
	"errors"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type receiptRestoreReservation struct {
	commit  func() error
	release func()
}

func (r receiptRestoreReservation) Commit() error { return r.commit() }
func (r receiptRestoreReservation) Release() {
	if r.release != nil {
		r.release()
	}
}

func restoreReceiptEvent(label string, target uint64) *rtiv1.FederateEvent {
	return &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_RestoreInitiate{RestoreInitiate: &rtiv1.InitiateFederateRestore{Label: label, FederateHandle: target}}}
}

func TestCallbackReceiptRestoreCommitRebindsOnlyCurrentInitiation(t *testing.T) {
	for _, sentBeforeRestore := range []bool{false, true} {
		t.Run(map[bool]string{false: "send-commit-late", true: "send-commit-before-restore"}[sentBeforeRestore], func(t *testing.T) {
			type callerKey struct{}
			ctxA := context.WithValue(context.Background(), callerKey{}, "A")
			ctxB := context.WithValue(context.Background(), callerKey{}, "B")
			handles := map[string]core.FederateHandle{"A": 11, "B": 22}
			pending := map[core.FederateHandle]int{}
			observations := 0
			r, err := NewCallbackReceipts(CallbackReceiptOptions{
				Authenticate: func(ctx context.Context, _ core.FederationName, h core.FederateHandle) error {
					if handles[ctx.Value(callerKey{}).(string)] != h {
						return status.Error(codes.PermissionDenied, "wrong session")
					}
					return nil
				},
				Pending: func(_ core.FederationName, h core.FederateHandle, delta int) {
					pending[h] += delta
					if pending[h] < 0 {
						t.Errorf("negative restored pending count for %d", h)
					}
				},
				OnUnavailable: func(_ core.FederationName, _ core.FederateHandle, err error) {
					t.Errorf("valid restored baseline invalidated: %v", err)
				},
				Observe: func(_ context.Context, _ core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent, success bool, exception string) error {
					observations++
					if h != 22 || event.GetRestoreInitiate().GetLabel() != "saved" || success || exception != "FederateInternalError" {
						t.Errorf("fabricated callback outcome: %d %v %v %q", h, event, success, exception)
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			prepare := func(event *rtiv1.FederateEvent) ([]byte, func(bool)) {
				t.Helper()
				ticket, commit, err := r.Prepare(ctxA, "f", 11, event)
				if err != nil || len(ticket) != 32 {
					t.Fatal("prepare", err)
				}
				return ticket, commit
			}
			stale, staleCommit := prepare(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_Grant{Grant: &rtiv1.TimeAdvanceGrant{}}})
			staleCommit(true)
			oldAttempt, oldCommit := prepare(restoreReceiptEvent("saved", 22))
			oldCommit(true)
			begun, begunCommit := prepare(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_RestoreBegun{RestoreBegun: &rtiv1.FederationRestoreBegun{}}})
			begunCommit(true)
			current, send := prepare(restoreReceiptEvent("saved", 22))
			if sentBeforeRestore {
				send(true)
			}
			transition := r.PreserveRestoreTransition("f", "saved", []core.RestoreParticipant{{CurrentHandle: 11, SavedHandle: 22}, {CurrentHandle: 22, SavedHandle: 11}}, receiptRestoreReservation{commit: func() error {
				handles["A"], handles["B"] = 22, 11
				pending = map[core.FederateHandle]int{}
				return nil
			}})
			if err := transition.Commit(); err != nil {
				t.Fatal(err)
			}
			transition.Release()
			if err := transition.Commit(); err != nil {
				t.Fatal(err)
			}
			if pending[22] != 1 || pending[11] != 0 || observations != 0 {
				t.Fatal("restore invented a callback or lost pending accounting", pending, observations)
			}
			for _, ticket := range [][]byte{stale, oldAttempt, begun, make([]byte, 32)} {
				if err := r.Report(ctxA, "f", 22, ticket, true, ""); status.Code(err) != codes.FailedPrecondition {
					t.Fatalf("stale callback accepted: %v", err)
				}
			}
			if err := r.Report(ctxB, "f", 22, current, true, ""); status.Code(err) != codes.PermissionDenied {
				t.Fatal("another session used the target handle", err)
			}
			if err := r.Report(ctxB, "f", 11, current, true, ""); status.Code(err) != codes.FailedPrecondition {
				t.Fatal("another authenticated member used the receipt", err)
			}
			if err := r.Report(ctxA, "other-federation", 22, current, true, ""); status.Code(err) != codes.FailedPrecondition {
				t.Fatal("cross-federation receipt accepted", err)
			}
			if err := r.Report(ctxA, "f", 22, current, false, "FederateInternalError"); err != nil {
				t.Fatal(err)
			}
			if !sentBeforeRestore && observations != 0 {
				t.Fatal("uncommitted send counted")
			}
			send(true)
			if err := r.Report(ctxA, "f", 22, current, false, "FederateInternalError"); err != nil {
				t.Fatal(err)
			}
			if observations != 1 || pending[22] != 0 || r.pending != 0 {
				t.Fatal("callback not applied exactly once", observations, pending, r.pending)
			}
			if err := r.Report(ctxA, "f", 22, current, true, ""); status.Code(err) != codes.InvalidArgument {
				t.Fatal("conflicting outcome accepted", err)
			}
			r.ClearFederate("f", 22)
			if err := r.Report(ctxA, "f", 22, current, false, "FederateInternalError"); err == nil {
				t.Fatal("receipt survived resign")
			}
		})
	}
}

func TestCallbackReceiptRestoreRollbackDoesNotExpireOrRebind(t *testing.T) {
	for _, failCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "release", true: "commit-error"}[failCommit], func(t *testing.T) {
			ctx := context.Background()
			observed, pending, releases := 0, 0, 0
			r, _ := NewCallbackReceipts(CallbackReceiptOptions{
				Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
				Pending:      func(_ core.FederationName, _ core.FederateHandle, delta int) { pending += delta },
				Observe: func(_ context.Context, _ core.FederationName, h core.FederateHandle, _ *rtiv1.FederateEvent, _ bool, _ string) error {
					observed++
					if h != 3 {
						t.Error("failed restore rebound the receipt")
					}
					return nil
				},
			})
			ticket, sent, _ := r.Prepare(ctx, "f", 3, restoreReceiptEvent("saved", 1))
			sent(true)
			want := errors.New("transition rejected")
			reservation := r.PreserveRestoreTransition("f", "saved", []core.RestoreParticipant{{CurrentHandle: 3, SavedHandle: 1}}, receiptRestoreReservation{commit: func() error { return want }, release: func() { releases++ }})
			if failCommit {
				if err := reservation.Commit(); !errors.Is(err, want) {
					t.Fatal(err)
				}
			}
			reservation.Release()
			reservation.Release()
			if pending != 1 || observed != 0 || releases != 1 {
				t.Fatal("prepare/rollback changed receipt lifecycle", pending, observed, releases)
			}
			if err := r.Report(ctx, "f", 1, ticket, true, ""); err == nil {
				t.Fatal("saved handle accepted before successful commit")
			}
			if err := r.Report(ctx, "f", 3, ticket, true, ""); err != nil {
				t.Fatal(err)
			}
			if pending != 0 || observed != 1 {
				t.Fatal("rollback lost real callback", pending, observed)
			}
		})
	}
}

func TestCallbackReceiptRestoreRejectsMismatchedInitiation(t *testing.T) {
	for _, mismatch := range []string{"label", "target", "participant", "failed-attempt", "previous-transition"} {
		t.Run(mismatch, func(t *testing.T) {
			ctx := context.Background()
			r, _ := NewCallbackReceipts(CallbackReceiptOptions{Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil }, Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
				t.Error("stale initiation counted")
				return nil
			}})
			event := restoreReceiptEvent("saved", 1)
			participants := []core.RestoreParticipant{{CurrentHandle: 3, SavedHandle: 1}}
			switch mismatch {
			case "label":
				event.GetRestoreInitiate().Label = "older"
			case "target":
				event.GetRestoreInitiate().FederateHandle = 2
			case "participant":
				participants[0].CurrentHandle = 4
			}
			ticket, sent, _ := r.Prepare(ctx, "f", 3, event)
			sent(true)
			if mismatch == "failed-attempt" {
				_, sendFailed, _ := r.Prepare(ctx, "f", 3, &rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_RestoreFailed{RestoreFailed: &rtiv1.FederationNotRestored{Label: "saved"}}})
				sendFailed(true)
			}
			commit := func() {
				t.Helper()
				if err := r.PreserveRestoreTransition("f", "saved", participants, receiptRestoreReservation{commit: func() error { return nil }}).Commit(); err != nil {
					t.Fatal(err)
				}
			}
			commit()
			if mismatch == "previous-transition" {
				participants[0].CurrentHandle = 1
				commit()
			}
			if err := r.Report(ctx, "f", 1, ticket, true, ""); status.Code(err) != codes.FailedPrecondition {
				t.Fatal("invalid receipt retained", err)
			}
		})
	}
}
