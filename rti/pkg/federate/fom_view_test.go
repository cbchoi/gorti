package federate

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

func authoritativeView(revision uint64) *rtiv1.FOMView {
	v := &rtiv1.FOMView{Revision: revision, Digest: "exact-source", Modules: []*rtiv1.FOMModule{{Xml: []byte("not locally parsed")}},
		ObjectClasses:      []*rtiv1.FOMClassBinding{{Name: "HLAobjectRoot.Z", Handle: 19, Members: []*rtiv1.FOMMemberBinding{{Name: "Own", Handle: 2}, {Name: "Inherited", Handle: 7}, {Name: "HLAprivilegeToDeleteObject", Handle: 10}}}},
		InteractionClasses: []*rtiv1.FOMClassBinding{{Name: "HLAinteractionRoot.Event", Handle: 27, Members: []*rtiv1.FOMMemberBinding{{Name: "Value", Handle: 4}}}},
		Dimensions:         []*rtiv1.FOMDimensionBinding{{Name: "Z", Handle: 9, UpperBound: 100}},
	}
	if revision > 1 {
		v.ObjectClasses = append(v.ObjectClasses, &rtiv1.FOMClassBinding{Name: "HLAobjectRoot.A", Handle: 20})
		v.ObjectClasses[0].Members = append(v.ObjectClasses[0].Members, &rtiv1.FOMMemberBinding{Name: "Added", Handle: 11})
		v.InteractionClasses[0].Members = append(v.InteractionClasses[0].Members, &rtiv1.FOMMemberBinding{Name: "Added", Handle: 5})
		v.Dimensions = append(v.Dimensions, &rtiv1.FOMDimensionBinding{Name: "A", Handle: 10, UpperBound: 200})
	}
	return v
}

type fomViewClient struct {
	rtiv1.FederationServiceClient
	get func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error)
}

func (c fomViewClient) GetFOMView(ctx context.Context, req *rtiv1.GetFOMViewRequest, _ ...grpc.CallOption) (*rtiv1.FOMView, error) {
	return c.get(ctx, req)
}

func TestAuthoritativeFOMTablesUseWireAssignmentsAndRefresh(t *testing.T) {
	first, err := tablesFromFOMView(authoritativeView(1))
	if err != nil {
		t.Fatal(err)
	}
	f := &Federate{handles: first, federationName: testFederationName, federateHandle: 3, conn: &Connection{fed: fomViewClient{get: func(_ context.Context, req *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
		if req.GetFederationName() != testFederationName || req.GetFederateHandle() != 3 {
			t.Error("missing member context")
		}
		return authoritativeView(2), nil
	}}}}
	if got, ok := f.ObjectClassHandle("Z"); !ok || got != 19 {
		t.Fatalf("authoritative class=%d,%v", got, ok)
	}
	if got, ok := f.AttributeHandle(19, "Inherited"); !ok || got != 7 {
		t.Fatalf("authoritative inherited attribute=%d,%v", got, ok)
	}
	if err := f.RefreshFOM(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.ObjectClassHandle("A"); !ok || got != 20 {
		t.Fatalf("refreshed class=%d,%v", got, ok)
	}
	if got, ok := f.AttributeHandle(19, "Added"); !ok || got != 11 {
		t.Fatalf("refreshed attribute=%d,%v", got, ok)
	}
	if got, ok := f.ParameterHandle(27, "Added"); !ok || got != 5 {
		t.Fatalf("refreshed parameter=%d,%v", got, ok)
	}
	if got, ok := first.attributeName(19, 7); !ok || got != "Inherited" {
		t.Fatalf("old reverse mapping=%s,%v", got, ok)
	}
	if got, ok := first.attributeHandle("Z", "HLAprivilegeToDelete"); !ok || got != 10 {
		t.Fatal("privilege alias missing")
	}
}

func TestAuthoritativeFOMAliasesAndInvalidBindings(t *testing.T) {
	v := authoritativeView(1)
	v.ObjectClasses = []*rtiv1.FOMClassBinding{{Name: "HLAobjectRoot.Left.Node", Handle: 1}, {Name: "HLAobjectRoot.Right.Node", Handle: 2}}
	tables, err := tablesFromFOMView(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tables.objectClassHandle("Node"); ok {
		t.Fatal("ambiguous alias accepted")
	}
	for name, want := range map[string]uint64{"Left.Node": 1, "HLAobjectRoot.Right.Node": 2} {
		if got, ok := tables.objectClassHandle(name); !ok || got != want {
			t.Fatalf("qualified alias %s=%d,%v", name, got, ok)
		}
	}
	for _, mutate := range []func(*rtiv1.FOMView){
		func(v *rtiv1.FOMView) { v.Revision = 0 },
		func(v *rtiv1.FOMView) { v.ObjectClasses[0].Handle = 0 },
		func(v *rtiv1.FOMView) {
			v.ObjectClasses = append(v.ObjectClasses, proto.Clone(v.ObjectClasses[0]).(*rtiv1.FOMClassBinding))
		},
		func(v *rtiv1.FOMView) { v.ObjectClasses[0].Members[1].Handle = 2 },
		func(v *rtiv1.FOMView) { v.ObjectClasses[0].Members[1].Name = "Own" },
		func(v *rtiv1.FOMView) {
			v.Dimensions = append(v.Dimensions, &rtiv1.FOMDimensionBinding{Name: "A", Handle: 9})
		},
	} {
		bad := authoritativeView(1)
		mutate(bad)
		if _, err := tablesFromFOMView(bad); err == nil {
			t.Fatal("invalid authoritative view accepted")
		}
	}
}

func TestAuthoritativeFOMRefreshRejectsRemappingAndIgnoresStale(t *testing.T) {
	root, _ := tablesFromFOMView(authoritativeView(1))
	newer, _ := tablesFromFOMView(authoritativeView(3))
	if err := root.install(newer); err != nil {
		t.Fatal(err)
	}
	stale, _ := tablesFromFOMView(authoritativeView(2))
	if err := root.install(stale); err != nil {
		t.Fatal(err)
	}
	if root.current().revision != 3 {
		t.Fatal("stale reply replaced newer view")
	}
	bad := authoritativeView(4)
	bad.ObjectClasses[0].Members[0].Handle = 99
	invalid, _ := tablesFromFOMView(bad)
	if err := root.install(invalid); err == nil {
		t.Fatal("remapped member accepted")
	}
	if root.current() != newer {
		t.Fatal("invalid refresh changed mapping")
	}
}

func TestAuthoritativeFOMConcurrentRefreshAndLookups(t *testing.T) {
	root, _ := tablesFromFOMView(authoritativeView(1))
	var revision atomic.Uint64
	revision.Store(1)
	f := &Federate{handles: root, conn: &Connection{fed: fomViewClient{get: func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
		return authoritativeView(revision.Add(1)), nil
	}}}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 30; n++ {
				if err := f.RefreshFOM(context.Background()); err != nil {
					t.Error(err)
				}
				if h, ok := f.ObjectClassHandle("Z"); !ok || h != 19 {
					t.Error("unstable old class")
				}
				if h, ok := f.AttributeHandle(19, "Inherited"); !ok || h != 7 {
					t.Error("unstable old member")
				}
				if h, ok := f.ObjectClassHandle("A"); !ok || h != 20 {
					t.Error("new class missing after refresh")
				}
			}
		}()
	}
	wg.Wait()
	if f.FOMRevision() != revision.Load() {
		t.Fatal("latest revision lost")
	}
}

