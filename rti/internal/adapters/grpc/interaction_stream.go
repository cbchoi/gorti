package grpc

import (
	"io"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const interactionStreamMethod = "/rti.v1.InteractionStreamService/SendInteractions"

type interactionStreamService interface {
	SendInteractions(stdgrpc.ServerStream) error
}

type interactionStreamHandler struct {
	objects  *objectService
	observer *MOMObserver
}

func (h *interactionStreamHandler) SendInteractions(stream stdgrpc.ServerStream) error {
	// Send headers before reading the first message. The client uses this as a
	// capability handshake, so an old server can fall back before transmitting
	// an interaction and risking a duplicate retry.
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}
	ack := &rtiv1.Empty{}
	for {
		request := new(rtiv1.SendInteractionRequest)
		if err := stream.RecvMsg(request); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if request.GetWireVersion() == rtiv1.WireVersion_WIRE_VERSION_UNSPECIFIED &&
			request.GetFederationName() == "" && request.GetFederateHandle() == 0 {
			if err := stream.SendMsg(ack); err != nil {
				return err
			}
			continue
		}
		response, err := h.objects.SendInteraction(stream.Context(), request)
		if h.observer != nil {
			h.observer.Observe(stream.Context(), "/rti.v1.ObjectService/SendInteraction", request, response, err)
		}
		if err != nil {
			return err
		}
		if err := stream.SendMsg(ack); err != nil {
			return err
		}
	}
}

var interactionStreamServiceDescription = stdgrpc.ServiceDesc{
	ServiceName: "rti.v1.InteractionStreamService",
	HandlerType: (*interactionStreamService)(nil),
	Streams: []stdgrpc.StreamDesc{{
		StreamName:    "SendInteractions",
		Handler:       interactionStreamServerHandler,
		ServerStreams: true,
		ClientStreams: true,
	}},
	Metadata: "internal/interaction_stream",
}

func interactionStreamServerHandler(service any, stream stdgrpc.ServerStream) error {
	return service.(interactionStreamService).SendInteractions(stream)
}
