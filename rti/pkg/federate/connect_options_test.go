package federate

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
)

func TestConnectWithOptionsInitializesConfirmedObjectStreamByEligibility(t *testing.T) {
	tests := []struct {
		name       string
		opts       ConnectOptions
		wantStream bool
	}{
		{
			name:       "default insecure connection",
			wantStream: true,
		},
		{
			name:       "bearer token",
			opts:       ConnectOptions{BearerToken: "test-token"},
			wantStream: false,
		},
		{
			name: "bearer token provider",
			opts: ConnectOptions{BearerTokenProvider: func(context.Context) (string, error) {
				return "test-token", nil
			}},
			wantStream: false,
		},
		{
			// Dial tuning must NOT downgrade transport selection: only
			// auth options gate stream eligibility.
			name: "custom dial option",
			opts: ConnectOptions{ExtraDialOptions: []grpc.DialOption{
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
					return nil, context.Canceled
				}),
			}},
			wantStream: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn, err := ConnectWithOptions(
				context.Background(),
				"passthrough:///confirmed-object-connect-test",
				test.opts,
			)
			if err != nil {
				t.Fatalf("ConnectWithOptions: %v", err)
			}
			t.Cleanup(func() {
				if err := conn.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			})

			if conn.confirmedObject == nil {
				t.Fatal("confirmed object client is nil")
			}
			if got := conn.confirmedObjectStreamEnabled; got != test.wantStream {
				t.Fatalf("confirmed object stream enabled = %t, want %t", got, test.wantStream)
			}
			if conn.interactionStreamEnabled != conn.confirmedObjectStreamEnabled {
				t.Fatalf(
					"interaction and confirmed stream policies differ: interaction=%t confirmed=%t",
					conn.interactionStreamEnabled,
					conn.confirmedObjectStreamEnabled,
				)
			}
			if conn.localLRCEnabled != conn.confirmedObjectStreamEnabled {
				t.Fatalf(
					"LocalLRC and confirmed stream policies differ: local_lrc=%t confirmed=%t",
					conn.localLRCEnabled,
					conn.confirmedObjectStreamEnabled,
				)
			}
		})
	}
}

func TestConnectWithOptionsAdmissionMode(t *testing.T) {
	tests := []struct {
		name          string
		opts          ConnectOptions
		wantTransport ReceiveOrderTransport
		wantLocalLRC  bool
		wantStreams   bool
	}{
		{
			name:          "default is pipelined LocalLRC",
			wantTransport: ReceiveOrderTransportLocalLRC,
			wantLocalLRC:  true,
			wantStreams:   true,
		},
		{
			name:          "explicit pipelined",
			opts:          ConnectOptions{AdmissionMode: AdmissionModePipelined},
			wantTransport: ReceiveOrderTransportLocalLRC,
			wantLocalLRC:  true,
			wantStreams:   true,
		},
		{
			name:          "confirmed debug mode selects the unary confirmed path",
			opts:          ConnectOptions{AdmissionMode: AdmissionModeConfirmed},
			wantTransport: ReceiveOrderTransportConfirmed,
			wantLocalLRC:  false,
			wantStreams:   false,
		},
		{
			name: "confirmed debug mode with dial tuning stays unary",
			opts: ConnectOptions{
				AdmissionMode: AdmissionModeConfirmed,
				ExtraDialOptions: []grpc.DialOption{
					grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
						return nil, context.Canceled
					}),
				},
			},
			wantTransport: ReceiveOrderTransportConfirmed,
			wantLocalLRC:  false,
			wantStreams:   false,
		},
		{
			name:          "legacy ReceiveOrderTransportConfirmed still honored",
			opts:          ConnectOptions{ReceiveOrderTransport: ReceiveOrderTransportConfirmed},
			wantTransport: ReceiveOrderTransportConfirmed,
			wantLocalLRC:  true,
			wantStreams:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn, err := ConnectWithOptions(
				context.Background(),
				"passthrough:///admission-mode-test",
				test.opts,
			)
			if err != nil {
				t.Fatalf("ConnectWithOptions: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })

			if conn.receiveOrderTransport != test.wantTransport {
				t.Fatalf("receive-order transport = %d, want %d",
					conn.receiveOrderTransport, test.wantTransport)
			}
			if conn.localLRCEnabled != test.wantLocalLRC {
				t.Fatalf("localLRCEnabled = %t, want %t",
					conn.localLRCEnabled, test.wantLocalLRC)
			}
			if conn.confirmedObjectStreamEnabled != test.wantStreams ||
				conn.interactionStreamEnabled != test.wantStreams {
				t.Fatalf("stream flags = (confirmed=%t interaction=%t), want both %t",
					conn.confirmedObjectStreamEnabled,
					conn.interactionStreamEnabled,
					test.wantStreams)
			}
		})
	}
}