func TestAuthoritativeFOMRefreshOnMissAndNilTables(t *testing.T) {
	var nilTables *fomTables
	if _, ok := nilTables.objectClassHandle("missing"); ok {
		t.Fatal("nil tables resolved class")
	}
	root, _ := tablesFromFOMView(authoritativeView(1))
	var calls atomic.Int32
	f := &Federate{handles: root, conn: &Connection{fed: fomViewClient{get: func(ctx context.Context, _ *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("automatic refresh has no deadline")
		}
		calls.Add(1)
		return authoritativeView(2), nil
	}}}}
	f.bindFOMRefresh()
	if h, ok := f.ObjectClassHandle("A"); !ok || h != 20 {
		t.Fatal("miss did not refresh authoritative mapping")
	}
	if h, ok := f.DimensionHandle("A"); !ok || h != 10 {
		t.Fatal("new dimension missing")
	}
	if name, ok := f.DimensionName(9); !ok || name != "Z" {
		t.Fatal("old dimension moved")
	}
	if bound, ok := f.DimensionUpperBound(10); !ok || bound != 200 {
		t.Fatal("new dimension upper bound missing")
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls=%d", calls.Load())
	}
}

func TestAuthoritativeFOMRestoreRejectsInflightPreRestoreRefresh(t *testing.T) {
	root, _ := tablesFromFOMView(authoritativeView(3))
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	f := &Federate{handles: root, conn: &Connection{fed: fomViewClient{get: func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return authoritativeView(4), nil
		}
		return authoritativeView(1), nil
	}}}}
	done := make(chan error, 1)
	go func() { done <- f.RefreshFOM(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not enter")
	}
	if err := f.restoreFOMRefresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("pre-restore response was not rejected")
	}
	if f.FOMRevision() != 1 {
		t.Fatal("restore did not permit lower revision")
	}
	if _, ok := f.ObjectClassHandle("A"); ok {
		t.Fatal("restored mapping retained removed class")
	}
	if got, ok := f.AttributeHandle(19, "Inherited"); !ok || got != 7 {
		t.Fatal("restored member unavailable")
	}
}
