package savebundle

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

type graphExtensionProbe struct {
	validate func(core.FederationName, core.FOMHandle, []core.FederateHandle, string, []byte) error
	rewrite  func([]byte, func(core.FederateHandle) (core.FederateHandle, error)) ([]byte, error)
}

func (*graphExtensionProbe) Marshal(core.FederationName) ([]byte, error) {
	panic("validation must not capture live state")
}
func (*graphExtensionProbe) Unmarshal(core.FederationName, []byte) error {
	panic("validation must not publish state")
}
func (p *graphExtensionProbe) RewriteFederates(data []byte, rewrite func(core.FederateHandle) (core.FederateHandle, error)) ([]byte, error) {
	if p.rewrite != nil {
		return p.rewrite(data, rewrite)
	}
	return bytes.Clone(data), nil
}
func (p *graphExtensionProbe) ValidateCheckpoint(fed core.FederationName, model core.FOMHandle, roster []core.FederateHandle, family string, data []byte) error {
	return p.validate(fed, model, roster, family, data)
}

func TestRestoreGraphExtensionCandidateValidationDetachedAndTyped(t *testing.T) {
	t.Run("Float64", func(t *testing.T) {
		manifest, model, _ := knownClassGraphFixture(t, true)
		checkGraphExtensionCandidate(t, manifest, model)
	})
	t.Run("Integer64", func(t *testing.T) {
		manifest, model := integerGraphFixture(t)
		checkGraphExtensionCandidate(t, manifest, model)
	})
}

func checkGraphExtensionCandidate[T core.LogicalTimeScalar](t *testing.T, manifest core.CheckpointManifestOf[T], model core.FOMHandle) {
	t.Helper()
	data := []byte("portable-parent-owned-codec")
	roster := append([]core.FederateHandle(nil), manifest.Federates...)
	manifest.ManagerSnapshots["callback_pending"] = bytes.Clone(data)
	// Exercise the optional built-in component's path too: it must not return
	// before extensions have validated their candidate snapshots.
	state := federation.AutomaticResignState{Version: 1}
	for _, name := range manifest.FederateNames {
		state.Directives = append(state.Directives, federation.AutomaticResignDirective{Name: name, Action: core.ResignActionNoAction})
	}
	manifest.ManagerSnapshots["automatic_resign"] = graphJSON(t, state)
	called := 0
	probe := &graphExtensionProbe{validate: func(fed core.FederationName, candidate core.FOMHandle, members []core.FederateHandle, family string, payload []byte) error {
		called++
		if fed != manifest.Federation || candidate != model || family != core.ScalarRepresentation[T]().String() || !reflect.DeepEqual(members, roster) || !bytes.Equal(payload, data) {
			t.Fatal("incorrect candidate validation context")
		}
		members[0] = 99
		payload[0] = '!'
		return nil
	}}
	graph := RestoreGraphOf[T]{Snapshots: map[string]core.CheckpointSnapshotter{"callback_pending": probe, "automatic_resign": AutomaticResign{}}}
	if err := graph.Validate(manifest.Federation, manifest, model); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("candidate validation calls: %d", called)
	}
	if !reflect.DeepEqual(manifest.Federates, roster) || !bytes.Equal(manifest.ManagerSnapshots["callback_pending"], data) {
		t.Fatal("extension validation mutated manifest")
	}
	failure := errors.New("immutable wire timestamp or FOM reference invalid")
	probe.validate = func(core.FederationName, core.FOMHandle, []core.FederateHandle, string, []byte) error { return failure }
	if err := graph.Validate(manifest.Federation, manifest, model); !errors.Is(err, failure) || !strings.Contains(err.Error(), "callback_pending") {
		t.Fatalf("extension candidate failure: %v", err)
	}
	delete(manifest.ManagerSnapshots, "callback_pending")
	if err := graph.Validate(manifest.Federation, manifest, model); err != nil {
		t.Fatalf("legacy absent extension: %v", err)
	}
}

func TestRestoreGraphExtensionReferencesAndRebindRemainChecked(t *testing.T) {
	manifest, _, _ := knownClassGraphFixture(t, true)
	const historical core.FederateHandle = 7
	manifest.ManagerSnapshots["callback_pending"] = []byte("saved")
	probe := &graphExtensionProbe{rewrite: func(data []byte, rewrite func(core.FederateHandle) (core.FederateHandle, error)) ([]byte, error) {
		if string(data) != "saved" {
			return nil, fmt.Errorf("unknown extension format")
		}
		h, err := rewrite(historical)
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprint(h)), nil
	}}
	graph := RestoreGraph{Snapshots: map[string]core.CheckpointSnapshotter{"callback_pending": probe}}
	refs, err := graph.References(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(refs, []core.FederateHandle{1, 2, historical}) {
		t.Fatalf("extension references: %v", refs)
	}
	mapping := map[core.FederateHandle]core.FederateHandle{1: 9<<32 | 1, 2: 9<<32 | 2}
	if _, err := graph.Rebind(manifest, 9, mapping); err == nil {
		t.Fatal("unmapped extension identity accepted")
	}
	mapping[historical] = 9<<32 | 7
	rebound, err := graph.Rebind(manifest, 9, mapping)
	if err != nil || string(rebound.ManagerSnapshots["callback_pending"]) != fmt.Sprint(mapping[historical]) {
		t.Fatalf("extension rewrite: %v", err)
	}
	if string(manifest.ManagerSnapshots["callback_pending"]) != "saved" {
		t.Fatal("rebind changed source manifest")
	}
	if _, err := (RestoreGraph{}).References(manifest); err == nil {
		t.Fatal("unknown extension silently accepted")
	}
	manifest.ManagerSnapshots["callback_pending"] = []byte("unknown-version")
	if _, err := graph.References(manifest); err == nil {
		t.Fatal("unknown carrier format silently accepted")
	}
}
