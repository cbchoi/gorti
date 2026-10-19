package savepoint

import (
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type candidateTestModel struct {
	core.FOMHandle
	valid bool
}

func (m candidateTestModel) IsValid() bool { return m.valid }

type candidateTestSnapshot struct {
	leasedTransitionSnapshot
	model core.FOMHandle
}
type candidateTestPrepared struct {
	*leasedTransitionPrepared
	model core.FOMHandle
}

func (s *candidateTestSnapshot) PrepareRestoreState(_ core.FederationName, data []byte) (core.PreparedRestore, error) {
	return &candidateTestPrepared{&leasedTransitionPrepared{owner: &s.leasedTransitionSnapshot, data: append([]byte(nil), data...)}, s.model}, nil
}
func (p *candidateTestPrepared) Candidate() core.FOMHandle { return p.model }

type candidateTestDependent struct {
	transitionSnapshot
	want             core.FOMHandle
	failure          error
	calls, fallbacks int
}

func (s *candidateTestDependent) PrepareRestore(core.FederationName, []byte) (func(), error) {
	s.fallbacks++
	return nil, errors.New("unexpected live-model fallback")
}
func (s *candidateTestDependent) PrepareRestoreWithFOM(fed core.FederationName, data []byte, model core.FOMHandle) (func(), error) {
	s.calls++
	if model != s.want {
		return nil, errors.New("wrong saved candidate")
	}
	if s.failure != nil {
		return nil, s.failure
	}
	return s.transitionSnapshot.PrepareRestore(fed, data)
}

func TestSavedFOMCandidatePreparationAndFailureRelease(t *testing.T) {
	for _, mode := range []string{"success", testDependentFailure, "nil-candidate", "invalid-candidate", "missing-capability"} {
		t.Run(mode, func(t *testing.T) {
			var candidate core.FOMHandle = candidateTestModel{valid: true}
			if mode == "nil-candidate" {
				candidate = nil
			}
			if mode == "invalid-candidate" {
				candidate = candidateTestModel{valid: false}
			}
			fom := &candidateTestSnapshot{leasedTransitionSnapshot: leasedTransitionSnapshot{transitionSnapshot: transitionSnapshot{live: "live-fom"}}, model: candidate}
			dependent := &candidateTestDependent{transitionSnapshot: transitionSnapshot{live: "live-declaration"}, want: candidate}
			failure := errors.New("dependent validation failed")
			if mode == testDependentFailure {
				dependent.failure = failure
			}
			var fomAdapter ManagerSnapshotter = fom
			if mode == "missing-capability" {
				fomAdapter = &fom.leasedTransitionSnapshot
			}
			manager := &Manager{opts: Options{RequiredSnapshots: []string{"fom", "declaration"}, ManagerSnapshots: map[string]ManagerSnapshotter{"fom": fomAdapter, "declaration": dependent}}}
			commit, release, err := manager.prepareCompleteSnapshots("f", map[string][]byte{"fom": []byte("saved-fom"), "declaration": []byte("saved-declaration")})
			if fom.live != "live-fom" || dependent.live != "live-declaration" || fom.commits != 0 {
				t.Fatal("preparation published state")
			}
			if dependent.fallbacks != 0 {
				t.Fatal("dependent validated against live model")
			}
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if dependent.calls != 1 || fom.releases != 0 {
					t.Fatal("candidate not retained through preparation")
				}
				if err := commit(); err != nil {
					t.Fatal(err)
				}
				release()
				release()
				if fom.live != "saved-fom" || dependent.live != "saved-declaration" {
					t.Fatal("prepared state not committed")
				}
			} else {
				if err == nil || commit != nil || release != nil {
					t.Fatal("failed preparation returned usable commit")
				}
				if mode == testDependentFailure && !errors.Is(err, failure) {
					t.Fatalf("lost validation failure: %v", err)
				}
				if mode != testDependentFailure && dependent.calls != 0 {
					t.Fatal("invalid candidate reached dependent")
				}
			}
			if fom.releases != 1 {
				t.Fatalf("FOM preparation released %d times", fom.releases)
			}
		})
	}
}

const testDependentFailure = "dependent-failure"
