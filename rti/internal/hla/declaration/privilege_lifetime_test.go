package declaration_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
)

const (
	privilegeExplicit   = "explicit"
	privilegeSuppressed = "suppressed"
)

func privilegeFixture(t *testing.T) (*declaration.Manager, core.FOMRepository, core.ObjectClassHandle, core.AttributeHandle, core.AttributeHandle) {
	t.Helper()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(context.Background(), []core.FOMModule{{Path: "privilege-lifetime.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Item</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("f", model)
	cls, _ := model.LookupObjectClass("Item")
	attr, _ := model.LookupAttribute(cls, "value")
	priv, ok := model.LookupAttribute(cls, "HLAprivilegeToDeleteObject")
	if !ok || cls == 0 || attr == 0 {
		t.Fatal("missing real FOM/MIM handles")
	}
	m := declaration.New()
	m.SetFOMRepository(repo)
	return m, repo, cls, attr, priv
}

func privilegeOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrivilegeLifetimePersistenceAndReset(t *testing.T) {
	for _, mode := range []string{"implicit", privilegeExplicit, privilegeSuppressed} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			m, repo, cls, attr, priv := privilegeFixture(t)
			privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
			if mode == privilegeExplicit {
				privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{priv}))
			}
			if mode == privilegeSuppressed {
				privilegeOK(t, m.UnpublishObjectClassAttributeSet(ctx, "f", 1, cls, []core.AttributeHandle{priv}))
			}
			data, err := json.Marshal(m.ExportState("f"))
			privilegeOK(t, err)
			var saved declaration.ManagerState
			privilegeOK(t, json.Unmarshal(data, &saved))
			restored := declaration.New()
			restored.SetFOMRepository(repo)
			commit, err := restored.PrepareRestore("f", &saved)
			privilegeOK(t, err)
			// Prepared state must be detached from the mutable input snapshot.
			saved.ObjectPublications = nil
			saved.ObjectPublicationHistory[0].DeletePrivilegeTouched = !saved.ObjectPublicationHistory[0].DeletePrivilegeTouched
			commit()
			if !reflect.DeepEqual(m.ExportState("f"), restored.ExportState("f")) {
				t.Fatal("history/publication round trip changed state")
			}
			privilegeOK(t, restored.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
			if got := slices.Contains(restored.PublishedObjectAttributes("f", 1, cls), priv); got != (mode != privilegeSuppressed) {
				t.Fatalf("restored %s privilege=%v", mode, got)
			}
			privilegeOK(t, restored.UnpublishObjectClassAttributeSet(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
			got := restored.PublishedObjectAttributes("f", 1, cls)
			if mode == privilegeExplicit {
				if !slices.Equal(got, []core.AttributeHandle{priv}) {
					t.Fatalf("explicit privilege lost: %v", got)
				}
				privilegeOK(t, restored.UnpublishObjectClassAttributeSet(ctx, "f", 1, cls, []core.AttributeHandle{priv}))
			} else if len(got) != 0 {
				t.Fatalf("last explicit removal retained publication %v", got)
			}
			privilegeOK(t, restored.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
			if !slices.Contains(restored.PublishedObjectAttributes("f", 1, cls), priv) {
				t.Fatal("new lifetime did not restore implicit privilege")
			}
		})
	}
}

func TestPrivilegeLifetimeResignAndClassIsolation(t *testing.T) {
	ctx := context.Background()
	m, repo, cls, attr, priv := privilegeFixture(t)
	model, err := repo.Get(ctx, "f")
	privilegeOK(t, err)
	root, _ := model.LookupObjectClass("HLAobjectRoot")
	rootPriv, _ := model.LookupAttribute(root, "HLAprivilegeToDeleteObject")
	privilegeOK(t, m.UnpublishObjectClassAttributeSet(ctx, "f", 1, cls, []core.AttributeHandle{priv}))
	privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, nil))
	if len(m.PublishedObjectAttributes("f", 1, cls)) != 0 {
		t.Fatal("empty publication established lifetime")
	}
	privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
	privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 2, cls, []core.AttributeHandle{attr}))
	privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, root, []core.AttributeHandle{rootPriv}))
	privilegeOK(t, m.UnpublishObjectClassAttributeSet(ctx, "f", 1, cls, []core.AttributeHandle{priv}))
	if !slices.Equal(m.PublishersFor(ctx, "f", cls, priv), []core.FederateHandle{2}) {
		t.Fatal("privilege publication leaked across federates")
	}
	if !slices.Equal(m.PublishedObjectAttributes("f", 1, root), []core.AttributeHandle{rootPriv}) {
		t.Fatal("child unpublication changed root")
	}
	m.OnFederateResign("f", 1)
	privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
	if !slices.Equal(m.PublishersFor(ctx, "f", cls, priv), []core.FederateHandle{1, 2}) {
		t.Fatal("resign left stale suppression history")
	}
	m.OnFederationDestroyed("f")
	privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
	if !slices.Contains(m.PublishedObjectAttributes("f", 1, cls), priv) {
		t.Fatal("destroy left stale history")
	}
}

func TestPrivilegeLifetimeFailedAdviceDoesNotCommitHistory(t *testing.T) {
	ctx := context.Background()
	m, _, cls, attr, priv := privilegeFixture(t)
	out := delivery.NewWithBatch(1, 1, 0)
	m.SetAdvisoryOutbox(out)
	queue, release, err := out.Subscribe(ctx, "f", 1)
	privilegeOK(t, err)
	defer func() { privilegeOK(t, release()) }()
	privilegeOK(t, m.SubscribeObjectClassAttributes(ctx, "f", 2, cls, []core.AttributeHandle{priv}))
	privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
	before := m.ExportState("f")
	if err := m.UnpublishObjectClassAttributeSet(ctx, "f", 1, cls, []core.AttributeHandle{priv}); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatalf("full queue rejection=%v", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("f")) {
		t.Fatal("rejected unpublication committed history or publications")
	}
	<-queue
	privilegeOK(t, m.UnpublishObjectClassAttributeSet(ctx, "f", 1, cls, []core.AttributeHandle{priv}))
	if slices.Contains(m.PublishedObjectAttributes("f", 1, cls), priv) {
		t.Fatal("successful retry failed to suppress privilege")
	}
}

