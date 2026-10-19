package savebundle

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func validateMOMPointCallbackMetadata(metadata *core.ObjectCallbackMetadata) error {
	if metadata == nil || metadata.MOMAttributePoint == nil {
		return nil
	}
	point := metadata.MOMAttributePoint
	if point.Subject == 0 || point.Subject == ^core.FederateHandle(0) || point.Dimension == 0 || point.Coordinate != uint64(point.Subject)&0xffffffff {
		return fmt.Errorf("time snapshot: invalid MOM attribute point")
	}
	if len(metadata.Regions) != 0 || len(metadata.ConveyedRegions) != 0 || len(metadata.AttributeConveyedRegions) != 0 || metadata.RetractionHandle != 0 || metadata.SentOrder != core.OrderReceive || metadata.ReceivedOrder != core.OrderReceive || metadata.DelayedSubscription {
		return fmt.Errorf("time snapshot: MOM point conflicts with ordinary regional or timestamp metadata")
	}
	return nil
}

func validateMOMPointQueuedEvent(kind string, metadata *core.ObjectCallbackMetadata, receiveOrder, timestampPresent bool) error {
	if metadata != nil && metadata.MOMAttributePoint != nil && (kind != checkpointReflect || !receiveOrder || timestampPresent) {
		return fmt.Errorf("time snapshot: MOM point requires a timestamp-free RO reflection")
	}
	return nil
}
