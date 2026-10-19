package server

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/adapters/auth/session"
	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle/storage"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const ownershipReviewFederation core.FederationName = "ownership-restore-review"

type ownershipRestoreReview struct {
	members        *federation.Manager
	owner          *ownership.Manager
	outbox         *delivery.MultiOutbox
	save           *savepoint.Manager
	sessions       *session.Manager
	receipts       *grpcadapter.OwnershipCallbackReceipts
	saved, current [2]core.FederateHandle
	channels       [2]<-chan []core.OutboundEvent
	auth, oldAuth  context.Context
	manifest       savepoint.Manifest
	oldReceipt     *rtiv1.OwnershipCallbackReceipt
	generation     uint64
}

type ownershipReviewReservationProbe struct {
	core.OutboxReservation
	beforeCommit      func()
	commits, releases int
}

func (r *ownershipReviewReservationProbe) Commit() error {
	r.commits++
	if r.beforeCommit != nil {
		r.beforeCommit()
	}
	return r.OutboxReservation.Commit()
}
func (r *ownershipReviewReservationProbe) Release() {
	r.releases++
	r.OutboxReservation.Release()
}
func ownershipReviewProbe(r core.OutboxReservation) *ownershipReviewReservationProbe {
	wrapper := r.(*ownershipScopeRestoreReservation)
	probe := &ownershipReviewReservationProbe{OutboxReservation: wrapper.OutboxReservation}
	wrapper.OutboxReservation = probe
	return probe
}

