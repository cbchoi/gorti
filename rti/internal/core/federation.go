package core

import (
	"context"
	"time"
)

// FOMModule represents one FOM XML module submitted at federation create.
type FOMModule struct {
	Path string // optional, for diagnostics
	XML  []byte
}

// CreateFederationRequest is the input to FederationStore.CreateFederation.
//
// TransportMode + DDSDomainID are M19 Phase 1a (docs/m19-dds-adapter.md
// §4.1): the data-plane wire path the federation uses. Default
// TransportModeGRPC matches today's behavior; TransportModeDDS is
// rejected by cmd/rtid composition when the binary was not built with
// `-tags=dds` or the operator did not pass `--enable-dds=true`.
type CreateFederationRequest struct {
	Name          FederationName
	FOMModules    []FOMModule
	Mode          Mode
	StallTimeout  time.Duration // 0 = use server default (60s)
	Seed          uint64        // 0 = derive from name + creation time
	TransportMode TransportMode // 0 (TransportModeUnspecified) treated as GRPC
	DDSDomainID   int32         // only meaningful when TransportMode == DDS
}

// TransportMode is the data-plane wire path for a federation. Mirrors
// the proto rti.v1.TransportMode enum at the core layer so packages
// outside transport/grpc can reason about transport without importing
// the genproto package. M19 Phase 1a — see docs/m19-dds-adapter.md §2.1.
type TransportMode int32

const (
	// TransportModeUnspecified is the zero value; treated as GRPC for
	// backward-compat with cut-2 wire clients.
	TransportModeUnspecified TransportMode = 0
	// TransportModeGRPC is the cut-2 default — control + data plane
	// both ride gRPC streams to rtid.
	TransportModeGRPC TransportMode = 1
	// TransportModeDDS routes the data plane through DDS topics.
	// Control plane stays gRPC. Requires rtid to be built with the
	// `dds` build tag AND the operator to have passed
	// `--enable-dds=true` at startup.
	TransportModeDDS TransportMode = 2
)

// String returns a stable diagnostic string for logging + tests.
// Unknown values surface as "transport(N)" so a future enum addition
// is visible without a code change.
func (t TransportMode) String() string {
	switch t {
	case TransportModeUnspecified:
		return "unspecified"
	case TransportModeGRPC:
		return "grpc"
	case TransportModeDDS:
		return "dds"
	default:
		return "transport(" + itoa32(int32(t)) + ")"
	}
}

// itoa32 inlines a tiny strconv.Itoa to avoid pulling strconv into
// the core package's dependency surface for a single diagnostic
// path. Negative values are rendered with a leading '-'.
func itoa32(v int32) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// JoinFederationRequest is the input to FederationStore.JoinFederation.
//
// FederateType is optional (M13 thread B — docs/srs.md §10.4): when
// non-empty, the federation manager records it per-federate and the
// rtid composition root forwards it through to the MOM hook so the
// HLAfederate.HLAfederateType attribute reflects what the federate
// declared at join-time. Empty FederateType preserves the cut-1
// default behavior.
type JoinFederationRequest struct {
	Federation   FederationName
	FederateName string
	FederateType string
}

// FederationSummary is what ListFederations returns per federation.
type FederationSummary struct {
	Name            FederationName
	Mode            Mode
	FederatesJoined uint32
}

// FederationStore is the entry point for federation lifecycle services.
// Implementations serialize per-federation state mutations.
type FederationStore interface {
	CreateFederation(ctx context.Context, req CreateFederationRequest) error
	DestroyFederation(ctx context.Context, name FederationName) error

	JoinFederation(ctx context.Context, req JoinFederationRequest) (FederateHandle, error)
	ResignFederation(ctx context.Context, fed FederationName, h FederateHandle, action ResignAction) error

	List(ctx context.Context) ([]FederationSummary, error)

	// --- Read-only introspection (rtid-TUI Phase 1) ----------------------

	// Snapshot returns the federation roster (mode + per-federate
	// handles + names) for the AdminService handler. The federations
	// slice is sorted by name; each FederationRoster.Federates slice
	// is sorted by handle. Returns an empty slice when no federations
	// are active.
	Snapshot() []FederationRoster
}

// FederateInfo is one (handle, name) entry on a FederationRoster.
//
// JoinedAt is the wall-clock instant the federate completed
// JoinFederation. Used by the AdminService Snapshot path to populate
// FederateSnapshot.join_unix_seconds (rtid-TUI Phase 3 — the
// drilldown view's `age` column). The zero value indicates the
// federation manager could not record a join time (e.g. a legacy
// federation predating the field) — callers MUST treat
// JoinedAt.IsZero() as "unknown / hide".
//
// Type is the HLAfederateType string the federate declared on
// JoinFederation (M13 thread B — docs/srs.md §10.4). Empty string
// means "not declared" — federates joining via the cut-1 wire format
// (no federate_type field) keep the empty value.
type FederateInfo struct {
	Handle   FederateHandle
	Name     string
	JoinedAt time.Time
	Type     string
}

// FederationRoster is one federation's roster snapshot.
//
// TransportMode + DDSDomainID are M19 Phase 1a (docs/m19-dds-adapter.md
// §4.2): the rtid-TUI surfaces the transport mode in the drilldown
// header so operators can tell at a glance which wire path a
// federation uses. Default TransportModeGRPC keeps today's drilldown
// rendering byte-identical (the TUI elides the DDS column for GRPC-mode
// federations).
type FederationRoster struct {
	Name          FederationName
	Mode          Mode
	Federates     []FederateInfo
	TransportMode TransportMode
	DDSDomainID   int32
}
