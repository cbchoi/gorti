//go:build windows && runtime_checkpoint_live

package federate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
)

func startSDKRuntimeServer(t *testing.T, binary, directory string) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	log, err := os.Create(filepath.Join(directory, fmt.Sprintf("server-%s.log", address[len("127.0.0.1:"):])))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-listen", address, "-admin-listen=", "-metrics-listen=", "-save-dir", filepath.Join(directory, "saves"), "-state-dir", filepath.Join(directory, "state"), "-enable-integer64=true")
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { _ = command.Process.Kill(); _ = command.Wait(); _ = log.Close() }) }
	t.Cleanup(stop)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return address, stop
		}
		time.Sleep(25 * time.Millisecond)
	}
	stop()
	t.Fatal("actual RTI server did not bind its TCP address")
	return "", stop
}

func waitSDKRuntimeEvent(t *testing.T, ctx context.Context, f *Federate, match func(Event) bool) Event {
	t.Helper()
	for {
		select {
		case event, open := <-f.Events():
			if !open {
				t.Fatal("actual SDK event stream closed")
			}
			if failure, ok := event.(RestoreStateError); ok {
				t.Fatal(failure)
			}
			if match(event) {
				return event
			}
			if _, ok := event.(ReflectAttributeValues); ok {
				t.Fatalf("unexpected data before selected callback: %T", event)
			}
			if callbackErr, reportErr := f.DispatchEvent(ctx, event, func(Event) error { return nil }); callbackErr != nil || reportErr != nil && reportErr != ErrCallbackReceiptUnavailable {
				t.Fatalf("control dispatch %T: %v / %v", event, callbackErr, reportErr)
			}
		case <-ctx.Done():
			t.Fatalf("waiting for actual callback: %v", ctx.Err())
		}
	}
}

func dispatchSDKRuntimeControl(t *testing.T, ctx context.Context, f *Federate, event Event) {
	t.Helper()
	if callbackErr, reportErr := f.DispatchEvent(ctx, event, func(Event) error { return nil }); callbackErr != nil || reportErr != nil {
		t.Fatalf("selected control dispatch %T: %v / %v", event, callbackErr, reportErr)
	}
}

