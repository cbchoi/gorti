package main

import (
	"strings"
	"testing"
)

// W8 acceptance, two halves:
//
//  1. Zero config inherits the TUNED product defaults (1MB/1MB,
//     adopted from the isolated A/B — see grpcbuffers.go) so the
//     unmodified lrcbench harness, which composes newRTID with a
//     zero rtidConfig, measures the shipped defaults.
//  2. Both knobs at -1 append NO ServerOptions: the escape hatch is
//     byte-identical to a pre-W8 build.
func TestGRPCBufferServerOptionsDefaultsAndDisable(t *testing.T) {
	// Pin the shipped product defaults to the W8 A/B evidence (1MB
	// per knob, ~17% isolated TCP median win each). Changing them
	// requires fresh measurement, not a drive-by edit.
	if defaultGRPCWriteBufferSize != 1<<20 || defaultGRPCReadBufferSize != 1<<20 {
		t.Fatalf("product defaults drifted: write=%d read=%d, want %d/%d (W8 decision rule)",
			defaultGRPCWriteBufferSize, defaultGRPCReadBufferSize, 1<<20, 1<<20)
	}
	opts, err := grpcBufferServerOptions(0, 0)
	if err != nil {
		t.Fatalf("grpcBufferServerOptions(0, 0): %v", err)
	}
	if len(opts) != 2 {
		t.Fatalf("zero config appended %d ServerOption(s), want 2 (tuned write+read defaults)", len(opts))
	}
	// -1 per knob must append nothing: it forces the gRPC library
	// default, byte-identical to pre-W8 behavior.
	opts, err = grpcBufferServerOptions(grpcBufferDisabled, grpcBufferDisabled)
	if err != nil {
		t.Fatalf("grpcBufferServerOptions(-1, -1): %v", err)
	}
	if len(opts) != 0 {
		t.Fatalf("knobs at -1 appended %d ServerOption(s), want 0 (byte-identical escape hatch)", len(opts))
	}
}

func TestGRPCBufferServerOptionsPositiveValuesAppend(t *testing.T) {
	tests := []struct {
		name        string
		write, read int
		wantLen     int
	}{
		{"write only", 512 << 10, grpcBufferDisabled, 1},
		{"read only", grpcBufferDisabled, 512 << 10, 1},
		{"both", 512 << 10, 1 << 20, 2},
		{"zero inherits both tuned defaults", 0, 0, 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts, err := grpcBufferServerOptions(test.write, test.read)
			if err != nil {
				t.Fatalf("grpcBufferServerOptions(%d, %d): %v", test.write, test.read, err)
			}
			if len(opts) != test.wantLen {
				t.Fatalf("appended %d ServerOption(s), want %d", len(opts), test.wantLen)
			}
		})
	}
}

func TestResolveGRPCBufferSize(t *testing.T) {
	tests := []struct {
		name    string
		v, def  int
		want    int
		wantErr bool
	}{
		{"zero selects product default", 0, 512 << 10, 512 << 10, false},
		{"zero with zero default stays zero", 0, 0, 0, false},
		{"-1 forces library default over nonzero product default", grpcBufferDisabled, 512 << 10, 0, false},
		{"positive passes through", 64 << 10, 0, 64 << 10, false},
		{"other negatives rejected", -2, 0, 0, true},
		{"over 1GiB rejected", maxGRPCBufferSize + 1, 0, 0, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveGRPCBufferSize(test.v, test.def)
			if test.wantErr {
				if err == nil {
					t.Fatalf("resolveGRPCBufferSize(%d, %d) accepted invalid input", test.v, test.def)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveGRPCBufferSize(%d, %d): %v", test.v, test.def, err)
			}
			if got != test.want {
				t.Fatalf("resolveGRPCBufferSize(%d, %d) = %d, want %d", test.v, test.def, got, test.want)
			}
		})
	}
}

// Invalid buffer values must fail newRTID composition with a config
// error rather than silently running untuned.
func TestNewRTIDRejectsInvalidGRPCBufferConfig(t *testing.T) {
	_, err := newRTID(rtidConfig{GRPCWriteBufferSize: -2})
	if err == nil {
		t.Fatal("newRTID accepted GRPCWriteBufferSize=-2")
	}
	if !strings.Contains(err.Error(), "gRPC buffer") {
		t.Fatalf("error %q does not identify the gRPC buffer configuration", err)
	}
	_, err = newRTID(rtidConfig{GRPCReadBufferSize: -7})
	if err == nil {
		t.Fatal("newRTID accepted GRPCReadBufferSize=-7")
	}
}
