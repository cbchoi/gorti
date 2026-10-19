package federate

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
)

const atomicDDMRegisterMethod = "/rti.v1.DDMService/RegisterObjectInstanceWithRegions"

type atomicDDMRegistrationWire struct {
	grpc.ClientConnInterface
	methods      []string
	requests     [][]byte
	objectHandle uint64
	err          error
}

func (w *atomicDDMRegistrationWire) Invoke(_ context.Context, method string, args, reply any, _ ...grpc.CallOption) error {
	payload, err := proto.Marshal(args.(proto.Message))
	if err != nil {
		return err
	}
	w.methods = append(w.methods, method)
	w.requests = append(w.requests, payload)
	var response proto.Message
	switch method {
	case atomicDDMRegisterMethod:
		if w.err != nil {
			return w.err
		}
		response = &rtiv1.RegisterObjectWithRegionsResponse{ObjectHandle: w.objectHandle}
	case "/rti.v1.ObjectService/RegisterObjectInstance":
		// Keep the obsolete first call observable without hiding later failures.
		response = &rtiv1.RegisterObjectResponse{ObjectHandle: 91, ObjectName: "ordinary-only-name"}
	default:
		return fmt.Errorf("unexpected RPC: %s", method)
	}
	payload, err = proto.Marshal(response)
	if err != nil {
		return err
	}
	return proto.Unmarshal(payload, reply.(proto.Message))
}

func TestRegisterObjectInstanceWithRegionsAtomicWire(t *testing.T) {
	const serverHandle uint64 = 1<<60 + 73
	bindings := []AttributeRegions{
		{AttributeHandle: 2, RegionHandles: []uint64{17, 11}},
		{AttributeHandle: 3, RegionHandles: []uint64{29}},
	}
	rejected := status.Error(codes.FailedPrecondition, "region does not belong to federate")
	unavailable := status.Error(codes.Unavailable, "DDM service unavailable")
	for _, test := range []struct {
		name         string
		bindings     []AttributeRegions
		objectHandle uint64
		rpcErr       error
		wantErr      error
		zeroHandle   bool
	}{
		{name: "success", bindings: bindings, objectHandle: serverHandle},
		{name: "empty_bindings", objectHandle: serverHandle},
		{name: "rejected", bindings: bindings, rpcErr: rejected, wantErr: rejected},
		{name: "unavailable", bindings: bindings, rpcErr: unavailable, wantErr: unavailable},
		{
			name: "typed_error", bindings: bindings,
			rpcErr: status.Error(codes.NotFound, "attribute not found"), wantErr: ErrAttributeNotDefined,
		},
		{name: "zero_handle", bindings: bindings, zeroHandle: true},
	} {
		for _, objectName := range []string{"region-object", ""} {
			t.Run(test.name+"/name="+objectName, func(t *testing.T) {
				wire := &atomicDDMRegistrationWire{objectHandle: test.objectHandle, err: test.rpcErr}
				fed := &Federate{
					conn: &Connection{
						ddm: rtiv1.NewDDMServiceClient(wire),
						obj: rtiv1.NewObjectServiceClient(wire),
					},
					federationName: "atomic-registration",
					federateHandle: 23,
				}
				fed.rememberPublishedObjectAttributes(7, []uint64{2, 3})
				fed.rememberRegisteredObject(41, 7)

				handle, err := fed.RegisterObjectInstanceWithRegions(context.Background(), 7, test.bindings, objectName)
				if !reflect.DeepEqual(wire.methods, []string{atomicDDMRegisterMethod}) {
					t.Errorf("RPC sequence = %v, want exactly one DDM registration and no ordinary registration", wire.methods)
				}
				wantRequest := &rtiv1.RegisterObjectWithRegionsRequest{
					WireVersion:       rtiv1.WireVersion_WIRE_VERSION_V1,
					FederationName:    "atomic-registration",
					FederateHandle:    23,
					ObjectClassHandle: 7,
					ObjectName:        objectName,
				}
				if len(test.bindings) != 0 {
					wantRequest.AttributeRegions = []*rtiv1.AttributeRegions{
						{AttributeHandle: 2, RegionHandles: []uint64{17, 11}},
						{AttributeHandle: 3, RegionHandles: []uint64{29}},
					}
				}
				for i, method := range wire.methods {
					if method != atomicDDMRegisterMethod {
						continue
					}
					request := &rtiv1.RegisterObjectWithRegionsRequest{}
					if err := proto.Unmarshal(wire.requests[i], request); err != nil {
						t.Fatal(err)
					}
					if !proto.Equal(request, wantRequest) {
						t.Errorf("DDM request = %v, want %v", request, wantRequest)
					}
				}

				wantClasses := map[uint64]uint64{41: 7}
				wantOwned := map[uint64]map[uint64]struct{}{41: {2: {}, 3: {}}}
				switch {
				case test.zeroHandle:
					if handle != 0 || err == nil || !strings.Contains(err.Error(), "zero object handle") {
						t.Errorf("zero response = (%d, %v), want zero object handle error", handle, err)
					}
				case test.wantErr != nil:
					if handle != 0 || !errors.Is(err, test.wantErr) {
						t.Errorf("failure = (%d, %v), want (0, %v)", handle, err, test.wantErr)
					}
				default:
					if err != nil || handle != serverHandle {
						t.Errorf("registration = (%d, %v), want server handle %d", handle, err, serverHandle)
					}
					wantClasses[serverHandle] = 7
					wantOwned[serverHandle] = map[uint64]struct{}{2: {}, 3: {}}
				}
				if !reflect.DeepEqual(fed.objectClasses, wantClasses) {
					t.Errorf("remembered classes = %v, want %v", fed.objectClasses, wantClasses)
				}
				if !reflect.DeepEqual(fed.ownedObjectAttributes, wantOwned) {
					t.Errorf("remembered ownership = %v, want %v", fed.ownedObjectAttributes, wantOwned)
				}
			})
		}
	}
}
