package ownership_test

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func ownershipQueryRequest(t *testing.T, version rtiv1.WireVersion, fed string, caller, obj, attr uint64) *rtiv1.QueryOwnershipRequest {
	t.Helper()
	req := &rtiv1.QueryOwnershipRequest{WireVersion: version, FederationName: fed, ObjectHandle: obj, AttributeHandle: attr}
	field := req.ProtoReflect().Descriptor().Fields().ByName("federate_handle")
	if field == nil || field.Number() != 5 || field.Kind() != protoreflect.Uint64Kind {
		t.Fatal("QueryOwnershipRequest requires uint64 federate_handle = 5 for caller-scoped validation")
	}
	req.ProtoReflect().Set(field, protoreflect.ValueOfUint64(caller))
	return req
}

// IEEE's 2010 RTIambassador.h, sections 7.17 and 7.19, declares
// AttributeNotDefined and ObjectInstanceNotKnown for both query methods.
// Each failure below is isolated: the declarations do not specify precedence.
func TestOwnershipQueryPreconditions(t *testing.T) {
	for _, method := range []string{"query", "is-owned"} {
		for _, scenario := range []string{"invalid-object", unknownObjectName, "unknown-object-with-owner-entry", "unseen-object", "deleted-object", "invalid-attribute", undefinedAttributeName, "undefined-attribute-with-owner-entry", "attribute-of-other-class", "invalid-member", "unknown-member", "wrong-federation", "wire-version"} {
			t.Run(method+"/"+scenario, func(t *testing.T) {
				f := setup(t)
				ctx := context.Background()
				fed, caller, obj, attr, version := "fed", uint64(2), uint64(f.obj), uint64(f.y), rtiv1.WireVersion(1)
				want, code := objectNotKnownErrorName, codes.InvalidArgument
				switch scenario {
				case "invalid-object":
					obj = 0
				case unknownObjectName:
					obj += 1000
				case "unknown-object-with-owner-entry":
					obj += 1000
					f.own.RegisterInitialOwnership("fed", 2, core.ObjectHandle(obj), []core.AttributeHandle{f.y})
				case "unseen-object":
					caller = 3 // Published, but neither registered nor discovered this object.
				case "deleted-object":
					must(t, f.reg.Delete(ctx, "fed", 1, f.obj, nil, nil))
				case "invalid-attribute":
					attr, want, code = 0, attributeNotDefinedErrorName, codes.NotFound
				case undefinedAttributeName:
					attr, want, code = attr+1000, attributeNotDefinedErrorName, codes.NotFound
				case "undefined-attribute-with-owner-entry":
					attr, want, code = attr+1000, attributeNotDefinedErrorName, codes.NotFound
					f.own.RegisterInitialOwnership("fed", 2, f.obj, []core.AttributeHandle{core.AttributeHandle(attr)})
				case "attribute-of-other-class":
					fom := composition.NewModelHandle(model.NewFOM([]model.ObjectClass{
						{Name: "Root", Attributes: []model.Attribute{{Name: "X"}}},
						{Name: "Vehicle", ParentName: "Root", Attributes: []model.Attribute{{Name: "Y"}}},
					}, nil, nil))
					root, ok := fom.LookupObjectClass("Root")
					if !ok {
						t.Fatal("missing root class")
					}
					rootX, ok := fom.LookupAttribute(root, "X")
					if !ok {
						t.Fatal("missing root attribute")
					}
					must(t, f.decl.PublishObjectClassAttributes(ctx, "fed", 1, root, []core.AttributeHandle{rootX}))
					rootObj, _, err := f.reg.Register(ctx, "fed", 1, root, "")
					must(t, err)
					// Handles are class-relative: Vehicle's inherited X is outside Root's range.
					attr = uint64(f.x)
					obj, caller, want, code = uint64(rootObj), 1, attributeNotDefinedErrorName, codes.NotFound
				case "invalid-member":
					caller, want, code = 0, notExecutionMemberErrorName, codes.FailedPrecondition
				case "unknown-member":
					caller, want, code = 999, notExecutionMemberErrorName, codes.FailedPrecondition
				case "wrong-federation":
					fed, want, code = "other", notExecutionMemberErrorName, codes.FailedPrecondition
				case "wire-version":
					version, want, code = 999, "", codes.FailedPrecondition
				}
				before, callbacks, objects := f.own.ExportState("fed"), f.out.snapshot(), f.reg.Snapshot("fed")
				var trailer metadata.MD
				var err error
				if method == "query" {
					_, err = f.client.QueryAttributeOwnership(ctx, ownershipQueryRequest(t, version, fed, caller, obj, attr), grpc.Trailer(&trailer))
				} else {
					_, err = f.client.IsAttributeOwnedByFederate(ctx, &rtiv1.IsOwnedRequest{WireVersion: version, FederationName: fed, FederateHandle: caller, ObjectHandle: obj, AttributeHandle: attr}, grpc.Trailer(&trailer))
				}
				if status.Code(err) != code {
					t.Errorf("status = %v (%v), want %v", status.Code(err), err, code)
				}
				var wantTrailer []string
				if want != "" {
					wantTrailer = []string{want}
				}
				if !reflect.DeepEqual(trailer.Get("rti-spec-exception"), wantTrailer) {
					t.Errorf("exception = %v, want %v", trailer.Get("rti-spec-exception"), wantTrailer)
				}
				if !reflect.DeepEqual(before, f.own.ExportState("fed")) || !reflect.DeepEqual(callbacks, f.out.snapshot()) || objects != f.reg.Snapshot("fed") {
					t.Error("rejected query changed state or emitted callbacks")
				}
			})
		}
	}
}

