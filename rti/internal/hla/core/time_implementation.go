package core

import (
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

var ErrCouldNotCreateLogicalTimeFactory = errors.New("could not create logical time factory")

// ResolveLogicalTimeImplementationName describes the representations supported
// by the complete service path, not merely by the local value-type library.
func ResolveLogicalTimeImplementationName(name string) (string, error) {
	return ResolveLogicalTimeImplementationCapabilities(name, logicaltime.HLAfloat64Time.String())
}

// ResolveLogicalTimeImplementationCapabilities accepts only standardized value
// factories whose complete service paths have been composed by the application.
func ResolveLogicalTimeImplementationCapabilities(name string, supported ...string) (string, error) {
	if name == "" {
		name = logicaltime.HLAfloat64Time.String()
	}
	if name == logicaltime.HLAfloat64Time.String() || name == logicaltime.HLAinteger64Time.String() {
		for _, capability := range supported {
			if capability == name {
				return name, nil
			}
		}
	}
	return "", fmt.Errorf("%w: %q is not supported by this server", ErrCouldNotCreateLogicalTimeFactory, name)
}