func TestGoSDKCrossGenerationRuntimeActualServer(t *testing.T) {
	binary := os.Getenv("GORTI_GO_RUNTIME_LIVE_SERVER")
	if binary == "" {
		t.Fatal("GORTI_GO_RUNTIME_LIVE_SERVER must identify the assessed executable")
	}
	image, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(image)
	t.Logf("actual server: %s SHA256=%s", binary, hex.EncodeToString(digest[:]))
	for _, scalar := range []string{HLAfloat64Time, HLAinteger64Time} {
		t.Run(scalar, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			directory := t.TempDir()
			if root := os.Getenv("GORTI_GO_RUNTIME_LIVE_DIR"); root != "" {
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				directory, err = os.MkdirTemp(root, scalar+"-")
				if err != nil {
					t.Fatal(err)
				}
			}
			module, err := os.ReadFile(filepath.Join("..", "fom", "exporter", "testdata", "retained.xml"))
			if err != nil {
				t.Fatal(err)
			}
			spec := FederationSpec{Name: "go-runtime-" + scalar, FOMModules: []FOMModule{{Path: "runtime.xml", XML: module}}, LogicalTimeImplementationName: scalar}
			address, stop := startSDKRuntimeServer(t, binary, directory)
			connection, err := ConnectWithOptions(ctx, address, ConnectOptions{ReceiveOrderTransport: ReceiveOrderTransportConfirmed})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = connection.Close() })
			publisher, err := connection.JoinFederation(ctx, spec, "publisher")
			if err != nil {
				t.Fatal(err)
			}
			receiver, err := connection.JoinFederation(ctx, spec, "receiver")
			if err != nil {
				t.Fatal(err)
			}
			if err := publisher.PublishObjectClassAttributes(ctx, "HLAobjectRoot.Parent.Leaf", []string{"Local"}); err != nil {
				t.Fatal(err)
			}
			if err := receiver.SubscribeObjectClassAttributes(ctx, "HLAobjectRoot.Parent.Leaf", []string{"Local"}); err != nil {
				t.Fatal(err)
			}
			object, err := publisher.RegisterObjectInstance(ctx, "HLAobjectRoot.Parent.Leaf", "")
			if err != nil {
				t.Fatal(err)
			}
			discovery := waitSDKRuntimeEvent(t, ctx, receiver, func(e Event) bool { _, ok := e.(DiscoverObjectInstance); return ok })
			lookahead, _ := logicaltime.Float64Interval(1)
			at, _ := logicaltime.Float64(math.Nextafter(2, 3))
			if scalar == HLAinteger64Time {
				lookahead, _ = logicaltime.Integer64Interval(1)
				at, _ = logicaltime.Integer64(1<<53 + 1)
			}
			if err := publisher.EnableTimeRegulationValue(ctx, lookahead); err != nil {
				t.Fatal(err)
			}
			regulation := waitSDKRuntimeEvent(t, ctx, publisher, func(e Event) bool { _, ok := e.(TimeRegulationEnabled); return ok })
			if a, r := publisher.DispatchEvent(ctx, regulation, func(Event) error { return nil }); a != nil || r != nil {
				t.Fatalf("regulation dispatch: %v/%v", a, r)
			}
			payload := []byte{0, 255, 17}
			var old []Event
			for i := uint64(501); i <= 502; i++ {
				if err := publisher.UpdateAttributeValuesValue(ctx, object, map[string][]byte{"Local": payload}, &at, MessageOptions{Tag: []byte("equal"), RetractionHandle: i}); err != nil {
					t.Fatal(err)
				}
				event := waitSDKRuntimeEvent(t, ctx, receiver, func(e Event) bool { _, ok := e.(ReflectAttributeValues); return ok })
				old = append(old, event)
			}
			oldPublisher, oldReceiver, generation := publisher.Handle(), receiver.Handle(), receiver.federationGeneration
			state := runtimeTestState(t, receiver)
			if len(state.PendingData) != 3 || len(state.Callbacks) != 0 || len(state.Objects) == 0 {
				t.Fatalf("nonempty pending discovery/reflection state: %+v", state)
			}
			if err := publisher.RequestFederationSave(ctx, "nonempty", nil); err != nil {
				t.Fatal(err)
			}
			for _, f := range []*Federate{publisher, receiver} {
				initiate := waitSDKRuntimeEvent(t, ctx, f, func(e Event) bool { _, ok := e.(InitiateFederateSave); return ok })
				dispatchSDKRuntimeControl(t, ctx, f, initiate)
				if err := f.FederateSaveBegun(ctx); err != nil {
					t.Fatal(err)
				}
				if err := f.FederateSaveComplete(ctx); err != nil {
					t.Fatal(err)
				}
			}
			for _, f := range []*Federate{publisher, receiver} {
				saved := waitSDKRuntimeEvent(t, ctx, f, func(e Event) bool { _, ok := e.(FederationSaved); return ok })
				dispatchSDKRuntimeControl(t, ctx, f, saved)
			}
			_ = connection.Close()
			stop()
			address, _ = startSDKRuntimeServer(t, binary, directory)
			target, err := ConnectWithOptions(ctx, address, ConnectOptions{ReceiveOrderTransport: ReceiveOrderTransportConfirmed})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = target.Close() })
			restoredReceiver, err := target.JoinFederation(ctx, spec, "receiver")
			if err != nil {
				t.Fatal(err)
			}
			restoredPublisher, err := target.JoinFederation(ctx, spec, "publisher")
			if err != nil {
				t.Fatal(err)
			}
			if restoredReceiver.federationGeneration == generation {
				t.Fatal("execution generation was not recreated")
			}
			if err := restoredPublisher.RequestFederationRestore(ctx, "nonempty"); err != nil {
				t.Fatal(err)
			}
			for _, f := range []*Federate{restoredPublisher, restoredReceiver} {
				event := waitSDKRuntimeEvent(t, ctx, f, func(e Event) bool { _, ok := e.(InitiateFederateRestore); return ok }).(InitiateFederateRestore)
				if len(event.ClientState) == 0 || f.runtimeStaged == nil {
					t.Fatal("server did not return recognized nonempty SDK state")
				}
				dispatchSDKRuntimeControl(t, ctx, f, event)
			}
			for _, f := range []*Federate{restoredPublisher, restoredReceiver} {
				if err := f.FederateRestoreComplete(ctx); err != nil {
					t.Fatal(err)
				}
			}
			for _, f := range []*Federate{restoredPublisher, restoredReceiver} {
				restored := waitSDKRuntimeEvent(t, ctx, f, func(e Event) bool { _, ok := e.(FederationRestored); return ok })
				dispatchSDKRuntimeControl(t, ctx, f, restored)
			}
			if restoredPublisher.Handle() == oldPublisher || restoredReceiver.Handle() == oldReceiver || restoredReceiver.CallbackEpoch() == 0 {
				t.Fatal("restore did not fence source identities")
			}
			for _, event := range append([]Event{discovery}, old...) {
				invoked := false
				_, err := restoredReceiver.DispatchEvent(ctx, event, func(Event) error { invoked = true; return nil })
				if err == nil || invoked {
					t.Fatal("old raw callback crossed recreated execution")
				}
			}
			newDiscovery := waitSDKRuntimeEvent(t, ctx, restoredReceiver, func(e Event) bool { _, ok := e.(DiscoverObjectInstance); return ok })
			if bytes.Equal(newDiscovery.(DiscoverObjectInstance).CallbackReceipt(), discovery.(DiscoverObjectInstance).CallbackReceipt()) {
				t.Fatal("discovery replay reused old receipt")
			}
			if a, r := restoredReceiver.DispatchEvent(ctx, newDiscovery, func(Event) error { return nil }); a != nil || r != nil {
				t.Fatalf("fresh discovery dispatch %v/%v", a, r)
			}
			invocations := 0
			for i := 0; i < 2; i++ {
				event := waitSDKRuntimeEvent(t, ctx, restoredReceiver, func(e Event) bool { _, ok := e.(ReflectAttributeValues); return ok }).(ReflectAttributeValues)
				previous := old[i].(ReflectAttributeValues)
				if bytes.Equal(event.CallbackReceipt(), previous.CallbackReceipt()) || event.TimestampValue == nil || *event.TimestampValue != at || !bytes.Equal(event.Attributes["Local"], payload) {
					t.Fatal("fresh replay changed capability, exact time, or payload")
				}
				callback := func(Event) error { invocations++; return nil }
				for retry := 0; retry < 2; retry++ {
					if a, r := restoredReceiver.DispatchEvent(ctx, event, callback); a != nil || r != nil {
						t.Fatalf("replayed data dispatch %v/%v", a, r)
					}
				}
			}
			if invocations != 2 {
				t.Fatal("equal payloads collapsed or duplicate dispatch reinvoked")
			}
			if _, ok := restoredPublisher.objectClassFor(object); !ok {
				t.Fatal("restored publisher lost registered object knowledge")
			}
			if _, ok := restoredReceiver.objectClassFor(object); !ok {
				t.Fatal("restored receiver lost discovered object knowledge")
			}
			if next, known := restoredPublisher.NextRetractionHandle(); !known || next <= 502 {
				t.Fatalf("restored retraction identity recommendation=%d/%v", next, known)
			}
			select {
			case e := <-restoredReceiver.Events():
				t.Fatalf("duplicate local/server replay: %T", e)
			case <-time.After(150 * time.Millisecond):
			}
			t.Logf("PASS source_generation=%d target_generation=%d source_handles=%d,%d installed_handles=%d,%d exact_time=%v pending_data=3 actual_invocations=3", generation, restoredReceiver.federationGeneration, oldPublisher, oldReceiver, restoredPublisher.Handle(), restoredReceiver.Handle(), at)
		})
	}
}
