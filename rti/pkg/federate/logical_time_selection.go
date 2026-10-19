package federate

import "fmt"

const HLAfloat64Time = "HLAfloat64Time"
const HLAinteger64Time = "HLAinteger64Time"

// FederationCreationResponseError records a successful remote creation whose
// response selected an unsupported implementation. The execution was not
// automatically destroyed. Creation retains server metadata for manual cleanup.
type FederationCreationResponseError struct {
	FederationName string
	Creation       FederationCreation
}

func (e *FederationCreationResponseError) Error() string {
	return fmt.Sprintf("%v: server already created federation execution %q (generation %d), but its successful response reported unsupported logical time implementation %q; no automatic destruction was attempted",
		ErrRTIInternal, e.FederationName, e.Creation.FederationGeneration, e.Creation.LogicalTimeImplementationName)
}

func (e *FederationCreationResponseError) Unwrap() error { return ErrRTIInternal }

func logicalTimeImplementationName(name string) string {
	if name == "" {
		return HLAfloat64Time
	}
	return name
}

func requireSupportedLogicalTime(name string) error {
	if selected := logicalTimeImplementationName(name); selected != HLAfloat64Time && selected != HLAinteger64Time {
		return fmt.Errorf("%w: %q is not supported by this SDK", ErrCouldNotCreateLogicalTimeFactory, name)
	}
	return nil
}

// LogicalTimeImplementationName returns the joined execution's selected type.
// An absent name from a legacy server means HLAfloat64Time, not Integer64 support.
// Nil or uninitialized federates report no implementation.
func (f *Federate) LogicalTimeImplementationName() string {
	if f == nil {
		return ""
	}
	return f.logicalTimeImplementationName
}
