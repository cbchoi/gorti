package grpc

import (
	"context"
	"math"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type timeDecodeTrailerStream struct{ trailer metadata.MD }

func (*timeDecodeTrailerStream) Method() string               { return "time" }
func (*timeDecodeTrailerStream) SetHeader(metadata.MD) error  { return nil }
func (*timeDecodeTrailerStream) SendHeader(metadata.MD) error { return nil }
func (s *timeDecodeTrailerStream) SetTrailer(md metadata.MD) error {
	s.trailer = metadata.Join(s.trailer, md)
	return nil
}

// Any domain invocation (including a state query) panics through the nil port.
type timeDecodeNoDomain[T core.LogicalTimeScalar] struct{ core.TimeManagerOf[T] }

func reviewTimeDecodeInvoke[T core.LogicalTimeScalar](ctx context.Context, service *timeServiceOf[T], request proto.Message) error {
	var err error
	switch req := request.(type) {
	case *rtiv1.TARRequest:
		_, err = service.TimeAdvanceRequest(ctx, req)
	case *rtiv1.TARARequest:
		_, err = service.TimeAdvanceRequestAvailable(ctx, req)
	case *rtiv1.NERRequest:
		_, err = service.NextMessageRequest(ctx, req)
	case *rtiv1.NMRARequest:
		_, err = service.NextMessageRequestAvailable(ctx, req)
	case *rtiv1.FQRRequest:
		_, err = service.FlushQueueRequest(ctx, req)
	case *rtiv1.EnableRegulationRequest:
		_, err = service.EnableTimeRegulation(ctx, req)
	case *rtiv1.ModifyLookaheadRequest:
		_, err = service.ModifyLookahead(ctx, req)
	default:
		panic("invalid time review request")
	}
	return err
}

func reviewTimeDecodeMetadata[T core.LogicalTimeScalar](t *testing.T, integer bool) {
	t.Helper()
	floatWire := func(value float64) *rtiv1.LogicalTimeValue {
		return &rtiv1.LogicalTimeValue{Value: &rtiv1.LogicalTimeValue_Float64Value{Float64Value: value}}
	}
	type decodeCase struct {
		name   string
		legacy float64
		exact  *rtiv1.LogicalTimeValue
	}
	for _, method := range []struct {
		name       string
		newRequest func() proto.Message
		interval   bool
	}{
		{"TAR", func() proto.Message { return &rtiv1.TARRequest{} }, false},
		{"TARA", func() proto.Message { return &rtiv1.TARARequest{} }, false},
		{"NER", func() proto.Message { return &rtiv1.NERRequest{} }, false},
		{"NMRA", func() proto.Message { return &rtiv1.NMRARequest{} }, false},
		{"FQR", func() proto.Message { return &rtiv1.FQRRequest{} }, false},
		{"enable", func() proto.Message { return &rtiv1.EnableRegulationRequest{} }, true},
		{"modify", func() proto.Message { return &rtiv1.ModifyLookaheadRequest{} }, true},
	} {
		rows := []decodeCase{
			{"empty-selector", 0, &rtiv1.LogicalTimeValue{}},
			{"exact-nan", 0, floatWire(math.NaN())},
			{"exact-infinite", 0, floatWire(math.Inf(1))},
			{"exact-negative", 0, floatWire(-1)},
			{"conflicting-pair", 1, floatWire(2)},
		}
		if integer {
			rows = append(rows, decodeCase{"integer-negative", 0, integerWire(-1)},
				decodeCase{"missing-integer", 0, nil},
				decodeCase{"integer-mixed-pair", 1, integerWire(1)})
		} else {
			rows = append(rows, decodeCase{"wrong-integer-kind", 0, integerWire(1)})
		}
		if method.interval {
			for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1} {
				rows = append(rows, decodeCase{"legacy-invalid", value, nil})
			}
		}
		for _, row := range rows {
			t.Run(method.name+"/"+row.name, func(t *testing.T) {
				stream := &timeDecodeTrailerStream{}
				ctx := grpcgo.NewContextWithServerTransportStream(context.Background(), stream)
				request := method.newRequest()
				message := request.ProtoReflect()
				fields := message.Descriptor().Fields()
				message.Set(fields.ByName("wire_version"), protoreflect.ValueOfEnum(1))
				message.Set(fields.ByName("federation_name"), protoreflect.ValueOfString("fed"))
				message.Set(fields.ByName("federate_handle"), protoreflect.ValueOfUint64(1))
				legacyField, exactField, exception := "logical_time", "logical_time_value", "InvalidLogicalTime"
				if method.interval {
					legacyField, exactField, exception = "lookahead", "lookahead_value", "InvalidLookahead"
				}
				message.Set(fields.ByName(protoreflect.Name(legacyField)), protoreflect.ValueOfFloat64(row.legacy))
				if row.exact != nil {
					message.Set(fields.ByName(protoreflect.Name(exactField)), protoreflect.ValueOfMessage(row.exact.ProtoReflect()))
				}
				err := reviewTimeDecodeInvoke(ctx, newTimeServiceOf[T](&timeDecodeNoDomain[T]{}), request)
				if status.Code(err) != codes.InvalidArgument || !reflect.DeepEqual(stream.trailer.Get(specExceptionTrailerKey), []string{exception}) {
					t.Fatalf("error=%v trailer=%v want=%s", err, stream.trailer, exception)
				}
			})
		}
	}
}

func TestTimeDecodeFailuresPreserveIEEEBeforeDomain(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { reviewTimeDecodeMetadata[core.LogicalTime](t, false) })
	t.Run("Integer64", func(t *testing.T) { reviewTimeDecodeMetadata[int64](t, true) })
}
