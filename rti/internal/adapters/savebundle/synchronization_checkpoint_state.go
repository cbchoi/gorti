package savebundle

import (
	"fmt"
	syncpkg "github.com/cbchoi/gorti/rti/internal/hla/synchronization"
)

func ReadSynchronizationCheckpointState(data []byte) (*syncpkg.ManagerState, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if !isStrictSynchronization(data) {
		return nil, fmt.Errorf("sync snapshot: exact callback history unavailable")
	}
	return readStrictSynchronization(data)
}
