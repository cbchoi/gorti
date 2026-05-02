// Package model holds the immutable FOM data structures produced by the
// parser. Values are constructed once and read many times; deterministic
// name-sorted iteration is a project requirement (see docs/CODING_CONVENTIONS.md
// D-2). Fields are exported but immutability is by convention — constructors
// take all fields and there are no setters.
package model

import "sort"

// FOM is the root immutable model produced by parsing one or more FOM
// modules. Construct via NewFOM; do not assign fields directly outside this
// package.
type FOM struct {
	objectClasses      []ObjectClass
	interactionClasses []InteractionClass
	dataTypes          []DataType
}

// NewFOM returns a FOM whose internal slices are independent copies of the
// caller's inputs, sorted by name (stable). Passing nil for any argument is
// equivalent to passing an empty slice.
func NewFOM(objectClasses []ObjectClass, interactionClasses []InteractionClass, dataTypes []DataType) *FOM {
	oc := append([]ObjectClass(nil), objectClasses...)
	ic := append([]InteractionClass(nil), interactionClasses...)
	dt := append([]DataType(nil), dataTypes...)

	sort.SliceStable(oc, func(i, j int) bool { return oc[i].Name < oc[j].Name })
	sort.SliceStable(ic, func(i, j int) bool { return ic[i].Name < ic[j].Name })
	sort.SliceStable(dt, func(i, j int) bool { return dt[i].Name() < dt[j].Name() })

	return &FOM{
		objectClasses:      oc,
		interactionClasses: ic,
		dataTypes:          dt,
	}
}

// ObjectClasses returns a defensive copy of the FOM's object classes in
// name-sorted order.
func (f *FOM) ObjectClasses() []ObjectClass {
	return append([]ObjectClass(nil), f.objectClasses...)
}

// InteractionClasses returns a defensive copy of the FOM's interaction
// classes in name-sorted order.
func (f *FOM) InteractionClasses() []InteractionClass {
	return append([]InteractionClass(nil), f.interactionClasses...)
}

// DataTypes returns a defensive copy of the FOM's data types in name-sorted
// order. The returned slice elements are the same pointers held by the FOM;
// DataType implementations are expected to be immutable after construction.
func (f *FOM) DataTypes() []DataType {
	return append([]DataType(nil), f.dataTypes...)
}

// ObjectClass is one node in the FOM object class tree. ParentName is empty
// for the root (HLAobjectRoot); otherwise it names the immediate parent.
// Attributes lists only the attributes declared on this class, not inherited.
type ObjectClass struct {
	Name       string
	ParentName string
	Attributes []Attribute
}

// Attribute is one attribute on an object class.
type Attribute struct {
	Name           string
	DataType       string
	UpdateType     string
	Ownership      string
	Sharing        string
	Transportation string
	Order          string
}

// InteractionClass is one node in the FOM interaction class tree.
// ParentName is empty for the root (HLAinteractionRoot).
type InteractionClass struct {
	Name           string
	ParentName     string
	Sharing        string
	Transportation string
	Order          string
	Parameters     []Parameter
}

// Parameter is one parameter on an interaction class.
type Parameter struct {
	Name     string
	DataType string
}