func assertOwnershipQueries(t *testing.T, f fixture, attr core.AttributeHandle, owner core.FederateHandle) {
	t.Helper()
	before, callbacks, objects := f.own.ExportState("fed"), f.out.snapshot(), f.reg.Snapshot("fed")
	for _, caller := range []uint64{1, 2} {
		var trailer metadata.MD
		got, err := f.client.QueryAttributeOwnership(context.Background(), ownershipQueryRequest(t, 1, "fed", caller, uint64(f.obj), uint64(attr)), grpc.Trailer(&trailer))
		must(t, err)
		if got.GetOwned() != (owner != 0) || got.GetOwnerFederateHandle() != uint64(owner) || len(trailer.Get("rti-spec-exception")) != 0 {
			t.Fatalf("caller %d query = %v, exception = %v; want owner %d", caller, got, trailer, owner)
		}
		trailer = nil
		owned, err := f.client.IsAttributeOwnedByFederate(context.Background(), &rtiv1.IsOwnedRequest{WireVersion: 1, FederationName: "fed", FederateHandle: caller, ObjectHandle: uint64(f.obj), AttributeHandle: uint64(attr)}, grpc.Trailer(&trailer))
		must(t, err)
		if owned.GetOwned() != (owner != 0 && caller == uint64(owner)) || len(trailer.Get("rti-spec-exception")) != 0 {
			t.Fatalf("caller %d is-owned = %v, exception = %v; want owner %d", caller, owned, trailer, owner)
		}
	}
	if !reflect.DeepEqual(before, f.own.ExportState("fed")) || !reflect.DeepEqual(callbacks, f.out.snapshot()) || objects != f.reg.Snapshot("fed") {
		t.Fatal("successful query changed state or emitted callbacks")
	}
}

func TestOwnershipQueryUnownedAndTransferredInheritedAttribute(t *testing.T) {
	f := setup(t)
	assertOwnershipQueries(t, f, f.x, 0)
	assertOwnershipQueries(t, f, f.y, 1)
	_, err := f.client.AttributeOwnershipAcquisition(context.Background(), &rtiv1.AcquireRequest{WireVersion: 1, FederationName: "fed", FederateHandle: 2, ObjectHandle: uint64(f.obj), AttributeHandles: []uint64{uint64(f.x)}, IfAvailable: true})
	must(t, err)
	assertOwnershipQueries(t, f, f.x, 2)
	assertOwnershipQueries(t, f, f.y, 1)
	must(t, f.own.UnconditionalDivest(context.Background(), "fed", 2, f.obj, []core.AttributeHandle{f.x}))
	assertOwnershipQueries(t, f, f.x, 0)
}

func TestOwnershipQueryDoesNotRequirePublicationOrCurrentSubscription(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, f.decl.UnpublishObjectClassAttributes(ctx, "fed", 2, f.cls, nil))
	must(t, f.decl.UnsubscribeObjectClassAttributes(ctx, "fed", 2, f.cls, nil))
	assertOwnershipQueries(t, f, f.x, 0)
	assertOwnershipQueries(t, f, f.y, 1)
}

