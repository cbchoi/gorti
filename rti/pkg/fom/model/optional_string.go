package model

// OptionalString retains an optional descriptive declaration. Present=true
// with Value="" is an explicit empty element; the zero value is absent.
// Exported value fields preserve that distinction in snapshot serialization.
type OptionalString struct {
	Value   string
	Present bool
}

func OptionalStringFrom(value *string) OptionalString {
	if value == nil {
		return OptionalString{}
	}
	return OptionalString{Value: *value, Present: true}
}

// MergeOptionalString fills an absent declaration but never overwrites a
// different supplied value, including an explicit empty description. This is
// a bounded engineering compatibility rule, not full modular-FOM validation.
func MergeOptionalString(a, b OptionalString) (OptionalString, bool) {
	if !a.Present {
		return b, true
	}
	if !b.Present {
		return a, true
	}
	return a, a == b
}
