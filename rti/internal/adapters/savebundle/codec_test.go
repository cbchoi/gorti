package savebundle

import (
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestSnapshotCodecsRejectMalformedProtobuf(t *testing.T) {
	garbage := []byte{0xff, 0xff, 0xff}
	for name, codec := range map[string]interface {
		Unmarshal(core.FederationName, []byte) error
	}{
		"synchronization": Synchronization{},
		"ownership":       Ownership{},
		"mom":             MOM{},
		"ddm":             DDM{},
	} {
		t.Run(name, func(t *testing.T) {
			if err := codec.Unmarshal("fed", garbage); err == nil {
				t.Fatal("malformed snapshot accepted")
			}
		})
	}
}
