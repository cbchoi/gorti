package composition

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestSubscriptionFOMReferenceFollowsCommittedRevisionAndRecreation(t *testing.T) {
	ctx := context.Background()
	repo := NewFOMRepository()
	base, err := repo.Load(ctx, []core.FOMModule{{Path: "base.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("fed", base)
	ref := repo.FOMHandleReference("fed")
	if ref.CurrentFOMHandle() != base {
		t.Fatal("initial reference does not identify current handle")
	}
	prep, err := repo.PrepareLiveExtension(ctx, "fed", []core.FOMModule{{Path: "extension.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Added</name></objectClass></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	defer prep.Release()
	if err := prep.PrepareCommit(); err != nil {
		t.Fatal(err)
	}
	if ref.CurrentFOMHandle() != base {
		t.Fatal("uncommitted revision leaked to cache reference")
	}
	if err := prep.Commit(); err != nil {
		t.Fatal(err)
	}
	current, err := repo.Get(ctx, "fed")
	if err != nil || current != prep.FOM() || ref.CurrentFOMHandle() != current {
		t.Fatal("reference stale after committed extension", err)
	}
	if current.(interface{ FOMRevision() uint64 }).FOMRevision() != 2 {
		t.Fatal("reference does not identify revision 2")
	}
	repo.ForgetFor("fed")
	if ref.CurrentFOMHandle() != nil {
		t.Fatal("reference retained destroyed federation")
	}
	repo.RememberFor("fed", base)
	if ref.CurrentFOMHandle() != base {
		t.Fatal("reference stale after new generation/rebind")
	}
}
