package parser

import (
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

func TestFMContributions20261004FirstDefinitionsOnly(t *testing.T) {
	baseline := model.NewFOM([]model.ObjectClass{{Name: objectClassRootName}}, []model.InteractionClass{{Name: interactionClassRootName}}, nil)
	module := func(path, content string) Module {
		return Module{Path: path, XML: []byte("<objectModel>" + content + "</objectModel>")}
	}
	modules := []Module{
		module("empty.xml", ""),
		module("roots.xml", "<objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>"),
		module("object.xml", "<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Item</name></objectClass></objectClass></objects>"),
		module("interaction.xml", "<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Pulse</name></interactionClass></interactionClass></interactions>"),
		module("dimension.xml", "<dimensions><dimension><name>X</name><upperBound>100</upperBound></dimension></dimensions>"),
		module("transport.xml", "<transportations><transportation><name>Custom</name><reliable>Yes</reliable></transportation></transportations>"),
		module("rate.xml", "<updateRates><updateRate><name>Fast</name><rate>100</rate></updateRate></updateRates>"),
		module("dimension-again.xml", "<dimensions><dimension><name>X</name><upperBound>0100</upperBound></dimension></dimensions>"),
		module("switch.xml", "<switches><autoProvide><enabled>true</enabled></autoProvide></switches>"),
	}
	modules = append(modules, Module{Path: "duplicate.xml", XML: append([]byte(nil), modules[2].XML...)})
	before := make([]Module, len(modules))
	for i, m := range modules {
		before[i] = Module{Path: m.Path, XML: append([]byte(nil), m.XML...)}
	}
	got, err := ContributingModuleIndices(modules, baseline)
	if err != nil || !reflect.DeepEqual(got, []int{2, 3, 4, 5, 6}) {
		t.Fatalf("contributions=%v err=%v", got, err)
	}
	if !reflect.DeepEqual(before, modules) {
		t.Fatal("provenance accounting changed supplied paths or bytes")
	}
	complete, err := Parse(modules[2:7])
	if err != nil || len(complete.Diagnostics) != 0 {
		t.Fatalf("fixture definitions did not parse: %+v %v", complete.Diagnostics, err)
	}
	got, err = ContributingModuleIndices(modules[2:7], complete.FOM.(*model.FOM))
	if err != nil || len(got) != 0 {
		t.Fatalf("baseline declarations counted again: %v %v", got, err)
	}
}

func TestFMContributions20261004MalformedSourceFailsClosed(t *testing.T) {
	got, err := ContributingModuleIndices([]Module{{Path: "broken.xml", XML: []byte("<objectModel>")}}, nil)
	if err == nil || got != nil {
		t.Fatalf("unavailable source was reported as an empty valid list: %v %v", got, err)
	}
}
