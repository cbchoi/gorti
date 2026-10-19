package mim_test

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

func federationAttributes(t *testing.T, f *model.FOM) []model.Attribute {
	t.Helper()
	for _, c := range f.ObjectClasses() {
		if c.Key() == "HLAobjectRoot.HLAmanager.HLAfederation" {
			return c.Attributes
		}
	}
	t.Fatal("MOM federation class missing")
	return nil
}

func TestMIMTablePrecedencePreservesSourceAndAttributePosition(t *testing.T) {
	source := mim.StandardMIMBytes()
	if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != "839f092b4b3e02a1363ec4463efcb1aae7fbfdfb17778b159065f277a3f2b023" {
		t.Fatalf("IEEE source bytes changed: %s", got)
	}
	base, err := mim.StandardMIMHandle()
	if err != nil {
		t.Fatal(err)
	}
	attrs := federationAttributes(t, base)
	if len(attrs) != 12 || attrs[9].Name != "HLAAnnexSaveName" || attrs[9].DataType != "HLAunicodeString" || attrs[10].Name != "HLAnextSaveTime" {
		t.Fatalf("Table 6 name, type or ordinal changed: %+v", attrs)
	}
	for _, name := range []string{"HLAnextSaveName", "HLAAnnexSaveName"} {
		t.Run(name, func(t *testing.T) {
			input := []byte(strings.Replace(string(source), "<name>HLAnextSaveName</name>", "<name>"+name+"</name>", 1))
			selected, err := mim.ParseSelected(input)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(attrs, federationAttributes(t, selected)) {
				t.Fatal("selected MIM changes the effective attribute declarations")
			}
			parsed, err := parser.Parse([]parser.Module{{Path: "MIM.xml", XML: input}})
			if err != nil || len(parsed.Diagnostics) != 0 {
				t.Fatalf("parse MIM: %v, %+v", err, parsed.Diagnostics)
			}
			if !reflect.DeepEqual(attrs, federationAttributes(t, parsed.FOM.(*model.FOM))) {
				t.Fatal("public parser and runtime MIM disagree")
			}
		})
	}
}

func TestMIMTablePrecedenceStillRejectsOtherServiceChanges(t *testing.T) {
	for _, name := range []string{"HLAwrongSaveName", "HLAAnnexSaveName "} {
		input := strings.Replace(string(mim.StandardMIMBytes()), "<name>HLAnextSaveName</name>", "<name>"+name+"</name>", 1)
		if _, err := mim.ParseSelected([]byte(input)); err == nil {
			t.Fatalf("unsupported declaration %q accepted", name)
		}
	}
}
