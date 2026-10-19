package savepoint

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func runtimeRebindFixture[T core.LogicalTimeScalar]() (*ManagerOf[T], ManifestOf[T]) {
	sourceGeneration := uint64(1)
	source := ManifestOf[T]{
		Version: 2, Federation: testFed, Label: testRuntime, FederationGeneration: &sourceGeneration,
		Federates:            []core.FederateHandle{1<<32 | 1, 1<<32 | 2},
		FederateNames:        []string{"one", "two"},
		FederateClientStates: [][]byte{[]byte("go:source"), []byte("python:source")},
		ManagerSnapshots:     map[string][]byte{"fom": []byte("saved-fom")},
	}
	m := &ManagerOf[T]{opts: OptionsOf[T]{
		Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{2<<32 | 1, 2<<32 | 2} },
		Roster: func(core.FederationName) []core.FederationMember {
			return []core.FederationMember{{Handle: 2<<32 | 1, Name: "one"}, {Handle: 2<<32 | 2, Name: "two"}}
		},
		RequiredSnapshots: []string{"fom"},
		PrepareRestoreTransition: func(context.Context, core.FederationName, []RestoreParticipant, []core.OutboxDelivery) (core.OutboxReservation, error) {
			return nil, errors.New("prepare must not publish")
		},
		ValidateRestoreSnapshots: func(core.FederationName, ManifestOf[T], core.FOMHandle) error { return nil },
		RebindRestore: func(_ context.Context, _ core.FederationName, target uint64, in ManifestOf[T]) (ManifestOf[T], error) {
			in.FederationGeneration = &target
			in.Federates = []core.FederateHandle{2<<32 | 9, 2<<32 | 10}
			return in, nil
		},
		ValidateClientState: func(context.Context, ClientStateValidationRequest) error { return nil },
		RebindClientState: func(_ context.Context, in ClientStateRebindRequest) ([]byte, error) {
			return append(in.State, []byte(":target")...), nil
		},
	}}
	return m, source
}

func TestClientStateRebindTypedPreflightOrderingAndIsolation(t *testing.T) {
	t.Run("float64", func(t *testing.T) { testRuntimeRebindOrdering[float64](t) })
	t.Run("integer64", func(t *testing.T) { testRuntimeRebindOrdering[int64](t) })
}

func testRuntimeRebindOrdering[T core.LogicalTimeScalar](t *testing.T) {
	for _, language := range []string{"go", "python", "java"} {
		t.Run(language, func(t *testing.T) {
			m, manifest := runtimeRebindFixture[T]()
			manifest.FederateClientStates[0] = []byte(language + ":source")
			before := append([]byte(nil), manifest.FederateClientStates[0]...)
			var calls []string
			m.opts.ValidateClientState = func(ctx context.Context, in ClientStateValidationRequest) error {
				calls = append(calls, "validate")
				if ctx == nil || in.Federation != testFed || in.Label != testRuntime || in.SourceGeneration != 1 || uint64(in.SourceHandle)>>32 != 1 {
					t.Fatalf("bad validation context: %+v", in)
				}
				in.State[0] = 'X'
				return nil
			}
			graph := m.opts.RebindRestore
			m.opts.RebindRestore = func(ctx context.Context, fed core.FederationName, generation uint64, in ManifestOf[T]) (ManifestOf[T], error) {
				calls = append(calls, "graph")
				in.FederateClientStates[0][0] = 'Z'
				return graph(ctx, fed, generation, in)
			}
			var retained []byte
			m.opts.RebindClientState = func(_ context.Context, in ClientStateRebindRequest) ([]byte, error) {
				calls = append(calls, "rewrite")
				if in.SourceGeneration != 1 || in.TargetGeneration != 2 || in.Federation != testFed || in.Label != testRuntime ||
					in.TargetHandle != in.FederateHandles[in.SourceHandle] || len(in.FederateHandles) != 2 {
					t.Fatalf("bad rewrite context: %+v", in)
				}
				if in.SourceHandle == 1<<32|1 && !bytes.Equal(in.State, before) {
					t.Fatal("preflight or graph mutated source runtime bytes")
				}
				if in.FederateHandles[1<<32|1] != 2<<32|9 {
					t.Fatal("previous invocation mutated the shared participant map")
				}
				in.FederateHandles[1<<32|1] = 0
				out := in.State
				out = append(out, []byte(":target")...)
				if in.SourceHandle == 1<<32|1 {
					retained = out
				}
				return out, nil
			}
			ar := &activeRestoreOf[T]{}
			if err := m.prepareCrossGeneration(context.Background(), testFed, 2, "target-fom", &manifest, ar); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, []string{"validate", "validate", "graph", "rewrite", "rewrite"}) {
				t.Fatalf("preflight did not precede handle reservation: %v", calls)
			}
			retained[0] = 'Q'
			if string(manifest.FederateClientStates[0]) != language+":source:target" || string(manifest.FederateClientStates[1]) != "python:source:target" {
				t.Fatalf("runtime result aliases adapter state: %q", manifest.FederateClientStates)
			}
			if *ar.targetGeneration != 2 || ar.targetFOMSHA256 != "target-fom" {
				t.Fatal("target provenance not pinned")
			}
		})
	}
}

