// Package grpctest owns gRPC connection and server lifecycles for integration tests.
package grpctest

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

// Serve waits for the serving goroutine during cleanup and checks its result.
func Serve(t testing.TB, server *grpc.Server, listener net.Listener) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-done; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("gRPC server: %v", err)
		}
	})
}

// DialReady preserves a bounded connection-readiness check for real TCP tests.
func DialReady(ctx context.Context, target string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient("passthrough:///"+target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	conn.Connect()
	for state := conn.GetState(); state != connectivity.Ready; state = conn.GetState() {
		if !conn.WaitForStateChange(ctx, state) {
			return nil, errors.Join(ctx.Err(), conn.Close())
		}
	}
	return conn, nil
}
