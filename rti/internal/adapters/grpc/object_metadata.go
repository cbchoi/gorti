package grpc

import (
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func callbackMetadata(m *core.ObjectCallbackMetadata) *rtiv1.ObjectCallbackMetadata {
	if m == nil {
		return nil
	}
	out := &rtiv1.ObjectCallbackMetadata{}
	if m.SentOrder != 0 {
		v := rtiv1.OrderType(m.SentOrder)
		out.SentOrder = &v
	}
	if m.ReceivedOrder != 0 {
		v := rtiv1.OrderType(m.ReceivedOrder)
		out.ReceivedOrder = &v
	}
	if m.Transportation != 0 {
		v := rtiv1.TransportationType(m.Transportation)
		out.TransportationType = &v
	}
	if m.Producer != 0 {
		v := uint64(m.Producer)
		out.ProducingFederate = &v
	}
	if m.RetractionHandle != 0 {
		v := m.RetractionHandle
		out.MessageRetractionHandle = &v
	}
	if regions := m.ConveyedRegionDesignators(); regions != nil {
		out.ReceivedRegions = &rtiv1.ReceivedRegions{RegionHandles: make([]uint64, len(regions))}
		for i, h := range regions {
			out.ReceivedRegions.RegionHandles[i] = uint64(h)
		}
	}
	return out
}