func newOwnershipRestoreReview(t *testing.T) *ownershipRestoreReview {
	t.Helper()
	ctx := context.Background()
	f := &ownershipRestoreReview{outbox: delivery.NewWithBatch(16, 1, 0)}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var err error
	f.members, err = federation.New(federation.Options{Clock: core.NewRealClock(), FOMs: composition.NewFOMRepository(), GenerationEpoch: 1})
	check(err)
	check(f.members.CreateFederation(ctx, core.CreateFederationRequest{Name: ownershipReviewFederation}))
	f.generation, _ = f.members.GenerationFor(ownershipReviewFederation)
	f.sessions, err = session.New(session.Options{Membership: f.members})
	check(err)
	t.Cleanup(f.sessions.Close)
	for i, name := range []string{"owner", "recipient"} {
		f.saved[i], err = f.members.JoinFederation(ctx, core.JoinFederationRequest{Federation: ownershipReviewFederation, FederateName: name})
		check(err)
		channel, closeQueue, err := f.outbox.Subscribe(ctx, ownershipReviewFederation, f.saved[i])
		check(err)
		f.channels[i] = channel
		t.Cleanup(func() { _ = closeQueue() })
	}
	f.current = f.saved
	// Establish a real queue epoch 1 before taking the ownership snapshot.
	initial, err := newRestoreTransition(f.members, f.outbox)(ctx, ownershipReviewFederation, f.participants(), f.terminals())
	check(err)
	check(initial.Commit())
	initial.Release()
	for _, channel := range f.channels {
		<-channel
	}
	f.owner, err = ownership.New(ownership.Options{Outbox: f.outbox})
	check(err)
	store, err := storage.NewFSStorage(t.TempDir())
	check(err)
	f.save, err = savepoint.New(savepoint.Options{Outbox: f.outbox, Members: f.members.MembersOf, BundleStore: store})
	check(err)
	f.receipts, err = newMandatoryOwnershipReceipts(f.owner, f.members, f.outbox, f.save, f.sessions.Authenticate)
	check(err)
	for _, handle := range f.saved {
		check(f.owner.EnableOwnershipCallbackReceipts(ctx, core.OwnershipCallbackScope{Federation: ownershipReviewFederation, Recipient: handle, FederationGeneration: f.generation, CallbackEpoch: 1}))
	}
	oldID, err := f.members.MemberIdentity(ownershipReviewFederation, f.saved[1])
	check(err)
	oldToken := bytes.Repeat([]byte{1}, session.TokenSize)
	f.sessions.Register(oldID, oldToken)
	f.oldAuth = metadata.NewIncomingContext(ctx, metadata.Pairs(session.MetadataKey, string(oldToken)))
	f.owner.RegisterInitialOwnership(ownershipReviewFederation, f.saved[0], 77, []core.AttributeHandle{9})
	check(f.owner.AcquireIfAvailable(ctx, ownershipReviewFederation, f.saved[1], 77, []core.AttributeHandle{9}, nil))
	event := (<-f.channels[1])[0]
	pb, err := grpcadapter.EncodeOutboundEvent(event)
	check(err)
	finish, err := f.receipts.Prepare(f.oldAuth, ownershipReviewFederation, f.saved[1], pb)
	check(err)
	if finish == nil || pb.GetOwnershipUnavailable() == nil {
		t.Fatal("missing real terminal ownership receipt")
	}
	finish(true)
	f.oldReceipt = pb.GetOwnershipReceipt()
	data, err := (savebundle.Ownership{Manager: f.owner}).Marshal(ownershipReviewFederation)
	check(err)
	f.manifest.ManagerSnapshots = map[string][]byte{savepoint.ManagerSnapshotKeyOwnership: data}
	// Rejoin the same name with a new handle/token and a fresh epoch-0 queue.
	check(f.members.ResignFederation(ctx, ownershipReviewFederation, f.saved[1], core.ResignActionNoAction))
	f.outbox.Unbind(ownershipReviewFederation, f.saved[1])
	f.current[1], err = f.members.JoinFederation(ctx, core.JoinFederationRequest{Federation: ownershipReviewFederation, FederateName: "recipient"})
	check(err)
	newID, err := f.members.MemberIdentity(ownershipReviewFederation, f.current[1])
	check(err)
	newToken := bytes.Repeat([]byte{2}, session.TokenSize)
	f.sessions.Register(newID, newToken)
	f.auth = metadata.NewIncomingContext(ctx, metadata.Pairs(session.MetadataKey, string(newToken)))
	channel, closeQueue, err := f.outbox.Subscribe(ctx, ownershipReviewFederation, f.current[1])
	check(err)
	f.channels[1] = channel
	t.Cleanup(func() { _ = closeQueue() })
	if epoch, err := f.outbox.CallbackEpoch(ownershipReviewFederation, f.current[1]); err != nil || epoch != 0 || f.current[1] == f.saved[1] {
		t.Fatalf("not a fresh rejoin: handle=%d epoch=%d err=%v", f.current[1], epoch, err)
	}
	return f
}

func (f *ownershipRestoreReview) participants() []savepoint.RestoreParticipant {
	return []savepoint.RestoreParticipant{{Name: "owner", CurrentHandle: f.current[0], SavedHandle: f.saved[0]}, {Name: "recipient", CurrentHandle: f.current[1], SavedHandle: f.saved[1]}}
}
func (f *ownershipRestoreReview) terminals() []core.OutboxDelivery {
	return []core.OutboxDelivery{{Recipient: f.current[0], Event: &savepoint.RestoredEvent{}}, {Recipient: f.current[1], Event: &savepoint.RestoredEvent{}}}
}
func (f *ownershipRestoreReview) prepare(t *testing.T) core.OutboxReservation {
	t.Helper()
	r, err := newOwnershipRestoreTransition(f.owner, f.members, f.outbox)(context.Background(), ownershipReviewFederation, f.manifest, f.participants(), f.terminals())
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f *ownershipRestoreReview) importSaved(t *testing.T) {
	t.Helper()
	commit, err := (savebundle.Ownership{Manager: f.owner}).PrepareRestore(ownershipReviewFederation, f.manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeyOwnership])
	if err != nil {
		t.Fatal(err)
	}
	commit()
}

