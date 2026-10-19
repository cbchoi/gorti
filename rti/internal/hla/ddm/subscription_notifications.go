package ddm

import (
	"context"
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// Registered before lock-release defers, this runs only after advisory
// admission or rollback has finished and all subscription locks are released.
func (m *Manager) notifyObjectSubscribed(ctx context.Context, fed core.FederationName, sub core.FederateHandle, cls core.ObjectClassHandle, attrs *[]core.AttributeHandle, err *error) {
	if *err != nil || len(*attrs) == 0 || m.opts.OnObjectSubscribed == nil {
		return
	}
	snapshot := slices.Clone(*attrs)
	slices.Sort(snapshot)
	m.opts.OnObjectSubscribed(ctx, fed, sub, cls, slices.Compact(snapshot))
}
