package model_test

import (
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/parser"
)

func TestFOMAdvisoryMetadataNotDiscarded(t *testing.T) {
	result, err := parser.Parse([]parser.Module{{Path: "switches.xml", XML: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><switches><attributeScopeAdvisory isEnabled="true"/></switches></objectModel>`)}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("parse: %v %v", err, result.Diagnostics)
	}
	method := reflect.ValueOf(result.FOM).MethodByName("AdvisorySwitches")
	if !method.IsValid() {
		t.Fatal("parser discarded FDD advisory-switch metadata")
	}
	values := method.Call(nil)[0]
	if values.Len() != 1 || values.Index(0).FieldByName("Name").String() != "attributeScopeAdvisory" || !values.Index(0).FieldByName("Enabled").Bool() {
		t.Fatalf("switch values %v", values.Interface())
	}
}

func TestFOMAdvisoryMalformedXMLBooleanRejected(t *testing.T) {
	result, err := parser.Parse([]parser.Module{{Path: "switches.xml", XML: []byte(`<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010"><switches><attributeScopeAdvisory isEnabled="yes"/></switches></objectModel>`)}})
	if err == nil && len(result.Diagnostics) == 0 {
		t.Fatal("malformed XML boolean silently accepted")
	}
}
