package savebundle

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func privilegeCodecFixture(t *testing.T) (*declaration.Manager, core.FOMHandle, core.ObjectClassHandle, core.AttributeHandle, core.AttributeHandle) {
	t.Helper()
	repo := composition.NewFOMRepository()
	model, err := repo.Load(context.Background(), []core.FOMModule{{Path: "privilege-codec.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Item</name><attribute><name>value</name><dataType>HLAinteger32BE</dataType></attribute></objectClass></objectClass></objects></objectModel>`)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("f", model)
	cls, _ := model.LookupObjectClass("Item")
	attr, _ := model.LookupAttribute(cls, "value")
	priv, _ := model.LookupAttribute(cls, "HLAprivilegeToDeleteObject")
	m := declaration.New()
	m.SetFOMRepository(repo)
	return m, model, cls, attr, priv
}

func privilegeHistoryGraph(t *testing.T, h core.FederateHandle, generation uint64, data []byte) core.CheckpointManifest {
	t.Helper()
	return core.CheckpointManifest(savepoint.Manifest{Federation: "f", FederationGeneration: &generation, Federates: []core.FederateHandle{h}, FederateNames: []string{"member"}, ManagerSnapshots: map[string][]byte{
		"object": nil, "ownership": nil, "declaration": data, "ddm": nil, "time": nil, "sync": nil, "mom": nil,
		"retractions": graphJSON(t, retractionSnapshot{Version: 1, History: &timepkg.RetractionHistory{Complete: true}}),
	}})
}

func TestDeclarationPrivilegeHistoryGenerationRebindAndRestore(t *testing.T) {
	const source core.FederateHandle = 1<<32 | 7
	const target core.FederateHandle = 9<<32 | 20
	for _, mode := range []string{"implicit", testExplicit, "suppressed"} {
		t.Run(mode, func(t *testing.T) {
			m, model, cls, attr, priv := privilegeCodecFixture(t)
			ctx := context.Background()
			if err := m.PublishObjectClassAttributes(ctx, "f", source, cls, []core.AttributeHandle{attr}); err != nil {
				t.Fatal(err)
			}
			if mode == testExplicit {
				if err := m.PublishObjectClassAttributes(ctx, "f", source, cls, []core.AttributeHandle{priv}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "suppressed" {
				if err := m.UnpublishObjectClassAttributeSet(ctx, "f", source, cls, []core.AttributeHandle{priv}); err != nil {
					t.Fatal(err)
				}
			}
			codec := Declaration{Manager: m}
			data, err := codec.Marshal("f")
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			if string(wire["version"]) != "4" || len(wire["object_publication_history"]) == 0 {
				t.Fatal("new snapshot does not identify complete publication history")
			}
			graph := RestoreGraph{}
			if err := graph.Validate("f", privilegeHistoryGraph(t, source, 1, data), model); err != nil {
				t.Fatal(err)
			}
			rebound, err := codec.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) {
				if h != source {
					t.Fatalf("unexpected source identity %d", h)
				}
				return target, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := graph.Validate("f", privilegeHistoryGraph(t, target, 9, rebound), model); err != nil {
				t.Fatalf("rebound graph: %v", err)
			}
			if err := graph.Validate("f", privilegeHistoryGraph(t, target, 9, data), model); err == nil {
				t.Fatal("old generation declarations accepted with rebound roster")
			}
			before := m.ExportState("f")
			commit, err := codec.PrepareRestoreWithFOM("f", rebound, model)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, m.ExportState("f")) {
				t.Fatal("restore preparation changed live history")
			}
			clear(rebound)
			commit()
			if len(m.PublishedObjectAttributes("f", source, cls)) != 0 {
				t.Fatal("old generation publication retained")
			}
			if got := m.ExportState("f").ObjectPublicationHistory; len(got) != 1 || got[0].Federate != target || got[0].Class != cls || got[0].DeletePrivilegeTouched != (mode != "implicit") {
				t.Fatalf("history identity/state lost: %+v", got)
			}
			if err := m.PublishObjectClassAttributes(ctx, "f", target, cls, []core.AttributeHandle{attr}); err != nil {
				t.Fatal(err)
			}
			if got := slices.Contains(m.PublishedObjectAttributes("f", target, cls), priv); got != (mode != "suppressed") {
				t.Fatalf("restored %s privilege=%v", mode, got)
			}
			if err := m.UnpublishObjectClassAttributeSet(ctx, "f", target, cls, []core.AttributeHandle{attr}); err != nil {
				t.Fatal(err)
			}
			if got := m.PublishedObjectAttributes("f", target, cls); mode == testExplicit && !slices.Equal(got, []core.AttributeHandle{priv}) || mode != testExplicit && len(got) != 0 {
				t.Fatalf("explicit/implicit distinction lost: %v", got)
			}
		})
	}
}

func TestDeclarationPrivilegeHistoryPriorFormatsRemainReadable(t *testing.T) {
	for version := 1; version <= 3; version++ {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			m, model, cls, attr, priv := privilegeCodecFixture(t)
			data := graphJSON(t, map[string]any{"version": version, "object_publications": []declarationObjectRecord{{Class: cls, Attribute: attr, Federates: []core.FederateHandle{1}}}})
			commit, err := (Declaration{Manager: m}).PrepareRestoreWithFOM("f", data, model)
			if err != nil {
				t.Fatal(err)
			}
			commit()
			if !slices.Contains(m.PublishedObjectAttributes("f", 1, cls), priv) {
				t.Fatal("legacy explicit-only publication did not recover implicit privilege")
			}
			if err := m.UnpublishObjectClassAttributeSet(context.Background(), "f", 1, cls, []core.AttributeHandle{attr}); err != nil {
				t.Fatal(err)
			}
			if len(m.PublishedObjectAttributes("f", 1, cls)) != 0 {
				t.Fatal("legacy implicit privilege treated as explicit")
			}
		})
	}
}

func TestDeclarationPrivilegeHistoryMalformedSnapshotHasNoCommit(t *testing.T) {
	m, model, cls, attr, priv := privilegeCodecFixture(t)
	const member core.FederateHandle = 1<<32 | 7
	if err := m.PublishObjectClassAttributes(context.Background(), "f", member, cls, []core.AttributeHandle{attr}); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("f")
	for _, mode := range []string{"missing", "empty", "null", "orphan", "duplicate", "missing-implicit", "unknown-class"} {
		t.Run(mode, func(t *testing.T) {
			pubs := []declarationObjectRecord{{Class: cls, Attribute: attr, Federates: []core.FederateHandle{member}}, {Class: cls, Attribute: priv, Federates: []core.FederateHandle{member}}}
			history := []declaration.ObjectPublicationHistoryState{{Class: cls, Federate: member}}
			wire := map[string]any{"version": 4, "object_publications": pubs, "object_publication_history": history}
			switch mode {
			case "missing":
				delete(wire, "object_publication_history")
			case "empty":
				wire["object_publication_history"] = []declaration.ObjectPublicationHistoryState{}
			case "null":
				wire["object_publication_history"] = nil
			case "orphan":
				history[0].Federate++
			case "duplicate":
				wire["object_publication_history"] = append(history, history[0])
			case "missing-implicit":
				wire["object_publications"] = pubs[:1]
			case "unknown-class":
				history[0].Class = ^core.ObjectClassHandle(0)
			}
			data := graphJSON(t, wire)
			if commit, err := (Declaration{Manager: m}).PrepareRestoreWithFOM("f", data, model); err == nil || commit != nil {
				t.Fatal("malformed history prepared a commit")
			}
			if err := (RestoreGraph{}).Validate("f", privilegeHistoryGraph(t, member, 1, data), model); err == nil {
				t.Fatal("graph accepted malformed history")
			}
			if !reflect.DeepEqual(before, m.ExportState("f")) {
				t.Fatal("malformed history changed manager")
			}
		})
	}
}

const testExplicit = "explicit"
