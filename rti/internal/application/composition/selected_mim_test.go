package composition

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
)

func TestIEEE2010SelectedMIMReservedDesignator(t *testing.T) {
	repo := NewFOMRepository()
	modules := []core.FOMModule{{Path: "fom.xml", XML: []byte(`<objectModel/>`)}}
	for _, designator := range []string{"HLAstandardMIM", "HLAstandardMIM.xml", "custom.xml"} {
		t.Run(designator, func(t *testing.T) {
			_, err := repo.LoadWithMIM(context.Background(), modules, &core.FOMModule{Path: designator, XML: mim.StandardMIMBytes()})
			if designator == "HLAstandardMIM" {
				if err == nil || !strings.Contains(err.Error(), "HLAstandardMIM") {
					t.Fatalf("reserved designator accepted or misdiagnosed: %v", err)
				}
			} else if err != nil {
				t.Fatalf("nonreserved designator rejected: %v", err)
			}
		})
	}
}

func TestSelectedMIMRepositoryRetentionAndRestore(t *testing.T) {
	ctx := context.Background()
	repo := NewFOMRepository()
	source := []byte(strings.Replace(string(mim.StandardMIMBytes()), "This object class is the root class of all MOM object classes", "selected MIM marker", 1))
	selected := &core.FOMModule{Path: "unreadable:/chosen MIM.xml", XML: append([]byte(nil), source...)}
	fomSource, err := os.ReadFile("../../../pkg/fom/exporter/testdata/retained.xml")
	if err != nil {
		t.Fatal(err)
	}
	h, err := repo.LoadWithMIM(ctx, []core.FOMModule{{Path: "retained.xml", XML: fomSource}}, selected)
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("selected", h)
	selected.XML[0] = '!'
	selected.Path = "mutated"
	before, err := repo.CurrentFDD(ctx, "selected")
	if err != nil || !bytes.Contains(before, []byte("selected MIM marker")) {
		t.Fatalf("selected FDD lost: %v", err)
	}
	view, err := repo.FOMView(ctx, "selected")
	if err != nil || view.MIMModule == nil || !bytes.Equal(view.MIMModule.XML, source) || len(view.Modules) != 1 {
		t.Fatalf("selected view: %v", err)
	}
	if path, err := repo.MIMDesignator(ctx, "selected"); err != nil || path != "unreadable:/chosen MIM.xml" {
		t.Fatalf("designator %q: %v", path, err)
	}
	state, err := repo.ExportState(ctx, "selected")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var saved core.FOMRepositoryState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	view.MIMModule.XML[0] = '!'
	additional := []core.FOMModule{{Path: "extension.xml", XML: []byte(`<objectModel><objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Later</name></objectClass></objectClass></objects></objectModel>`)}}
	prepared, err := repo.PrepareLiveExtension(ctx, "selected", additional)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	if !bytes.Equal(prepared.View().MIMModule.XML, source) {
		t.Fatal("join replaced explicit MIM")
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	repo.ForgetFor("selected")
	fresh := NewFOMRepository()
	restore, err := fresh.PrepareImportState(ctx, "selected", &saved)
	if err != nil {
		t.Fatal(err)
	}
	defer restore.Release()
	if _, err := fresh.Get(ctx, "selected"); err == nil {
		t.Fatal("restore published before commit")
	}
	if err := restore.Commit(); err != nil {
		t.Fatal(err)
	}
	after, err := fresh.CurrentFDD(ctx, "selected")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("fresh restore source differs: %v", err)
	}
	committed, _ := fresh.FOMView(ctx, "selected")
	for _, drop := range []bool{false, true} {
		var corrupt core.FOMRepositoryState
		if err := json.Unmarshal(data, &corrupt); err != nil {
			t.Fatal(err)
		}
		if drop {
			corrupt.View.MIMModule = nil
		} else {
			corrupt.View.MIMModule.XML = bytes.ReplaceAll(corrupt.View.MIMModule.XML, []byte("selected MIM marker"), []byte("tampered MIM marker"))
		}
		if _, err := fresh.PrepareImportState(ctx, "selected", &corrupt); err == nil {
			t.Fatal("tampered/dropped MIM restored")
		}
		current, _ := fresh.FOMView(ctx, "selected")
		if !reflect.DeepEqual(committed, current) {
			t.Fatal("failed restore changed state")
		}
	}
}

func TestSelectedMIMDigestRoleAndOmissionCompatibility(t *testing.T) {
	modules := []core.FOMModule{{Path: "fom.xml", XML: []byte("abc")}}
	mimModule := &core.FOMModule{Path: "chosen.xml", XML: []byte("def")}
	if DigestFOMSources(modules, nil) != DigestFOMModules(modules) {
		t.Fatal("legacy digest changed")
	}
	if DigestFOMSources(modules, mimModule) == DigestFOMModules(append(modules, *mimModule)) {
		t.Fatal("MIM role aliases FOM role")
	}
	if DigestFOMSources(modules, mimModule) == DigestFOMSources([]core.FOMModule{*mimModule}, &modules[0]) {
		t.Fatal("role swap aliases digest")
	}
	if DigestFOMSources(modules, &core.FOMModule{}) == DigestFOMSources(modules, nil) {
		t.Fatal("empty explicit source aliases omission")
	}
	relocated := *mimModule
	relocated.Path = "other path"
	if DigestFOMSources(modules, mimModule) != DigestFOMSources(modules, &relocated) {
		t.Fatal("diagnostic path changed content identity")
	}
	repo := NewFOMRepository()
	h, err := repo.Load(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("old", h)
	if path, err := repo.MIMDesignator(context.Background(), "old"); err != nil || path != "HLAstandardMIM" {
		t.Fatalf("legacy MIM %q: %v", path, err)
	}
	if _, err := repo.LoadWithMIM(context.Background(), nil, &core.FOMModule{XML: mim.StandardMIMBytes()}); err == nil {
		t.Fatal("anonymous explicit MIM accepted")
	}
}
