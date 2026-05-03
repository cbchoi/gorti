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
	dimensions         []Dimension
}

// NewFOM returns a FOM whose internal slices are independent copies of the
// caller's inputs, sorted by name (stable). Passing nil for any argument is
// equivalent to passing an empty slice.
//
// Dimensions are appended via NewFOMWithDimensions; use this constructor
// when no <dimensions> block was present in the source XML.
func NewFOM(objectClasses []ObjectClass, interactionClasses []InteractionClass, dataTypes []DataType) *FOM {
	return NewFOMWithDimensions(objectClasses, interactionClasses, dataTypes, nil)
}

// NewFOMWithDimensions is NewFOM extended with the <dimensions> block
// (M10 / FR-DDM-1). Dimensions are sorted by name (stable) for
// deterministic iteration.
func NewFOMWithDimensions(
	objectClasses []ObjectClass,
	interactionClasses []InteractionClass,
	dataTypes []DataType,
	dimensions []Dimension,
) *FOM {
	oc := append([]ObjectClass(nil), objectClasses...)
	ic := append([]InteractionClass(nil), interactionClasses...)
	dt := append([]DataType(nil), dataTypes...)
	dm := append([]Dimension(nil), dimensions...)

	sort.SliceStable(oc, func(i, j int) bool { return oc[i].Name < oc[j].Name })
	sort.SliceStable(ic, func(i, j int) bool { return ic[i].Name < ic[j].Name })
	sort.SliceStable(dt, func(i, j int) bool { return dt[i].Name() < dt[j].Name() })
	sort.SliceStable(dm, func(i, j int) bool { return dm[i].Name < dm[j].Name })

	return &FOM{
		objectClasses:      oc,
		interactionClasses: ic,
		dataTypes:          dt,
		dimensions:         dm,
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

// Dimensions returns a defensive copy of the FOM's dimensions in
// name-sorted order. M10 / FR-DDM-1: routing-space declarations.
func (f *FOM) Dimensions() []Dimension {
	return append([]Dimension(nil), f.dimensions...)
}

// Dimension is one routing-space dimension declared in the FOM
// (1516.2-2010 Annex A <dimension>). M10 / FR-DDM-1.
//
// In 1516-2010 dimensions are global (no enclosing routing-space
// element); each dimension is its own one-axis routing space. The
// optional NormalizationKey field carries the FOM's <normalization>
// hint when present (informational; the RTI does not perform
// normalization itself in cut-2).
type Dimension struct {
	Name             string
	UpperBound       uint64
	NormalizationKey string
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
