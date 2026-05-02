package model

// Compile-time assertions: every DataType variant satisfies the sealed
// interface. If a future commit forgets to implement isDataType or Name on
// a new variant, the build breaks here before any runtime test executes.
var (
	_ DataType = (*BasicData)(nil)
	_ DataType = (*SimpleData)(nil)
	_ DataType = (*EnumeratedData)(nil)
	_ DataType = (*ArrayData)(nil)
	_ DataType = (*FixedRecordData)(nil)
	_ DataType = (*VariantRecordData)(nil)
)
