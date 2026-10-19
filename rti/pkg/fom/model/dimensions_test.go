package model

import (
	"reflect"
	"testing"
)

func TestDimensionReferencesAreDefensiveCopies(t *testing.T) {
	for _, constructor := range []struct {
		name string
		new  func([]ObjectClass, []InteractionClass, []DataType) *FOM
	}{
		{"NewFOM", NewFOM},
		{"NewFOMWithDimensions", func(o []ObjectClass, i []InteractionClass, d []DataType) *FOM {
			return NewFOMWithDimensions(o, i, d, []Dimension{{Name: "X"}, {Name: "Y"}})
		}},
	} {
		t.Run(constructor.name, func(t *testing.T) {
			objects := []ObjectClass{{Name: "Parent", Attributes: []Attribute{{Name: "Position", Dimensions: []string{"Y", "X"}}}}}
			interactions := []InteractionClass{{Name: "Signal", Dimensions: []string{"Y", "X"}, Parameters: []Parameter{{Name: "Value"}}}}
			f := constructor.new(objects, interactions, nil)
			check := func() {
				t.Helper()
				a := f.ObjectClasses()[0].Attributes[0]
				i := f.InteractionClasses()[0]
				if a.Name != "Position" || !reflect.DeepEqual(a.Dimensions, []string{"Y", "X"}) {
					t.Errorf("attribute changed through an alias: %+v", a)
				}
				if !reflect.DeepEqual(i.Dimensions, []string{"Y", "X"}) || i.Parameters[0].Name != "Value" {
					t.Errorf("interaction changed through an alias: %+v", i)
				}
			}
			objects[0].Attributes[0].Dimensions[0] = testInputMutation
			objects[0].Attributes[0].Name = testInputMutation
			interactions[0].Dimensions[0] = testInputMutation
			interactions[0].Parameters[0].Name = testInputMutation
			check()
			outObjects, outInteractions := f.ObjectClasses(), f.InteractionClasses()
			outObjects[0].Attributes[0].Dimensions[0] = testOutputMutation
			outObjects[0].Attributes[0].Name = testOutputMutation
			outInteractions[0].Dimensions[0] = testOutputMutation
			outInteractions[0].Parameters[0].Name = testOutputMutation
			check()
			clone := NewFOMWithDimensions(f.ObjectClasses(), f.InteractionClasses(), f.DataTypes(), f.Dimensions())
			clone.ObjectClasses()[0].Attributes[0].Dimensions[0] = "clone mutation"
			clone.InteractionClasses()[0].Dimensions[0] = "clone mutation"
			check()
		})
	}
}

const (
	testOutputMutation  = "output mutation"
	testInputMutation   = "input mutation"
	testChangedMetadata = "changed"
	testPreciseDecimal  = "1.0000000000000000000000000001"
)
