// Package model holds the immutable FOM data structures produced by the
// parser. Values are constructed once and read many times; deterministic
// name-sorted iteration is a project requirement (see docs/CODING_CONVENTIONS.md
// D-2). Fields are exported but immutability is by convention — constructors
// take all fields and there are no setters.
package model

import (
	"sort"
	"strings"
)

// FOM is the root immutable model produced by parsing one or more FOM
// modules. Construct via NewFOM; do not assign fields directly outside this
// package.
type FOM struct {
	objectClasses         []ObjectClass
	interactionClasses    []InteractionClass
	dataTypes             []DataType
	dimensions            []Dimension
	updateRates           []UpdateRate
	transportations       []Transportation
	advisorySwitches      []AdvisorySwitch
	automaticResignAction string
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
	return NewFOMWithUpdateRates(objectClasses, interactionClasses, dataTypes, dimensions, nil)
}

// NewFOMWithUpdateRates retains named update rates in addition to dimensions.
func NewFOMWithUpdateRates(objectClasses []ObjectClass, interactionClasses []InteractionClass, dataTypes []DataType, dimensions []Dimension, updateRates []UpdateRate) *FOM {
	oc := cloneObjectClasses(objectClasses)
	ic := cloneInteractionClasses(interactionClasses)
	dt := append([]DataType(nil), dataTypes...)
	dm := append([]Dimension(nil), dimensions...)
	ur := append([]UpdateRate(nil), updateRates...)

	sort.SliceStable(oc, func(i, j int) bool { return oc[i].Name < oc[j].Name })
	sort.SliceStable(ic, func(i, j int) bool { return ic[i].Name < ic[j].Name })
	sort.SliceStable(dt, func(i, j int) bool { return dt[i].Name() < dt[j].Name() })
	sort.SliceStable(dm, func(i, j int) bool { return dm[i].Name < dm[j].Name })
	sort.SliceStable(ur, func(i, j int) bool { return ur[i].Name < ur[j].Name })

	return &FOM{
		objectClasses:      oc,
		interactionClasses: ic,
		dataTypes:          dt,
		dimensions:         dm,
		updateRates:        ur,
	}
}

// ObjectClasses returns a defensive copy of the FOM's object classes in
// name-sorted order.
func (f *FOM) ObjectClasses() []ObjectClass {
	return cloneObjectClasses(f.objectClasses)
}

// InteractionClasses returns a defensive copy of the FOM's interaction
// classes in name-sorted order.
func (f *FOM) InteractionClasses() []InteractionClass {
	return cloneInteractionClasses(f.interactionClasses)
}

func cloneObjectClasses(in []ObjectClass) []ObjectClass {
	out := append([]ObjectClass(nil), in...)
	for i := range out {
		out[i].Attributes = append([]Attribute(nil), in[i].Attributes...)
		for j := range out[i].Attributes {
			out[i].Attributes[j].Dimensions = append([]string(nil), in[i].Attributes[j].Dimensions...)
		}
	}
	return out
}

func cloneInteractionClasses(in []InteractionClass) []InteractionClass {
	out := append([]InteractionClass(nil), in...)
	for i := range out {
		out[i].Parameters = append([]Parameter(nil), in[i].Parameters...)
		out[i].Dimensions = append([]string(nil), in[i].Dimensions...)
	}
	return out
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
	DataType         string
	UpperBound       uint64
	NormalizationKey string
	// DefaultValue is the optional FDD <value> declaration, not an inferred
	// runtime range. Empty means absent; an explicitly empty value is invalid.
	// Legacy NA is retained by parsers but is not a schema-valid export value.
	DefaultValue string
}

// ObjectClass is one node in the FOM object class tree. ParentName is empty
// for the root (HLAobjectRoot); otherwise it names the immediate parent.
// Attributes lists only the attributes declared on this class, not inherited.
type ObjectClass struct {
	Name       string
	ParentName string
	Sharing    string
	Semantics  OptionalString
	// QualifiedName retains XML ancestry; Name and ParentName remain local names.
	QualifiedName string
	Attributes    []Attribute
}

// Key distinguishes classes with the same local name in different branches.
// Programmatic models without XML ancestry retain their explicit Name identity.
func (c ObjectClass) Key() string { return classKey(c.Name, c.QualifiedName) }

func (c ObjectClass) ParentKey() string { return parentKey(c.ParentName, c.QualifiedName) }

// Attribute is one attribute on an object class.
type Attribute struct {
	Name            string
	DataType        string
	UpdateType      string
	UpdateCondition OptionalString
	Ownership       string
	Sharing         string
	Transportation  string
	Order           string
	Semantics       OptionalString
	// Dimensions names the global dimensions available to this attribute.
	Dimensions []string
}

// InteractionClass is one node in the FOM interaction class tree.
// ParentName is empty for the root (HLAinteractionRoot).
type InteractionClass struct {
	Name           string
	ParentName     string
	QualifiedName  string
	Sharing        string
	Transportation string
	Order          string
	Parameters     []Parameter
	Semantics      OptionalString
	// Dimensions retains this class's dimension references, not its parent's.
	Dimensions []string
}

func (c InteractionClass) Key() string { return classKey(c.Name, c.QualifiedName) }

func (c InteractionClass) ParentKey() string { return parentKey(c.ParentName, c.QualifiedName) }

func classKey(name, qualified string) string {
	if qualified != "" {
		return qualified
	}
	return name
}

func parentKey(parent, qualified string) string {
	if qualified != "" {
		if i := strings.LastIndexByte(qualified, '.'); i >= 0 {
			return qualified[:i]
		}
		return ""
	}
	return parent
}

// ClassPath joins XML local names without dropping intermediate ancestry.
func ClassPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func LocalClassName(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// Parameter is one parameter on an interaction class.
type Parameter struct {
	Name      string
	DataType  string
	Semantics OptionalString
}
