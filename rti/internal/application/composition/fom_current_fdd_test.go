package composition

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/exporter"
)

type currentFDDProvider interface {
	CurrentFDD(context.Context, core.FederationName) ([]byte, error)
}

func currentFDDFixture(t *testing.T) (*Repository, *fomHandle) {
	t.Helper()
	data, err := os.ReadFile("../../../pkg/fom/exporter/testdata/retained.xml")
	if err != nil {
		t.Fatal(err)
	}
	r := NewFOMRepository()
	h, err := r.Load(context.Background(), []core.FOMModule{{Path: "not-a-readable-source-file.xml", XML: data}})
	if err != nil {
		t.Fatal(err)
	}
	r.RememberFor("fdd", h)
	return r, h.(*fomHandle)
}

func currentFDD(t *testing.T, r *Repository) []byte {
	t.Helper()
	data, err := r.CurrentFDD(context.Background(), "fdd")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCurrentFDDCommittedCreateAdditiveJoinAndRestore(t *testing.T) {
	r, handle := currentFDDFixture(t)
	ctx := context.Background()
	created := currentFDD(t, r)
	if bytes.Count(created, []byte("<objectModel")) != 1 || !bytes.Contains(created, []byte("<name>HLAfederation</name>")) {
		t.Fatal("create did not export a single merged MIM/FOM")
	}
	snapshot, err := r.ExportState(ctx, "fdd")
	if err != nil {
		t.Fatal(err)
	}
	extra := core.FOMModule{Path: "join.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Parent</name><objectClass><name>JoinedChild</name><sharing>PublishSubscribe</sharing><attribute><name>AddedByJoin</name><dataType>HLAinteger32BE</dataType><transportation>HLAbestEffort</transportation><order>Receive</order></attribute></objectClass></objectClass></objectClass></objects></objectModel>`)}
	prepared, err := r.PrepareLiveExtension(ctx, "fdd", []core.FOMModule{extra})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	assertStableBindings(t, handle.View(), prepared.FOM().(*fomHandle))
	parent, _ := handle.LookupObjectClass("Parent")
	if _, ok := prepared.FOM().LookupAttribute(parent, "AddedByJoin"); ok {
		t.Fatal("new subclass attribute was added to its existing parent")
	}
	child, ok := prepared.FOM().LookupObjectClass("Parent.JoinedChild")
	if !ok {
		t.Fatal("new subclass missing")
	}
	if _, ok := prepared.FOM().LookupAttribute(child, "AddedByJoin"); !ok {
		t.Fatal("new subclass attribute missing")
	}
	if !bytes.Equal(created, currentFDD(t, r)) {
		t.Fatal("uncommitted join candidate leaked")
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	joined := currentFDD(t, r)
	if bytes.Equal(created, joined) || bytes.Count(joined, []byte("<name>AddedByJoin</name>")) != 1 {
		t.Fatal("committed additive model missing or flattened")
	}
	restored, err := r.PrepareImportState(ctx, "fdd", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Release()
	if !bytes.Equal(joined, currentFDD(t, r)) {
		t.Fatal("prepared restore leaked")
	}
	if err := restored.Commit(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(created, currentFDD(t, r)) {
		t.Fatal("restore did not return to saved authoritative model")
	}
	// A model-only handle cannot supply source XML. Export must still work.
	r.RememberFor("fdd", NewModelHandle(handle.fom))
	if !bytes.Equal(created, currentFDD(t, r)) {
		t.Fatal("export depends on source modules or handle assignments")
	}
	created[0] = '!'
	if currentFDD(t, r)[0] != '<' {
		t.Fatal("caller modified future provider results")
	}
}

func TestCurrentFDDErrorsAndCanceledContext(t *testing.T) {
	r, _ := currentFDDFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if data, err := r.CurrentFDD(ctx, "fdd"); data != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %q %v", data, err)
	}
	if data, err := r.CurrentFDD(context.Background(), "unknown"); data != nil || !errors.Is(err, core.ErrFederationNotFound) {
		t.Fatalf("missing: %q %v", data, err)
	}
	before := currentFDD(t, r)
	bad := core.FOMModule{XML: []byte(`<objectModel><switches><autoProvide isEnabled="false"/></switches></objectModel>`)}
	if _, err := r.PrepareLiveExtension(context.Background(), "fdd", []core.FOMModule{bad}); err == nil {
		t.Fatal("conflicting switch accepted")
	}
	if !bytes.Equal(before, currentFDD(t, r)) {
		t.Fatal("failed extension changed FDD")
	}
	h, err := r.Load(context.Background(), []core.FOMModule{{XML: []byte(`<objectModel/>`)}})
	if err != nil {
		t.Fatal(err)
	}
	r.RememberFor("fdd", h)
	if data, err := r.CurrentFDD(context.Background(), "fdd"); data != nil || !errors.Is(err, exporter.ErrUnsupported) || !strings.Contains(err.Error(), "switch") {
		t.Fatalf("incomplete model invented metadata: %q %v", data, err)
	}
}

func TestCurrentFDDConcurrentCommittedSnapshots(t *testing.T) {
	r, h := currentFDDFixture(t)
	want := currentFDD(t, r)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				data, err := r.CurrentFDD(context.Background(), "fdd")
				if err != nil || !bytes.Equal(data, want) {
					t.Errorf("inconsistent committed view: %v", err)
					return
				}
			}
		}()
	}
	for i := 0; i < 8; i++ {
		r.RememberFor("fdd", h)
	}
	wg.Wait()
}

func TestCurrentFDDProviderAvailable(t *testing.T) {
	provider, ok := any(NewFOMRepository()).(currentFDDProvider)
	if !ok {
		t.Fatal("repository cannot export the committed merged FOM as HLAcurrentFDD")
	}
	if data, err := provider.CurrentFDD(context.Background(), "missing"); err == nil || data != nil {
		t.Fatalf("missing federation returned data=%q error=%v", data, err)
	}
}
