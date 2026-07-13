package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const generationEpochHelperModeEnv = "GORTI_GENERATION_EPOCH_HELPER_MODE"

type generationEpochChild struct {
	cmd    *exec.Cmd
	output *bytes.Buffer
	waited bool
	ready  string
	result string
}

func TestGenerationEpochConcurrentProcessesReserveDisjointBlocks(t *testing.T) {
	const childCount = 12
	dir := t.TempDir()
	gate := filepath.Join(dir, "start")
	children := make([]*generationEpochChild, 0, childCount)
	defer func() { stopGenerationEpochChildren(children) }()

	for i := 0; i < childCount; i++ {
		child := startGenerationEpochChild(
			t,
			"allocate",
			dir,
			gate,
			filepath.Join(dir, "ready-"+strconv.Itoa(i)),
			filepath.Join(dir, "result-"+strconv.Itoa(i)),
		)
		children = append(children, child)
	}
	waitForGenerationEpochChildren(t, children)
	if err := os.WriteFile(gate, []byte("start\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		if err := child.cmd.Wait(); err != nil {
			child.waited = true
			t.Fatalf("generation epoch helper failed: %v\n%s", err, child.output.String())
		}
		child.waited = true
	}

	epochs := make([]uint32, 0, childCount)
	for _, child := range children {
		contents, err := os.ReadFile(child.result)
		if err != nil {
			t.Fatal(err)
		}
		epoch, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		epochs = append(epochs, uint32(epoch))
	}
	sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })
	for i, epoch := range epochs {
		want := uint32(1) + uint32(i)*generationReservationSpan
		if epoch != want {
			t.Fatalf("sorted epoch[%d] = %d, want %d; all epochs: %v", i, epoch, want, epochs)
		}
	}
	if got, want := readGenerationEpochHighWater(t, dir), uint32(childCount)*generationReservationSpan; got != want {
		t.Fatalf("published high-water = %d, want %d", got, want)
	}
}

func TestGenerationEpochLockReleasedAfterAbruptProcessExit(t *testing.T) {
	dir := t.TempDir()
	gate := filepath.Join(dir, "exit")
	child := startGenerationEpochChild(
		t,
		"exit-with-lock",
		dir,
		gate,
		filepath.Join(dir, "ready"),
		"",
	)
	children := []*generationEpochChild{child}
	defer stopGenerationEpochChildren(children)
	waitForGenerationEpochChildren(t, children)
	if err := os.WriteFile(gate, []byte("exit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := child.cmd.Wait(); err != nil {
		child.waited = true
		t.Fatalf("generation epoch crash helper failed: %v\n%s", err, child.output.String())
	}
	child.waited = true

	type allocationResult struct {
		epoch uint32
		err   error
	}
	result := make(chan allocationResult, 1)
	go func() {
		epoch, err := nextFederationGenerationEpoch(dir)
		result <- allocationResult{epoch: epoch, err: err}
	}()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.epoch != 1 {
			t.Fatalf("epoch after lock holder exit = %d, want 1", got.epoch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("generation epoch lock remained held after its process exited")
	}
}

func TestGenerationEpochInterruptedPublicationPreservesCommittedHighWater(t *testing.T) {
	dir := t.TempDir()
	first, err := nextFederationGenerationEpoch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 {
		t.Fatalf("first epoch = %d, want 1", first)
	}

	// Simulate a process exiting after flushing its temporary file but before
	// the atomic replacement publishes it.
	orphan, err := os.CreateTemp(dir, ".federation-generation-epoch-*")
	if err != nil {
		t.Fatal(err)
	}
	orphanPath := orphan.Name()
	defer func() { _ = os.Remove(orphanPath) }()
	if _, err := orphan.WriteString(strconv.FormatUint(uint64(2*generationReservationSpan), 10) + "\n"); err != nil {
		_ = orphan.Close()
		t.Fatal(err)
	}
	if err := orphan.Sync(); err != nil {
		_ = orphan.Close()
		t.Fatal(err)
	}
	if err := orphan.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readGenerationEpochHighWater(t, dir); got != generationReservationSpan {
		t.Fatalf("high-water changed before publication: got %d, want %d", got, generationReservationSpan)
	}

	second, err := nextFederationGenerationEpoch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second != first+generationReservationSpan {
		t.Fatalf("second epoch = %d, want %d", second, first+generationReservationSpan)
	}
	if got, want := readGenerationEpochHighWater(t, dir), 2*generationReservationSpan; got != want {
		t.Fatalf("published high-water = %d, want %d", got, want)
	}
}

func TestGenerationEpochSubprocessHelper(t *testing.T) {
	mode := os.Getenv(generationEpochHelperModeEnv)
	if mode == "" {
		return
	}
	dir := os.Getenv("GORTI_GENERATION_EPOCH_HELPER_DIR")
	gate := os.Getenv("GORTI_GENERATION_EPOCH_HELPER_GATE")
	ready := os.Getenv("GORTI_GENERATION_EPOCH_HELPER_READY")

	switch mode {
	case "allocate":
		markGenerationEpochHelperReady(t, ready)
		waitForGenerationEpochGate(t, gate)
		epoch, err := nextFederationGenerationEpoch(dir)
		if err != nil {
			t.Fatal(err)
		}
		result := os.Getenv("GORTI_GENERATION_EPOCH_HELPER_RESULT")
		if err := os.WriteFile(result, []byte(strconv.FormatUint(uint64(epoch), 10)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	case "exit-with-lock":
		release, err := lockFederationGenerationEpoch(filepath.Join(dir, federationGenerationEpochFilename) + ".lock")
		if err != nil {
			t.Fatal(err)
		}
		markGenerationEpochHelperReady(t, ready)
		waitForGenerationEpochGate(t, gate)
		runtime.KeepAlive(release)
		os.Exit(0)
	default:
		t.Fatalf("unknown generation epoch helper mode %q", mode)
	}
}

func startGenerationEpochChild(t *testing.T, mode, dir, gate, ready, result string) *generationEpochChild {
	t.Helper()
	output := new(bytes.Buffer)
	cmd := exec.Command(os.Args[0], "-test.run=^TestGenerationEpochSubprocessHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		generationEpochHelperModeEnv+"="+mode,
		"GORTI_GENERATION_EPOCH_HELPER_DIR="+dir,
		"GORTI_GENERATION_EPOCH_HELPER_GATE="+gate,
		"GORTI_GENERATION_EPOCH_HELPER_READY="+ready,
		"GORTI_GENERATION_EPOCH_HELPER_RESULT="+result,
	)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return &generationEpochChild{cmd: cmd, output: output, ready: ready, result: result}
}

func waitForGenerationEpochChildren(t *testing.T, children []*generationEpochChild) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		allReady := true
		for _, child := range children {
			if _, err := os.Stat(child.ready); err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
				allReady = false
			}
		}
		if allReady {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for generation epoch helper processes")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func markGenerationEpochHelperReady(t *testing.T, ready string) {
	t.Helper()
	if err := os.WriteFile(ready, []byte("ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForGenerationEpochGate(t *testing.T, gate string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(gate); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for generation epoch helper gate")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func stopGenerationEpochChildren(children []*generationEpochChild) {
	for _, child := range children {
		if child.waited || child.cmd.Process == nil {
			continue
		}
		_ = child.cmd.Process.Kill()
		_ = child.cmd.Wait()
		child.waited = true
	}
}

func readGenerationEpochHighWater(t *testing.T, dir string) uint32 {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(dir, federationGenerationEpochFilename))
	if err != nil {
		t.Fatal(err)
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	return uint32(value)
}
