package m19spec

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// TestTransportModeEnum locks in the wire ordinals for TransportMode.
// Append-only proto contract — once shipped, the integer values cannot
// move or be reused. The test fails loudly if a future PR accidentally
// reorders or renumbers the enum.
func TestTransportModeEnum(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  rtiv1.TransportMode
		want int32
	}{
		{"unspecified", rtiv1.TransportMode_TRANSPORT_MODE_UNSPECIFIED, 0},
		{"grpc", rtiv1.TransportMode_TRANSPORT_MODE_GRPC, 1},
		{"dds", rtiv1.TransportMode_TRANSPORT_MODE_DDS, 2},
	}
	for _, c := range cases {
		if int32(c.got) != c.want {
			t.Errorf("TransportMode_%s = %d; want %d (append-only proto contract)",
				c.name, int32(c.got), c.want)
		}
	}
}

// TestCreateFederationRequest_TransportModeBackwardCompat asserts the
// new field defaults to UNSPECIFIED when omitted. Old federates that
// don't set the field continue to land in the GRPC code path
// (handler interprets UNSPECIFIED as GRPC).
func TestCreateFederationRequest_TransportModeBackwardCompat(t *testing.T) {
	t.Parallel()
	req := &rtiv1.CreateFederationRequest{
		FederationName: "fed",
	}
	if req.GetTransportMode() != rtiv1.TransportMode_TRANSPORT_MODE_UNSPECIFIED {
		t.Errorf("CreateFederationRequest{} transport_mode = %v; want UNSPECIFIED (append-only default)",
			req.GetTransportMode())
	}
}

// TestJoinFederationResponse_DDSFieldsBackwardCompat asserts the join
// response defaults are append-only safe — federates joining a
// pre-M19 rtid see UNSPECIFIED transport_mode and zero domain_id, both
// of which collapse to today's GRPC behavior.
func TestJoinFederationResponse_DDSFieldsBackwardCompat(t *testing.T) {
	t.Parallel()
	resp := &rtiv1.JoinFederationResponse{FederateHandle: 1}
	if resp.GetTransportMode() != rtiv1.TransportMode_TRANSPORT_MODE_UNSPECIFIED {
		t.Errorf("JoinFederationResponse{} transport_mode = %v; want UNSPECIFIED",
			resp.GetTransportMode())
	}
	if resp.GetDdsDomainId() != 0 {
		t.Errorf("JoinFederationResponse{} dds_domain_id = %d; want 0",
			resp.GetDdsDomainId())
	}
}