func TestOwnershipQueryDefinedAttributeNeverOwned(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, f.decl.UnpublishObjectClassAttributes(ctx, "fed", 1, f.cls, []core.AttributeHandle{f.x}))
	obj, _, err := f.reg.Register(ctx, "fed", 1, f.cls, "")
	must(t, err)
	f.obj = obj
	assertOwnershipQueries(t, f, f.x, 0)
	assertOwnershipQueries(t, f, f.y, 1)
}

func TestOwnershipQueryPreservesPendingTransfer(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	must(t, f.own.NegotiatedDivestTwoPhase(ctx, "fed", 1, f.obj, []core.AttributeHandle{f.y}, []byte("pending-divest")))
	must(t, f.own.Acquire(ctx, "fed", 2, f.obj, []core.AttributeHandle{f.y}, []byte("pending-acquire")))
	if state := f.own.ExportState("fed"); len(state.PendingAcquires) != 1 || len(state.PendingDivests) != 1 {
		t.Fatalf("transfer not pending: %+v", state)
	}
	assertOwnershipQueries(t, f, f.y, 1)
}

func TestOwnershipQueryDoesNotAppendOwnershipJournal(t *testing.T) {
	var appends atomic.Int64
	journal := &journalHook{append: func() { appends.Add(1) }}
	f := setupDeclarations(t, func(m *declaration.Manager) core.DeclarationManagement { return m }, journal)
	before := appends.Load()
	assertOwnershipQueries(t, f, f.x, 0)
	assertOwnershipQueries(t, f, f.y, 1)
	if got := appends.Load(); got != before {
		t.Fatalf("query appended ownership journal: before=%d after=%d", before, got)
	}
}

type blockedOwnershipSnapshot struct {
	*ownership.Manager
	entered chan struct{}
	release chan struct{}
}

func (b *blockedOwnershipSnapshot) QueryOwnership(fed core.FederationName, obj core.ObjectHandle, attr core.AttributeHandle) (core.FederateHandle, bool) {
	close(b.entered)
	<-b.release
	return b.Manager.QueryOwnership(fed, obj, attr)
}

func TestOwnershipQueryPinsObjectThroughOwnerSnapshot(t *testing.T) {
	ctx := context.Background()
	out := &recorder{}
	own, err := ownership.New(ownership.Options{Outbox: out})
	must(t, err)
	gate := &blockedOwnershipSnapshot{Manager: own, entered: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(gate.release) })
	t.Cleanup(release)
	fom := composition.NewModelHandle(model.NewFOM([]model.ObjectClass{{Name: "Vehicle", Attributes: []model.Attribute{{Name: "X"}}}}, nil, nil))
	cls, _ := fom.LookupObjectClass("Vehicle")
	attr, _ := fom.LookupAttribute(cls, "X")
	decl := declaration.New()
	must(t, decl.PublishObjectClassAttributes(ctx, "fed", 1, cls, []core.AttributeHandle{attr}))
	reg, err := object.New(object.Options{
		Declarations: decl, Outbox: out, FOMs: repository{fom}, Ownership: gate,
		Clock: core.NewManualClock(time.Unix(0, 0)),
		OnRegister: func(fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			own.RegisterInitialOwnership(fed, caller, obj, attrs)
		}, OnDelete: own.OnObjectDeleted,
	})
	must(t, err)
	obj, _, err := reg.Register(ctx, "fed", 1, cls, "")
	must(t, err)
	type result struct {
		owner core.FederateHandle
		owned bool
		err   error
	}
	queried := make(chan result, 1)
	go func() {
		owner, owned, err := reg.QueryAttributeOwnership(ctx, "fed", 1, obj, attr)
		queried <- result{owner, owned, err}
	}()
	select {
	case <-gate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("query did not reach ownership snapshot")
	}
	deleting, deleted := make(chan struct{}), make(chan error, 1)
	go func() {
		close(deleting)
		deleted <- reg.Delete(ctx, "fed", 1, obj, nil, nil)
	}()
	<-deleting
	select {
	case err := <-deleted:
		t.Fatalf("delete escaped the pinned ownership query: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case got := <-queried:
		if got.err != nil || got.owner != 1 || !got.owned {
			t.Fatalf("pinned query = %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("query did not release object lock")
	}
	select {
	case err := <-deleted:
		must(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("delete did not complete after query")
	}
	if _, _, err := reg.QueryAttributeOwnership(ctx, "fed", 1, obj, attr); err != core.ErrObjectHandleInvalid {
		t.Fatalf("deleted object query error = %v", err)
	}
}

const notExecutionMemberErrorName = "FederateNotExecutionMember"
