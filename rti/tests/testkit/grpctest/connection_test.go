package grpctest

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

func TestDialReadyConnectsToServingEndpoint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	Serve(t, grpc.NewServer(), listener)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := DialReady(ctx, listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close connection: %v", err)
		}
	}()
	if state := conn.GetState(); state != connectivity.Ready {
		t.Fatalf("connection state = %v, want Ready", state)
	}
}

func TestDialReadyHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn, err := DialReady(ctx, "127.0.0.1:0")
	if conn != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("connection = %v, error = %v; want nil and canceled", conn, err)
	}
}