func TestPrivilegeLifetimeDurableSnapshot(t *testing.T) {
	for _, mode := range []string{"implicit", privilegeExplicit, privilegeSuppressed} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			m, repo, cls, attr, priv := privilegeFixture(t)
			privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
			if mode == privilegeExplicit {
				privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{priv}))
			}
			if mode == privilegeSuppressed {
				privilegeOK(t, m.UnpublishObjectClassAttributeSet(ctx, "f", 1, cls, []core.AttributeHandle{priv}))
			}
			data, err := (savebundle.Declaration{Manager: m}).Marshal("f")
			privilegeOK(t, err)
			restored := declaration.New()
			restored.SetFOMRepository(repo)
			privilegeOK(t, (savebundle.Declaration{Manager: restored}).Unmarshal("f", data))
			if !reflect.DeepEqual(m.ExportState("f"), restored.ExportState("f")) {
				t.Fatal("durable snapshot lost publication lifetime history")
			}
			privilegeOK(t, restored.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
			if got := slices.Contains(restored.PublishedObjectAttributes("f", 1, cls), priv); got != (mode != privilegeSuppressed) {
				t.Fatalf("durable %s history changed privilege to %v", mode, got)
			}
			rebound, err := (savebundle.Declaration{}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) {
				if h != 1 {
					t.Fatalf("unexpected saved federate %d", h)
				}
				return 7, nil
			})
			privilegeOK(t, err)
			privilegeOK(t, (savebundle.Declaration{Manager: restored}).Unmarshal("f", rebound))
			if len(restored.PublishedObjectAttributes("f", 1, cls)) != 0 {
				t.Fatal("rebind retained old participant publication")
			}
			privilegeOK(t, restored.PublishObjectClassAttributes(ctx, "f", 7, cls, []core.AttributeHandle{attr}))
			if got := slices.Contains(restored.PublishedObjectAttributes("f", 7, cls), priv); got != (mode != privilegeSuppressed) {
				t.Fatalf("rebound %s history changed privilege to %v", mode, got)
			}
		})
	}
}

func TestPrivilegeLifetimeRestoreRejectsInvalidHistoryAtomically(t *testing.T) {
	m, _, cls, attr, priv := privilegeFixture(t)
	ctx := context.Background()
	privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
	before := m.ExportState("f")
	for _, mode := range []string{"duplicate", "zero-federate", "inactive-federate", "missing-implicit", "implicit-only", "missing-active-history"} {
		t.Run(mode, func(t *testing.T) {
			bad := m.ExportState("f")
			switch mode {
			case "duplicate":
				bad.ObjectPublicationHistory = append(bad.ObjectPublicationHistory, bad.ObjectPublicationHistory[0])
			case "zero-federate":
				bad.ObjectPublicationHistory[0].Federate = 0
			case "inactive-federate":
				bad.ObjectPublicationHistory[0].Federate = 2
			case "missing-implicit", "implicit-only":
				remove := priv
				if mode == "implicit-only" {
					remove = attr
				}
				bad.ObjectPublications = slices.DeleteFunc(bad.ObjectPublications, func(entry declaration.ObjectDeclarationState) bool { return entry.Attribute == remove })
			case "missing-active-history":
				for i := range bad.ObjectPublications {
					bad.ObjectPublications[i].Federates = append(bad.ObjectPublications[i].Federates, 2)
				}
			}
			if commit, err := m.PrepareRestore("f", bad); err == nil || commit != nil {
				t.Fatal("invalid history produced a restore commit")
			}
			if !reflect.DeepEqual(before, m.ExportState("f")) {
				t.Fatal("invalid restore changed live publication")
			}
		})
	}
}

func TestPrivilegeLifetimePublicationHookIncludesImplicitOnce(t *testing.T) {
	m, _, cls, attr, priv := privilegeFixture(t)
	ctx := context.Background()
	var deltas [][]core.AttributeHandle
	m.SetOnPublishObjectClass(func(_ context.Context, fed core.FederationName, pub core.FederateHandle, class core.ObjectClassHandle, attrs []core.AttributeHandle) {
		for _, a := range attrs {
			if !slices.Contains(m.PublishedObjectAttributes(fed, pub, class), a) {
				t.Fatal("hook before effective publication commit")
			}
		}
		deltas = append(deltas, slices.Clone(attrs))
	})
	privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
	want := []core.AttributeHandle{attr, priv}
	slices.Sort(want)
	if len(deltas) != 1 || !slices.Equal(deltas[0], want) {
		t.Fatalf("initial delta=%v want=%v", deltas, want)
	}
	privilegeOK(t, m.PublishObjectClassAttributes(ctx, "f", 1, cls, []core.AttributeHandle{attr, priv}))
	if len(deltas) != 1 {
		t.Fatal("explicit promotion repeated effective publication hook")
	}
	privilegeOK(t, m.UnpublishObjectClassAttributeSet(ctx, "f", 1, cls, []core.AttributeHandle{attr}))
	if !slices.Equal(m.PublishedObjectAttributes("f", 1, cls), []core.AttributeHandle{priv}) {
		t.Fatal("explicitly promoted privilege disappeared")
	}
}
