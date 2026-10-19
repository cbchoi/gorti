package server

import (
	"context"
	"flag"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// A separate process executes Main's real flag/config resolution rather than
// bypassing it with a manually assembled rtidConfig.
func TestProductionIntegerActualCLIResolvedDefaults(t *testing.T) {
	if os.Getenv("GORTI_INTEGER_CLI_CHILD") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
				break
			}
		}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		Main()
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	directory := t.TempDir()
	output, err := os.Create(filepath.Join(directory, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestProductionIntegerActualCLIResolvedDefaults$", "--", "--listen="+address, "--admin-listen=", "--metrics-listen=127.0.0.1:0", "--save-dir="+filepath.Join(directory, "saves"), "--log-level=error")
	child.Env = append(os.Environ(), "GORTI_INTEGER_CLI_CHILD=1", "GORTI_RESEARCH_CONFIG=", "GORTI_DETERMINISM=")
	child.Stdout, child.Stderr = output, output
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	conn, err := grpcgo.NewClient(address, grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := rtiv1.NewFederationServiceClient(conn)
	for _, factory := range []string{"", integer64TimeName} {
		result, err := client.CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: "cli-" + factory, LogicalTimeImplementationName: factory, FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}}, grpcgo.WaitForReady(true))
		if err != nil {
			_, _ = output.Seek(0, io.SeekStart)
			logs, _ := io.ReadAll(output)
			t.Fatalf("actual CLI factory %q failed: %v; log=%s", factory, err, logs)
		}
		want := factory
		if want == "" {
			want = float64TimeName
		}
		if result.LogicalTimeImplementationName != want {
			t.Fatalf("actual CLI selected %q, want %q", result.LogicalTimeImplementationName, want)
		}
	}
}
