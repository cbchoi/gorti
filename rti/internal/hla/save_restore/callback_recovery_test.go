package savepoint

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type recoveryWriterSpy struct {
	*memStore
	writes int
}

func (s *recoveryWriterSpy) WriterFor(key StorageKey) (BundleWriter, error) {
	s.writes++
	return s.memStore.WriterFor(key)
}

func TestCallbackRecoverySavePreflightNeverOpensWriterOrOverwrites(t *testing.T) {
	t.Run("float64", func(t *testing.T) { testCallbackRecoverySavePreflight[float64](t, "HLAfloat64Time") })
	t.Run("integer64", func(t *testing.T) { testCallbackRecoverySavePreflight[int64](t, "HLAinteger64Time") })
}

func testCallbackRecoverySavePreflight[T core.LogicalTimeScalar](t *testing.T, scalar string) {
	ctx := context.Background()
	generation := uint64(1)
	store := &recoveryWriterSpy{memStore: newMemStore()}
	manifest := ManifestOf[T]{Version: 2, Federation: testFed, Label: "saved", FederationGeneration: &generation, LogicalTimeImplementationName: scalar, Federates: []core.FederateHandle{1<<32 | 1}, FederateClientStates: [][]byte{[]byte("known-finished-outcome")}, ManagerSnapshots: map[string][]byte{"callback_pending": []byte("source-outcome-row")}}
	m := &ManagerOf[T]{opts: OptionsOf[T]{BundleStore: store}}
	if err := m.writeBundle(ctx, testFed, "saved", &generation, manifest); err != nil {
		t.Fatal(err)
	}
	key := StorageKey{Federation: testFed, Generation: 1, Label: "saved"}
	read := func() []byte {
		r, err := store.ReaderFor(key)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	before := read()
	unknown := errors.New("raw client entered body without known outcome")
	checks := 0
	m.opts.ValidateRestoreCallbackRecovery = func(_ context.Context, _ core.FederationName, in ManifestOf[T]) error {
		checks++
		if *in.FederationGeneration != 1 || in.Federates[0] != 1<<32|1 || string(in.FederateClientStates[0]) != "known-finished-outcome" {
			t.Fatal("save preflight did not receive source manifest")
		}
		in.FederateClientStates[0][0] = 'X'
		in.ManagerSnapshots["callback_pending"][0] = 'X'
		return unknown
	}
	if err := m.writeBundle(ctx, testFed, "saved", &generation, manifest); !errors.Is(err, unknown) {
		t.Fatalf("unknown source result accepted: %v", err)
	}
	if store.writes != 1 || checks != 1 || !bytes.Equal(before, read()) {
		t.Fatalf("failed preflight touched committed bundle: writes=%d checks=%d", store.writes, checks)
	}
	if string(manifest.FederateClientStates[0]) != "known-finished-outcome" || string(manifest.ManagerSnapshots["callback_pending"]) != "source-outcome-row" {
		t.Fatal("save preflight aliased source payloads")
	}
}

func TestCallbackRecoveryPreflightPrecedesRestorePreparation(t *testing.T) {
	t.Run("float64", func(t *testing.T) { testCallbackRecoveryPreflight[float64](t, "HLAfloat64Time") })
	t.Run("integer64", func(t *testing.T) { testCallbackRecoveryPreflight[int64](t, "HLAinteger64Time") })
}

func testCallbackRecoveryPreflight[T core.LogicalTimeScalar](t *testing.T, scalar string) {
	for _, target := range []uint64{1, 2} {
		t.Run(map[uint64]string{1: "same-generation", 2: "cross-generation"}[target], func(t *testing.T) {
			ctx := context.Background()
			store := newMemStore()
			generation := uint64(1)
			manifest := ManifestOf[T]{Version: 2, Federation: testFed, Label: "saved", FederationGeneration: &generation, FOMSHA256: testFOMDigest, LogicalTimeImplementationName: scalar, Federates: []core.FederateHandle{1<<32 | 1}, FederateNames: []string{"one"}, FederateClientStates: [][]byte{[]byte("source-runtime")}, ManagerSnapshots: map[string][]byte{"callback_pending": []byte("source-ledger")}}
			writer, err := store.WriterFor(StorageKey{Federation: testFed, Generation: 1, Label: "saved"})
			if err != nil {
				t.Fatal(err)
			}
			if err = WriteBundleOf(writer, manifest, nil); err != nil {
				t.Fatal(err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			snapshot := &fakeSnapshotter{}
			graphCalls, preflightCalls := 0, 0
			unknown := errors.New("entered callback result unknown")
			mgr, err := NewOf(OptionsOf[T]{Outbox: &fakeOutbox{}, BundleStore: store, Members: func(core.FederationName) []core.FederateHandle {
				return []core.FederateHandle{core.FederateHandle(target<<32 | 1)}
			}, Generation: func(core.FederationName) (uint64, bool) { return target, true }, FOMSHA256: func(core.FederationName) (string, bool) { return testFOMDigest, true }, ManagerSnapshots: map[string]ManagerSnapshotter{"callback_pending": snapshot},
				RebindRestore: func(context.Context, core.FederationName, uint64, ManifestOf[T]) (ManifestOf[T], error) {
					graphCalls++
					return ManifestOf[T]{}, errors.New("must not allocate")
				},
				ValidateRestoreCallbackRecovery: func(_ context.Context, fed core.FederationName, in ManifestOf[T]) error {
					preflightCalls++
					if fed != testFed || *in.FederationGeneration != 1 || in.Federates[0] != 1<<32|1 || string(in.FederateClientStates[0]) != "source-runtime" || string(in.ManagerSnapshots["callback_pending"]) != "source-ledger" {
						t.Fatalf("not detached source preflight: %+v", in)
					}
					in.FederateClientStates[0][0] = 'X'
					in.ManagerSnapshots["callback_pending"][0] = 'X'
					in.Federates[0] = 0
					*in.FederationGeneration = 99
					return unknown
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				err = mgr.RequestFederationRestoreFrom(ctx, testFed, core.FederateHandle(target<<32|1), "saved", 1)
				if !errors.Is(err, ErrSaveBundleIncompatible) || !errors.Is(err, unknown) {
					t.Fatalf("wrong preflight error: %v", err)
				}
			}
			if graphCalls != 0 || len(snapshot.unmarshaled) != 0 || preflightCalls != 2 {
				t.Fatalf("preflight failed after mutation: graph=%d snapshots=%d preflight=%d", graphCalls, len(snapshot.unmarshaled), preflightCalls)
			}
		})
	}
}