func TestConnectWithOptionsRejectsInvalidAdmissionMode(t *testing.T) {
	if _, err := ConnectWithOptions(
		context.Background(),
		"passthrough:///admission-mode-test",
		ConnectOptions{AdmissionMode: "turbo"},
	); err == nil {
		t.Fatal("ConnectWithOptions accepted an invalid admission mode")
	}
	if _, err := ConnectWithOptions(
		context.Background(),
		"passthrough:///admission-mode-test",
		ConnectOptions{
			AdmissionMode:         AdmissionModePipelined,
			ReceiveOrderTransport: ReceiveOrderTransportConfirmed,
		},
	); err == nil {
		t.Fatal("ConnectWithOptions accepted pipelined mode with confirmed transport")
	}
}

func TestConnectWithOptionsValidatesLocalLRCBatchSize(t *testing.T) {
	if _, err := ConnectWithOptions(
		context.Background(),
		"passthrough:///local-lrc-batch-test",
		ConnectOptions{LocalLRCBatchSize: 48},
	); err == nil {
		t.Fatal("ConnectWithOptions accepted unsupported LocalLRC batch size")
	}
	conn, err := ConnectWithOptions(
		context.Background(),
		"passthrough:///local-lrc-batch-test",
		ConnectOptions{LocalLRCBatchSize: 256},
	)
	if err != nil {
		t.Fatalf("ConnectWithOptions rejected supported batch size: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if conn.localLRCBatchSize != 256 {
		t.Fatalf("configured batch size = %d, want 256", conn.localLRCBatchSize)
	}
}

// W8 acceptance: first-class Transport tuning must not downgrade
// transport selection — the LocalLRC fast path and the shared streams
// stay enabled with every Transport knob set.
func TestConnectWithOptionsTransportTuningKeepsLocalLRCEnabled(t *testing.T) {
	conn, err := ConnectWithOptions(
		context.Background(),
		"passthrough:///transport-options-test",
		ConnectOptions{Transport: TransportOptions{
			WriteBufferSize:       512 << 10,
			ReadBufferSize:        512 << 10,
			InitialWindowSize:     1 << 20,
			InitialConnWindowSize: 1 << 20,
		}},
	)
	if err != nil {
		t.Fatalf("ConnectWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if !conn.localLRCEnabled {
		t.Fatal("localLRCEnabled = false with Transport options set, want true")
	}
	if !conn.interactionStreamEnabled || !conn.confirmedObjectStreamEnabled {
		t.Fatalf("stream flags = (interaction=%t confirmed=%t) with Transport options set, want both true",
			conn.interactionStreamEnabled, conn.confirmedObjectStreamEnabled)
	}
}

// W8 acceptance: the zero-value Transport appends NO DialOptions, so
// dial behavior is byte-identical to earlier SDKs; each set knob
// appends exactly one first-class DialOption.
func TestTransportOptionsDialOptions(t *testing.T) {
	if got := (TransportOptions{}).dialOptions(); len(got) != 0 {
		t.Fatalf("zero-value TransportOptions appended %d DialOption(s), want 0", len(got))
	}
	full := TransportOptions{
		WriteBufferSize:       512 << 10,
		ReadBufferSize:        256 << 10,
		InitialWindowSize:     1 << 20,
		InitialConnWindowSize: 2 << 20,
	}
	if got := full.dialOptions(); len(got) != 4 {
		t.Fatalf("fully-set TransportOptions appended %d DialOption(s), want 4", len(got))
	}
}

func TestConnectWithOptionsRejectsNegativeTransportOptions(t *testing.T) {
	for name, tr := range map[string]TransportOptions{
		"write buffer": {WriteBufferSize: -1},
		"read buffer":  {ReadBufferSize: -1},
		"window":       {InitialWindowSize: -1},
		"conn window":  {InitialConnWindowSize: -1},
	} {
		if _, err := ConnectWithOptions(
			context.Background(),
			"passthrough:///transport-options-test",
			ConnectOptions{Transport: tr},
		); err == nil {
			t.Fatalf("ConnectWithOptions accepted negative %s", name)
		}
	}
}

func TestConnectWithOptionsValidatesCallbackRepresentation(t *testing.T) {
	if _, err := ConnectWithOptions(
		context.Background(),
		"passthrough:///callback-representation-test",
		ConnectOptions{CallbackRepresentation: CallbackRepresentationHandles + 1},
	); err == nil {
		t.Fatal("ConnectWithOptions accepted an invalid callback representation")
	}
	conn, err := ConnectWithOptions(
		context.Background(),
		"passthrough:///callback-representation-test",
		ConnectOptions{CallbackRepresentation: CallbackRepresentationHandles},
	)
	if err != nil {
		t.Fatalf("ConnectWithOptions rejected handle callbacks: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if conn.callbackRepresentation != CallbackRepresentationHandles {
		t.Fatalf("callback representation = %d, want handles", conn.callbackRepresentation)
	}
}
