package savebundle

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func deletionTombstoneDocument() map[string]any {
	return map[string]any{"version": 3, "state": map[string]any{
		"LastObjectHandle": 4, "LastOutboundSequence": 10,
		"Deletions": []any{map[string]any{
			"Object": 4, "Producer": 1, "Timestamp": 5, "RetractionHandle": 7, "Committed": true,
			"ObjectName": "vehicle", "ObjectClass": 3,
			"Recipients": []any{map[string]any{"Recipient": 2, "ReceivedTSO": true, "Forgotten": false}},
		}},
		"Discoveries": []any{map[string]any{"Object": 4, "Federate": 2}},
	}}
}

func tombstoneDeletion(doc map[string]any) map[string]any {
	return doc["state"].(map[string]any)["Deletions"].([]any)[0].(map[string]any)
}

func TestDeletionTombstoneV3RoundTripAndIdentityRebind(t *testing.T) {
	s := objectStateCodec(t)
	data := graphJSON(t, deletionTombstoneDocument())
	if err := s.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	round, err := s.Marshal("fed")
	if err != nil || !bytes.Contains(round, []byte(`"version":3`)) {
		t.Fatalf("tombstone ledger lost: %s %v", round, err)
	}
	if err := s.Unmarshal("fed", round); err != nil {
		t.Fatal(err)
	}
	again, err := s.Marshal("fed")
	if err != nil || !bytes.Equal(round, again) {
		t.Fatalf("unstable tombstone snapshot: %s %v", again, err)
	}
	rebound, err := s.RewriteFederates(round, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 10, nil })
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(rebound, &doc); err != nil {
		t.Fatal(err)
	}
	state := doc["state"].(map[string]any)
	d := state["Deletions"].([]any)[0].(map[string]any)
	if d["Producer"] != float64(11) || d["Object"] != float64(4) || d["RetractionHandle"] != float64(7) || d["ObjectClass"] != float64(3) || d["ObjectName"] != "vehicle" {
		t.Fatalf("wrong tombstone identity rewrite: %v", d)
	}
	if recipient := d["Recipients"].([]any)[0].(map[string]any); recipient["Recipient"] != float64(12) {
		t.Fatalf("recipient not rebound: %v", recipient)
	}
	if discovery := state["Discoveries"].([]any)[0].(map[string]any); discovery["Federate"] != float64(12) {
		t.Fatal("known-object discovery not rebound")
	}
}

func TestDeletionTombstoneV3RejectsMalformedBeforeMutation(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"legacy v1":      func(d map[string]any) { d["version"] = 1 },
		"legacy v2":      func(d map[string]any) { d["version"] = 2 },
		"future version": func(d map[string]any) { d["version"] = 5 },
		"missing name":   func(d map[string]any) { delete(tombstoneDeletion(d), "ObjectName") },
		"missing class":  func(d map[string]any) { delete(tombstoneDeletion(d), "ObjectClass") },
		"zero recipient": func(d map[string]any) {
			tombstoneDeletion(d)["Recipients"].([]any)[0].(map[string]any)["Recipient"] = 0
		},
		"duplicate recipient": func(d map[string]any) {
			q := tombstoneDeletion(d)
			q["Recipients"] = append(q["Recipients"].([]any), q["Recipients"].([]any)[0])
		},
		"forgotten discovery": func(d map[string]any) {
			tombstoneDeletion(d)["Recipients"].([]any)[0].(map[string]any)["Forgotten"] = true
		},
		"RO still known": func(d map[string]any) {
			tombstoneDeletion(d)["Recipients"].([]any)[0].(map[string]any)["ReceivedTSO"] = false
		},
		"legacy uppercase null": func(d map[string]any) {
			d["version"] = 2
			q := tombstoneDeletion(d)
			delete(q, "Recipients")
			q["RECIPIENTS"] = nil
			delete(q, "ObjectClass")
			delete(q, "ObjectName")
			d["state"].(map[string]any)["Discoveries"] = nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := objectStateCodec(t)
			before, err := s.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			doc := deletionTombstoneDocument()
			mutate(doc)
			commit, err := s.PrepareRestore("fed", graphJSON(t, doc))
			if err == nil || commit != nil {
				t.Fatal("malformed tombstone accepted")
			}
			after, err := s.Marshal("fed")
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed prepare mutated registry")
			}
		})
	}
}
