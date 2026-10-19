package savepoint

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

func (m *ManagerOf[T]) timeImplementationFor(fed core.FederationName) (string, error) {
	name := core.ScalarRepresentation[T]().String()
	if m.opts.LogicalTimeImplementation != nil {
		var found bool
		name, found = m.opts.LogicalTimeImplementation(fed)
		if !found || name == "" {
			return "", fmt.Errorf("%w: federation time implementation unavailable", ErrSaveBundleIncompatible)
		}
	}
	resolved, err := logicaltime.ParseRepresentation(name)
	if err != nil || resolved != core.ScalarRepresentation[T]() {
		return "", fmt.Errorf("%w: %v", ErrSaveBundleIncompatible, err)
	}
	return resolved.String(), nil
}

func (m *ManagerOf[T]) validateRestoreTimeImplementation(fed core.FederationName, manifest ManifestOf[T]) error {
	live, err := m.timeImplementationFor(fed)
	if err != nil {
		return err
	}
	// Old bundles predate selectable time and contain Float64 snapshots.
	savedName := manifest.LogicalTimeImplementationName
	if savedName == "" {
		savedName = logicaltime.HLAfloat64Time.String()
	}
	saved, err := logicaltime.ParseRepresentation(savedName)
	if err != nil || saved.String() != live {
		return fmt.Errorf("%w: saved time implementation %q is incompatible with %q", ErrSaveBundleIncompatible, manifest.LogicalTimeImplementationName, live)
	}
	return nil
}
