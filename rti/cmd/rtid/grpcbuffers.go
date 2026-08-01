package main

import (
	"fmt"

	stdgrpc "google.golang.org/grpc"
)

// W8 — gRPC transport buffer knobs.
//
// The write/read buffer sizes of the federate-facing gRPC listener are
// operator-tunable via --grpc-write-buffer / --grpc-read-buffer. The
// PRODUCT DEFAULTS below are what an untouched rtid (and the W0
// lrcbench harness, which composes newRTID with a zero rtidConfig)
// actually runs with.
//
// Value semantics (flag and rtidConfig field alike):
//
//	 0 → product default (the constants below)
//	-1 → force the gRPC library default (no ServerOption appended;
//	     byte-identical to a pre-W8 build)
//	>0 → that many bytes per connection buffer
//
// Any other negative value is a configuration error.
const (
	// defaultGRPCWriteBufferSize / defaultGRPCReadBufferSize are the
	// PRODUCT DEFAULTS, adopted from the W8 isolated A/B on the W0
	// TCP lrcbench (5 process-isolated runs per config, interleaved
	// with baseline for drift control, medians):
	//
	//	knob        512KB median    1MB median      baseline median
	//	write buf   223508327 ns    202196643 ns    244341779 ns
	//	read  buf   222874220 ns    203518039 ns    244341779 ns
	//
	// Each knob IN ISOLATION at 1MB is a ~17% median win with spread
	// well under the win (0.4% / 2.0%), satisfying the >=2% decision
	// rule, so 1MB is the shipped default for both. Operators can
	// override per-knob (-1 restores the 32KB gRPC library default,
	// byte-identical to a pre-W8 build).
	defaultGRPCWriteBufferSize = 1 << 20
	defaultGRPCReadBufferSize  = 1 << 20

	// grpcBufferDisabled forces the gRPC library default explicitly,
	// overriding a (possibly nonzero) product default.
	grpcBufferDisabled = -1

	// maxGRPCBufferSize caps operator input at 1GiB — anything larger
	// is certainly a unit mistake (bytes, not KB/MB).
	maxGRPCBufferSize = 1 << 30
)

// resolveGRPCBufferSize maps a flag/config buffer value to the
// effective per-connection size in bytes. 0 selects def (the product
// default); grpcBufferDisabled (-1) selects the gRPC library default
// (returned as 0 = "append nothing").
func resolveGRPCBufferSize(v, def int) (int, error) {
	switch {
	case v == 0:
		return def, nil
	case v == grpcBufferDisabled:
		return 0, nil
	case v < 0:
		return 0, fmt.Errorf("gRPC buffer size must be positive, 0 (default), or -1 (library default); got %d", v)
	case v > maxGRPCBufferSize:
		return 0, fmt.Errorf("gRPC buffer size must be at most %d bytes; got %d", maxGRPCBufferSize, v)
	default:
		return v, nil
	}
}

// grpcBufferServerOptions resolves the write/read buffer knobs and
// returns the ServerOptions to append. A resolved size of 0 appends
// NOTHING for that knob — grpc.WriteBufferSize(0)/ReadBufferSize(0)
// would disable buffering entirely (every write hits the wire), which
// is NOT the library default, so the zero value must never be passed
// through.
func grpcBufferServerOptions(writeBuffer, readBuffer int) ([]stdgrpc.ServerOption, error) {
	write, err := resolveGRPCBufferSize(writeBuffer, defaultGRPCWriteBufferSize)
	if err != nil {
		return nil, fmt.Errorf("--grpc-write-buffer: %w", err)
	}
	read, err := resolveGRPCBufferSize(readBuffer, defaultGRPCReadBufferSize)
	if err != nil {
		return nil, fmt.Errorf("--grpc-read-buffer: %w", err)
	}
	var opts []stdgrpc.ServerOption
	if write > 0 {
		opts = append(opts, stdgrpc.WriteBufferSize(write))
	}
	if read > 0 {
		opts = append(opts, stdgrpc.ReadBufferSize(read))
	}
	return opts, nil
}
