package composition

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
)

func TestMIMTablePrecedenceLegacySnapshotAndAlias(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "selected"}[explicit], func(t *testing.T) {
			ctx := context.Background()
			repo := NewFOMRepository()
			var selected *core.FOMModule
			if explicit {
				selected = &core.FOMModule{Path: "original.xml", XML: mim.StandardMIMBytes()}
			}
			fom, err := os.ReadFile("../../../pkg/fom/exporter/testdata/retained.xml")
			if err != nil {
				t.Fatal(err)
			}
			h, err := repo.LoadWithMIM(ctx, []core.FOMModule{{Path: "base.xml", XML: fom}}, selected)
			if err != nil {
				t.Fatal(err)
			}
			repo.RememberFor("fed", h)
			class, ok := h.LookupObjectClass("HLAobjectRoot.HLAmanager.HLAfederation")
			if !ok {
				t.Fatal("missing MOM class")
			}
			attr, ok := h.LookupAttribute(class, "HLAAnnexSaveName")
			if !ok || attr != 10 {
				t.Fatalf("Table 6 attribute must retain original handle 10: %d, %v", attr, ok)
			}
			if legacy, ok := h.LookupAttribute(class, "HLAnextSaveName"); !ok || legacy != attr {
				t.Fatal("legacy lookup no longer resolves the same attribute")
			}
			state, err := repo.ExportState(ctx, "fed")
			if err != nil {
				t.Fatal(err)
			}
			// Reproduce the pre-fix version-1 snapshot, including its exact MIM bytes.
			for i := range state.View.ObjectClasses {
				for j := range state.View.ObjectClasses[i].Attributes {
					a := &state.View.ObjectClasses[i].Attributes[j]
					if a.Name == "HLAAnnexSaveName" {
						a.Name = "HLAnextSaveName"
					}
				}
			}
			target := NewFOMRepository()
			restored, err := target.PrepareImportState(ctx, "fed", state)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Release()
			if err := restored.Commit(); err != nil {
				t.Fatal(err)
			}
			after, err := target.FOMView(ctx, "fed")
			if err != nil {
				t.Fatal(err)
			}
			before, _ := repo.FOMView(ctx, "fed")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("legacy restore changed handles, sources, digest or revision")
			}
			fdd, err := target.CurrentFDD(ctx, "fed")
			if err != nil || !bytes.Contains(fdd, []byte("<name>HLAAnnexSaveName</name>")) || bytes.Contains(fdd, []byte("<name>HLAnextSaveName</name>")) {
				t.Fatalf("effective FDD does not follow Table 6: %v", err)
			}
		})
	}
}
