package model

// DataType is the sealed sum type for IEEE 1516.2-2010 §6 OMT data types.
// Concrete variants implement isDataType so the set is closed within this
// package; callers switch on the concrete type to discriminate.
type DataType interface {
	// Name returns the FOM-declared name of the data type.
	Name() string
	// isDataType is the sealing method; it has no observable behavior.
	isDataType()
}

// BasicData represents a basic data representation (1516.2-2010 §6.2).
// Examples: HLAinteger32BE, HLAfloat64BE, HLAoctet.
type BasicData struct {
	NameField      string
	Size           int
	Endian         string
	Interpretation string
	Encoding       string
}

// Name returns the data type's declared name.
func (d *BasicData) Name() string { return d.NameField }
func (*BasicData) isDataType()    {}

// SimpleData represents a simple data type (1516.2-2010 §6.3) — a named
// alias over a basic representation with units, accuracy, and resolution.
type SimpleData struct {
	NameField      string
	Representation string
	Units          string
	Resolution     string
	Accuracy       string
}

// Name returns the data type's declared name.
func (d *SimpleData) Name() string { return d.NameField }
func (*SimpleData) isDataType()    {}

// EnumeratedData represents an enumerated data type (1516.2-2010 §6.4).
type EnumeratedData struct {
	NameField      string
	Representation string
	Enumerators    []Enumerator
}

// Enumerator is one named value within an EnumeratedData.
type Enumerator struct {
	Name   string
	Values string
}

// Name returns the data type's declared name.
func (d *EnumeratedData) Name() string { return d.NameField }
func (*EnumeratedData) isDataType()    {}

// ArrayData represents an array data type (1516.2-2010 §6.5). When
// Cardinality is "Dynamic" the array is variable-length; otherwise a
// fixed-length array with the cardinality string per the spec.
type ArrayData struct {
	NameField   string
	DataType    string
	Cardinality string
	Encoding    string
}

// Name returns the data type's declared name.
func (d *ArrayData) Name() string { return d.NameField }
func (*ArrayData) isDataType()    {}

// FixedRecordData represents a fixed record data type (1516.2-2010 §6.6).
type FixedRecordData struct {
	NameField string
	Fields    []RecordField
	Encoding  string
}

// RecordField is one named field of a FixedRecordData or VariantRecordData.
type RecordField struct {
	Name     string
	DataType string
}

// Name returns the data type's declared name.
func (d *FixedRecordData) Name() string { return d.NameField }
func (*FixedRecordData) isDataType()    {}

// VariantRecordData represents a variant record data type (1516.2-2010 §6.7).
type VariantRecordData struct {
	NameField        string
	DiscriminantName string
	DiscriminantType string
	Alternatives     []VariantAlternative
	Encoding         string
}

// VariantAlternative is one alternative within a VariantRecordData.
type VariantAlternative struct {
	Enumerator string
	Name       string
	DataType   string
}

// Name returns the data type's declared name.
func (d *VariantRecordData) Name() string { return d.NameField }
func (*VariantRecordData) isDataType()    {}