func TestOwnershipRestoreReviewRejoinEpochFloorAndStaleReceipt(t *testing.T) {
	f := newOwnershipRestoreReview(t)
	exclusive := f.save.AcquireCheckpoint(ownershipReviewFederation)
	r := f.prepare(t)
	f.importSaved(t)
	probe := ownershipReviewProbe(r)
	probe.beforeCommit = func() {
		// This runs before the production membership/queue terminal commit.
		for _, handle := range f.saved {
			scope := core.OwnershipCallbackScope{Federation: ownershipReviewFederation, Recipient: handle, FederationGeneration: f.generation, CallbackEpoch: 2}
			if err := f.owner.EnableOwnershipCallbackReceipts(context.Background(), scope); err != nil {
				t.Errorf("terminal publication entered before every scope was valid: %v", err)
			}
		}
	}
	if err := r.Commit(); err != nil {
		r.Release()
		exclusive()
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	r.Release()
	r.Release()
	exclusive()
	if probe.commits != 1 || probe.releases != 0 {
		t.Fatalf("duplicate terminal reservation lifecycle: %+v", probe)
	}
	// Receiving either terminal must imply the entire enabled ledger is rebound.
	for i, channel := range f.channels {
		batch := <-channel
		if len(batch) != 1 {
			t.Fatalf("terminal batch: %+v", batch)
		}
		terminal, ok := batch[0].(*savepoint.RestoredEvent)
		if !ok || terminal.CallbackEpoch != 2 || terminal.FederateHandle != f.saved[i] {
			t.Fatalf("saved epoch 1/current 0 did not restore to 2: %+v", batch[0])
		}
		state := f.owner.ExportState(ownershipReviewFederation)
		if len(state.CallbackSessions) != 2 {
			t.Fatal("missing restored enabled scope")
		}
		for _, scope := range state.CallbackSessions {
			if scope.CallbackEpoch != 2 || scope.FederationGeneration != f.generation {
				t.Fatalf("terminal published before all scopes rebound: %+v", state.CallbackSessions)
			}
		}
	}
	service := grpcadapter.NewOwnershipCallbackService(f.receipts)
	invocation := &rtiv1.ReportOwnershipCallbackInvocationRequest{WireVersion: 1, FederationName: string(ownershipReviewFederation), FederateHandle: uint64(f.saved[1]), Receipt: f.oldReceipt}
	if _, err := service.ReportOwnershipCallbackInvocation(f.auth, invocation); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("saved receipt epoch reused after rejoin: %v", err)
	}
	if _, err := service.ReportOwnershipCallbackInvocation(f.oldAuth, invocation); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("retired same-name token accepted: %v", err)
	}
	invocation.FederateHandle = uint64(f.current[1])
	if _, err := service.ReportOwnershipCallbackInvocation(f.auth, invocation); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("retired current handle accepted: %v", err)
	}
	if !f.owner.HasAcquisitionObligation(ownershipReviewFederation, f.saved[1], 77, []core.AttributeHandle{9}) {
		t.Fatal("stale receipt cleared restored obligation")
	}
	_, err := service.RetryOwnershipCallbacks(f.auth, &rtiv1.OwnershipCallbackScopeRequest{WireVersion: 1, FederationName: string(ownershipReviewFederation), FederateHandle: uint64(f.saved[1]), FederationGeneration: f.generation, CallbackEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := grpcadapter.EncodeOutboundEvent((<-f.channels[1])[0])
	if err != nil {
		t.Fatal(err)
	}
	finish, err := f.receipts.Prepare(f.auth, ownershipReviewFederation, f.saved[1], pb)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	fresh := pb.GetOwnershipReceipt()
	if fresh.CallbackId != f.oldReceipt.CallbackId || fresh.DeliveryAttempt != 1 || fresh.CallbackEpoch != 2 {
		t.Fatalf("retry identity: %v", fresh)
	}
	invocation.FederateHandle, invocation.Receipt = uint64(f.saved[1]), fresh
	if _, err := service.ReportOwnershipCallbackInvocation(f.auth, invocation); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportOwnershipCallbackInvocation(f.auth, invocation); err != nil {
		t.Fatalf("duplicate fresh ACK: %v", err)
	}
	if f.owner.HasAcquisitionObligation(ownershipReviewFederation, f.saved[1], 77, []core.AttributeHandle{9}) {
		t.Fatal("fresh invocation did not clear obligation")
	}
	for _, channel := range f.channels {
		if len(channel) != 0 {
			t.Fatal("duplicate Commit published another terminal")
		}
	}
}

func TestOwnershipRestoreReviewMalformedScopesDoNotPublish(t *testing.T) {
	for _, mutation := range []string{"missing-recipient", "zero-generation", "wrong-recipient", "old-scope"} {
		t.Run(mutation, func(t *testing.T) {
			f := newOwnershipRestoreReview(t)
			exclusive := f.save.AcquireCheckpoint(ownershipReviewFederation)
			r := f.prepare(t)
			f.importSaved(t)
			before := f.owner.ExportState(ownershipReviewFederation)
			wrapper := r.(*ownershipScopeRestoreReservation)
			probe := ownershipReviewProbe(r)
			scope := wrapper.scopes[f.saved[0]]
			switch mutation {
			case "missing-recipient":
				delete(wrapper.scopes, f.saved[0])
			case "zero-generation":
				scope.FederationGeneration = 0
				wrapper.scopes[f.saved[0]] = scope
			case "wrong-recipient":
				scope.Recipient = f.saved[1]
				wrapper.scopes[f.saved[0]] = scope
			case "old-scope":
				scope.CallbackEpoch = 1
				wrapper.scopes[f.saved[0]] = scope
			}
			first := r.Commit()
			second := r.Commit()
			r.Release()
			r.Release()
			exclusive()
			if !errors.Is(first, core.ErrOwnershipCallbackScope) || second != first {
				t.Fatalf("malformed batch error/duplicate Commit changed: %v %v", first, second)
			}
			if probe.commits != 0 || probe.releases != 1 {
				t.Fatalf("failed scope batch escaped terminal fence: %+v", probe)
			}
			if !reflect.DeepEqual(before, f.owner.ExportState(ownershipReviewFederation)) {
				t.Fatal("failed validation partially rebound scope batch")
			}
			for i, handle := range f.current {
				if err := f.members.ValidateMember(ownershipReviewFederation, handle); err != nil {
					t.Fatal("failed validation committed roster")
				}
				expected := uint64(1)
				if i == 1 {
					expected = 0
				}
				if epoch, err := f.outbox.CallbackEpoch(ownershipReviewFederation, handle); err != nil || epoch != expected || len(f.channels[i]) != 0 {
					t.Fatalf("failed scope validation published terminal/epoch: %d %v", epoch, err)
				}
			}
		})
	}
}

func TestOwnershipRestoreReviewReleaseBeforeCommitIsStable(t *testing.T) {
	f := newOwnershipRestoreReview(t)
	before := f.owner.ExportState(ownershipReviewFederation)
	exclusive := f.save.AcquireCheckpoint(ownershipReviewFederation)
	r := f.prepare(t)
	probe := ownershipReviewProbe(r)
	r.Release()
	r.Release()
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := r.Commit(); err != nil {
		t.Fatal(err)
	}
	exclusive()
	if probe.commits != 0 || probe.releases != 1 {
		t.Fatalf("released reservation was reused: %+v", probe)
	}
	if !reflect.DeepEqual(before, f.owner.ExportState(ownershipReviewFederation)) {
		t.Fatal("released reservation mutated callback ledger")
	}
	for i, handle := range f.current {
		if err := f.members.ValidateMember(ownershipReviewFederation, handle); err != nil {
			t.Fatal(err)
		}
		if len(f.channels[i]) != 0 {
			t.Fatal("released reservation published terminal")
		}
	}
	if epoch, err := f.outbox.CallbackEpoch(ownershipReviewFederation, f.current[1]); err != nil || epoch != 0 {
		t.Fatalf("released reservation mutated epoch: %d %v", epoch, err)
	}
}

func TestOwnershipRestoreReviewUnknownSavedScopeRejectedBeforeReservation(t *testing.T) {
	f := newOwnershipRestoreReview(t)
	ctx := context.Background()
	// A valid enabled session whose handle is not in this saved roster.
	if err := f.owner.EnableOwnershipCallbackReceipts(ctx, core.OwnershipCallbackScope{Federation: ownershipReviewFederation, Recipient: f.current[1], FederationGeneration: f.generation, CallbackEpoch: 0}); err != nil {
		t.Fatal(err)
	}
	data, err := (savebundle.Ownership{Manager: f.owner}).Marshal(ownershipReviewFederation)
	if err != nil {
		t.Fatal(err)
	}
	f.manifest.ManagerSnapshots[savepoint.ManagerSnapshotKeyOwnership] = data
	before := f.owner.ExportState(ownershipReviewFederation)
	exclusive := f.save.AcquireCheckpoint(ownershipReviewFederation)
	r, err := newOwnershipRestoreTransition(f.owner, f.members, f.outbox)(ctx, ownershipReviewFederation, f.manifest, f.participants(), f.terminals())
	exclusive()
	if err == nil || r != nil {
		if r != nil {
			r.Release()
		}
		t.Fatalf("unknown saved callback recipient was admitted: %v", err)
	}
	if !reflect.DeepEqual(before, f.owner.ExportState(ownershipReviewFederation)) {
		t.Fatal("malformed saved roster changed callback state")
	}
	for i, handle := range f.current {
		if err := f.members.ValidateMember(ownershipReviewFederation, handle); err != nil {
			t.Fatal(err)
		}
		if len(f.channels[i]) != 0 {
			t.Fatal("malformed saved roster published terminal")
		}
	}
	if epoch, err := f.outbox.CallbackEpoch(ownershipReviewFederation, f.current[1]); err != nil || epoch != 0 {
		t.Fatalf("malformed saved roster changed fresh epoch: %d %v", epoch, err)
	}
}

func TestOwnershipScopeAdmissionReviewFailuresReleaseGates(t *testing.T) {
	for _, failure := range []string{testCancelledLabel, "authenticate", testGenerationLabel, "epoch", "unknown-member"} {
		t.Run(failure, func(t *testing.T) {
			f := newOwnershipRestoreReview(t)
			scope := core.OwnershipCallbackScope{Federation: ownershipReviewFederation, Recipient: f.current[1], FederationGeneration: f.generation, CallbackEpoch: 0}
			ctx := f.auth
			authenticate := f.sessions.Authenticate
			switch failure {
			case testCancelledLabel:
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "authenticate":
				authenticate = func(context.Context, core.FederationName, core.FederateHandle) error {
					return errors.New("authentication refused")
				}
			case testGenerationLabel:
				scope.FederationGeneration++
			case "epoch":
				scope.CallbackEpoch = 1
			case "unknown-member":
				scope.Recipient = f.saved[1]
			}
			if release, err := newOwnershipScopeAdmission(f.members, f.outbox, f.save, authenticate)(ctx, scope); err == nil || release != nil {
				t.Fatalf("bad scope admitted: %v", err)
			}
			probe := make(chan error, 1)
			go func() {
				checkpoint := f.save.AcquireCheckpoint(ownershipReviewFederation)
				defer checkpoint()
				probe <- f.members.ResignFederation(context.Background(), ownershipReviewFederation, f.current[1], core.ResignActionNoAction)
			}()
			select {
			case err := <-probe:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("failed admission leaked checkpoint/membership lease")
			}
		})
	}
}
