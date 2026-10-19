package federation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
)

type joinExtension struct {
	prepared, committed, released bool
	err                           error
}

func (p *joinExtension) FOM() core.FOMHandle  { return &stubFOMHandle{} }
func (p *joinExtension) View() core.FOMView   { return core.FOMView{Revision: 2, Digest: "extended"} }
func (p *joinExtension) PrepareCommit() error { p.prepared = true; return p.err }
func (p *joinExtension) Commit() error {
	if !p.prepared {
		return errors.New("unprepared commit")
	}
	p.committed = true
	return nil
}
func (p *joinExtension) Release() { p.released = true }

type joinExtensionLog struct {
	core.EventLog
	prep    *joinExtension
	fail    bool
	records []*core.FederationJoinedJournalRecord
}

func (l *joinExtensionLog) Append(_ context.Context, _ core.FederationName, e core.EventRecord) error {
	if l.prep != nil && !l.prep.prepared {
		return errors.New("journal before model reservation")
	}
	if l.fail {
		return errors.New("journal unavailable")
	}
	l.records = append(l.records, e.(*core.FederationJoinedJournalRecord))
	return nil
}

func TestJoinAdditionalFOMAtomicAdmission(t *testing.T) {
	for _, name := range []string{"unsupported", "prepare-rejected", "journal-failed", federationStageSuccess} {
		t.Run(name, func(t *testing.T) {
			p := &joinExtension{}
			if name == "prepare-rejected" {
				p.err = errors.New("stale FOM")
			}
			log := &joinExtensionLog{prep: p, fail: name == "journal-failed"}
			opts := validOptions()
			opts.EventLog = log
			if name != "unsupported" {
				opts.PrepareJoinFOM = func(_ context.Context, _ core.FederationName, modules []core.FOMModule) (core.FOMExtensionPreparation, error) {
					if string(modules[0].XML) != "extension" {
						t.Fatal("module lost")
					}
					return p, nil
				}
			}
			m, err := federation.New(opts)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := m.CreateFederation(ctx, core.CreateFederationRequest{Name: "f"}); err != nil {
				t.Fatal(err)
			}
			h, err := m.JoinFederation(ctx, core.JoinFederationRequest{Federation: "f", FederateName: "joiner", AdditionalFOMModules: []core.FOMModule{{XML: []byte("extension")}}})
			if name == federationStageSuccess {
				if err != nil || h == 0 || !p.committed || !p.released || len(log.records) != 1 {
					t.Fatalf("join=%d err=%v prep=%+v records=%d", h, err, p, len(log.records))
				}
				r := log.records[0]
				if r.FOMRevision != 2 || r.FOMDigest != "extended" || len(r.AdditionalFOMModules) != 1 {
					t.Fatalf("incomplete replay record: %+v", r)
				}
			} else if err == nil || h != 0 || p.committed || len(m.ListMembers("f")) != 0 || len(log.records) != 0 {
				t.Fatalf("failed admission mutated join: h=%d err=%v prep=%+v", h, err, p)
			}
		})
	}
}