func TestClientStateRebindRejectedPreflightNeverAllocatesHandles(t *testing.T) {
	for _, failure := range []string{"unknown-second", "missing-validator", "missing-rebinder", "roster-shape", "oversized-source", "missing-generation", "wrong-source-generation"} {
		t.Run(failure, func(t *testing.T) {
			m, manifest := runtimeRebindFixture[float64]()
			m.opts.RebindRestore = func(context.Context, core.FederationName, uint64, ManifestOf[float64]) (ManifestOf[float64], error) {
				t.Fatal("invalid source reached handle reservation")
				return ManifestOf[float64]{}, nil
			}
			cause := errors.New("unknown runtime format")
			m.opts.ValidateClientState = func(_ context.Context, in ClientStateValidationRequest) error {
				in.State[0] = 'X'
				if in.SourceHandle == 1<<32|2 {
					return cause
				}
				return nil
			}
			switch failure {
			case "missing-validator":
				m.opts.ValidateClientState = nil
			case "missing-rebinder":
				m.opts.RebindClientState = nil
			case "roster-shape":
				manifest.FederateClientStates = manifest.FederateClientStates[:1]
			case "oversized-source":
				manifest.FederateClientStates[1] = make([]byte, MaxClientStateBytes+1)
			case "missing-generation":
				manifest.FederationGeneration = nil
			case "wrong-source-generation":
				manifest.Federates[1] = 3<<32 | 1
			}
			original := string(manifest.FederateClientStates[0])
			ar := &activeRestoreOf[float64]{}
			err := m.prepareCrossGeneration(context.Background(), testFed, 2, "digest", &manifest, ar)
			if !errors.Is(err, ErrSaveBundleIncompatible) || string(manifest.FederateClientStates[0]) != original || ar.targetGeneration != nil {
				t.Fatalf("preflight failed non-atomically: manifest=%+v restore=%+v err=%v", manifest, ar, err)
			}
			if failure == "unknown-second" && !errors.Is(err, cause) {
				t.Fatalf("adapter error lost: %v", err)
			}
		})
	}
}

func TestClientStateRebindRejectsRewriteWithoutReplacingSource(t *testing.T) {
	for _, failure := range []string{"error-second", "empty-result", "oversized-result", "duplicate-target", "graph-mutates-source"} {
		t.Run(failure, func(t *testing.T) {
			m, manifest := runtimeRebindFixture[int64]()
			original := manifest.Federates[0]
			m.opts.RebindClientState = func(_ context.Context, in ClientStateRebindRequest) ([]byte, error) {
				in.State[0] = 'X'
				switch failure {
				case "error-second":
					if in.SourceHandle == 1<<32|2 {
						return nil, errors.New("bad payload")
					}
				case "empty-result":
					return nil, nil
				case "oversized-result":
					return make([]byte, MaxClientStateBytes+1), nil
				}
				return []byte("translated"), nil
			}
			if failure == "duplicate-target" || failure == "graph-mutates-source" {
				m.opts.RebindRestore = func(_ context.Context, _ core.FederationName, target uint64, in ManifestOf[int64]) (ManifestOf[int64], error) {
					in.FederationGeneration = &target
					in.Federates[0], in.Federates[1] = 2<<32|9, 2<<32|9
					if failure == "graph-mutates-source" {
						in.FederateNames[0] = testChanged
					}
					return in, nil
				}
			}
			ar := &activeRestoreOf[int64]{}
			err := m.prepareCrossGeneration(context.Background(), testFed, 2, "digest", &manifest, ar)
			if !errors.Is(err, ErrSaveBundleIncompatible) || manifest.Federates[0] != original || manifest.FederateNames[0] != "one" || string(manifest.FederateClientStates[0]) != "go:source" || ar.targetGeneration != nil {
				t.Fatalf("rewrite failure mutated source: %+v err=%v", manifest, err)
			}
		})
	}
}

func TestClientStateRebindEmptyLegacyBypassesPorts(t *testing.T) {
	for _, states := range [][][]byte{nil, {nil, {}}} {
		m, manifest := runtimeRebindFixture[float64]()
		manifest.FederateClientStates = states
		m.opts.ValidateClientState, m.opts.RebindClientState = nil, nil
		if err := m.prepareCrossGeneration(context.Background(), testFed, 2, "digest", &manifest, &activeRestoreOf[float64]{}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(manifest.FederateClientStates, states) {
			t.Fatal("legacy state shape changed")
		}
	}
}

const testChanged = "changed"

const testRuntime = "runtime"
