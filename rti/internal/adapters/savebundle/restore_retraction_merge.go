package savebundle

import (
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

// MergeSessionRetractions carries IDs accepted by the consenting current
// sessions into their new identities, in addition to the exact saved history.
// The live manager's import also retains all original target-generation IDs.
func MergeSessionRetractionsOf[T core.LogicalTimeScalar](saved, current []byte, sessions map[core.FederateHandle]core.FederateHandle) ([]byte, error) {
	a, err := readRetractionsOf[T](saved)
	if err != nil {
		return nil, err
	}
	b, err := readRetractionsOf[T](current)
	if err != nil {
		return nil, err
	}
	for i := range b.Producers {
		if target, ok := sessions[b.Producers[i].Producer]; ok {
			b.Producers[i].Producer = target
		}
	}
	merged, err := timepkg.MergeRetractionHistoriesOf[T](a, b)
	if err != nil {
		return nil, err
	}
	return writeRetractionsOf[T](merged)
}
