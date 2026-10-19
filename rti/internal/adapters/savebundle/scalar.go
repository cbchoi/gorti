package savebundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// The wire scalar must match the authoritative domain scalar. Integer codecs
// use an int64 JSON decoder, including for values beyond Float64 precision.
type integerSnapshotNumber int64

func (t integerSnapshotNumber) MarshalJSON() ([]byte, error) {
	if t < 0 {
		return nil, fmt.Errorf("invalid saved integer logical time %d", t)
	}
	return json.Marshal(int64(t))
}

func (t *integerSnapshotNumber) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("null saved integer logical time")
	}
	var value int64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value < 0 {
		return fmt.Errorf("invalid saved integer logical time %d", value)
	}
	*t = integerSnapshotNumber(value)
	return nil
}

func integerScalar[T core.LogicalTimeScalar]() bool { return T(1)/T(2) == 0 }

func validSnapshotScalar[T core.LogicalTimeScalar](value T) bool {
	// The comparison rejects NaN but retains the legacy positive-infinity sentinel.
	return value >= 0
}

func finiteSnapshotScalar[T core.LogicalTimeScalar](value T) bool {
	if integerScalar[T]() {
		return value >= 0
	}
	return value >= 0 && !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

func validateManifestScalar[T core.LogicalTimeScalar](manifest core.CheckpointManifestOf[T]) error {
	name := manifest.LogicalTimeImplementationName
	if integerScalar[T]() {
		if name != checkpointHLAinteger64Time {
			return fmt.Errorf("integer checkpoint requires HLAinteger64Time")
		}
	} else if name != "" && name != checkpointHLAfloat64Time {
		return fmt.Errorf("Float64 checkpoint has incompatible time representation")
	}
	if manifest.SaveTime != nil && !finiteSnapshotScalar(*manifest.SaveTime) {
		return fmt.Errorf("invalid checkpoint save time")
	}
	return nil
}

func requireSnapshotScalarField(fields map[string]json.RawMessage, name string) error {
	for key, value := range fields {
		if !strings.EqualFold(key, name) {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("null required snapshot scalar %s", name)
		}
		return nil
	}
	return fmt.Errorf("missing required snapshot scalar %s", name)
}
