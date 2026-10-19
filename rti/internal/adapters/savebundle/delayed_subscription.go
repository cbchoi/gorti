package savebundle

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func validateDelayedSubscriptionMetadata(kind string, metadata *core.ObjectCallbackMetadata) error {
	if metadata == nil {
		return nil
	}
	if !metadata.DelayedSubscription {
		if metadata.RegisteredClass != 0 {
			return fmt.Errorf("time snapshot: registered class without delayed evaluation")
		}
		return nil
	}
	if kind != checkpointReflect && kind != checkpointReceive {
		return fmt.Errorf("time snapshot: delayed evaluation on non-data callback")
	}
	if (kind == checkpointReflect) != (metadata.RegisteredClass != 0) {
		return fmt.Errorf("time snapshot: invalid delayed canonical class")
	}
	return nil
}

func (v *graphValidationOf[T]) delayedCanonicalClass(object core.ObjectHandle, known, actual core.ObjectClassHandle, metadata *core.ObjectCallbackMetadata) (core.ObjectClassHandle, error) {
	policy, ok := v.model.(core.FOMSubscriptionEvaluation)
	if !ok {
		return 0, fmt.Errorf("delayed callback without FDD subscription policy")
	}
	enabled, _ := policy.DelaySubscriptionEvaluation()
	if !enabled {
		return 0, fmt.Errorf("delayed callback when FDD switch disabled")
	}
	canonical := metadata.RegisteredClass
	if object == 0 || uint64(object) > v.lastObject || canonical == 0 || actual != 0 && actual != canonical {
		return 0, fmt.Errorf("delayed callback canonical object mismatch")
	}
	if known != 0 && !v.validKnownClass(canonical, known) {
		return 0, fmt.Errorf("delayed callback known class mismatch")
	}
	return canonical, nil
}

const checkpointReceive = "receive"

const checkpointReflect = "reflect"
