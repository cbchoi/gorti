package composition

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestFOMRepositoryEmptyModuleExtensionIncludesStandardMIM(t *testing.T) {
	for _, modules := range [][]core.FOMModule{nil, {}} {
		repo := NewFOMRepository()
		h, err := repo.Load(context.Background(), modules)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := h.LookupObjectClass("HLAobjectRoot.HLAmanager.HLAfederate"); !ok {
			t.Fatal("empty-module extension omitted the standard MIM federate class")
		}
		if _, ok := h.LookupInteractionClass("HLAinteractionRoot.HLAmanager.HLAfederate.HLArequest.HLArequestPublications"); !ok {
			t.Fatal("empty-module extension omitted the standard MIM publications request")
		}
		repo.RememberFor("empty-module-extension", h)
		if digest, ok := repo.DigestFor("empty-module-extension"); !ok || digest != DigestFOMModules(modules) {
			t.Fatal("default MIM merge changed the historical source digest")
		}
	}
}
