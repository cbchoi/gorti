package federate

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/grpc"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

type captureSyncClient struct {
	rtiv1.SyncServiceClient
	register *rtiv1.RegisterSyncPointRequest
	achieve  *rtiv1.AchieveSyncPointRequest
}

func (c *captureSyncClient) RegisterFederationSynchronizationPoint(
	_ context.Context, in *rtiv1.RegisterSyncPointRequest, _ ...grpc.CallOption,
) (*rtiv1.Empty, error) {
	c.register = in
	return &rtiv1.Empty{}, nil
}

func (c *captureSyncClient) SynchronizationPointAchieved(
	_ context.Context, in *rtiv1.AchieveSyncPointRequest, _ ...grpc.CallOption,
) (*rtiv1.Empty, error) {
	c.achieve = in
	return &rtiv1.Empty{}, nil
}

func TestSynchronizationPointCallsPreserveContract(t *testing.T) {
	client := &captureSyncClient{}
	fed := &Federate{
		conn:           &Connection{sync: client},
		federationName: "fair-comparison",
		federateHandle: 17,
	}
	tag := []byte("ready")
	required := []uint64{17, 23}
	if err := fed.RegisterFederationSynchronizationPoint(
		context.Background(), "VERIFY_READY", tag, required,
	); err != nil {
		t.Fatal(err)
	}
	tag[0] = 'X'
	required[0] = 99
	if client.register.GetLabel() != "VERIFY_READY" ||
		string(client.register.GetTag()) != "ready" ||
		!reflect.DeepEqual(client.register.GetRequiredFederates(), []uint64{17, 23}) {
		t.Fatalf("register request = %#v", client.register)
	}
	if err := fed.SynchronizationPointAchieved(
		context.Background(), "VERIFY_READY", true,
	); err != nil {
		t.Fatal(err)
	}
	if client.achieve.GetLabel() != "VERIFY_READY" || !client.achieve.GetSuccessfully() {
		t.Fatalf("achieve request = %#v", client.achieve)
	}
}

func TestTranslateSynchronizationCallbacksCopiesPayloads(t *testing.T) {
	fed := &Federate{}
	announcedProto := &rtiv1.SynchronizationPointAnnounced{
		Label:             "VERIFY_READY",
		Tag:               []byte("tag"),
		RequiredFederates: []uint64{2, 1},
	}
	announced, ok := fed.translate(&rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_SyncAnnounced{SyncAnnounced: announcedProto},
	}).(SynchronizationPointAnnounced)
	if !ok {
		t.Fatal("announce callback was not translated")
	}
	announcedProto.Tag[0] = 'X'
	announcedProto.RequiredFederates[0] = 99
	if string(announced.Tag) != "tag" ||
		!reflect.DeepEqual(announced.RequiredFederates, []uint64{2, 1}) {
		t.Fatalf("announced callback aliases protobuf storage: %#v", announced)
	}

	synchronizedProto := &rtiv1.FederationSynchronized{
		Label:        "VERIFY_READY",
		FailedToSync: []uint64{4},
	}
	synchronized, ok := fed.translate(&rtiv1.FederateEvent{
		Event: &rtiv1.FederateEvent_SyncSynchronized{SyncSynchronized: synchronizedProto},
	}).(FederationSynchronized)
	if !ok {
		t.Fatal("synchronized callback was not translated")
	}
	synchronizedProto.FailedToSync[0] = 88
	if !reflect.DeepEqual(synchronized.FailedToSync, []uint64{4}) {
		t.Fatalf("synchronized callback aliases protobuf storage: %#v", synchronized)
	}
}
