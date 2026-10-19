package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func roSnapshotDocument(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"version": 4, "stall_timeout_ns": 15000000000,
		"federates": []any{map[string]any{
			"handle": 2, "has_regulation_state": true, "constrained": true, "has_advance_state": true,
			"ro_queue": []any{
				map[string]any{"destination": 2, "sender": 1, "receive_order": true, "event": map[string]any{
					"kind": "reflect", "sequence": 31, "object": 4, "object_class": 7,
					"attributes": map[string]any{"8": []byte{0, 255, 3}}, "attribute_handles": []uint64{8},
					"user_supplied_tag": []byte{0, 255, 0},
					"metadata":          &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, Producer: 1, Regions: []core.DDMRegionHandleCore{4}},
				}},
				map[string]any{"destination": 2, "sender": 1, "receive_order": true, "event": map[string]any{
					"kind": "receive", "sequence": 32, "interaction_class": 9, "parameters": map[string]any{"10": []byte{255, 0}}, "logical_time": 12.5,
					"user_supplied_tag": []byte{255, 0, 255},
					"metadata":          &core.ObjectCallbackMetadata{SentOrder: core.OrderTimestamp, ReceivedOrder: core.OrderReceive, Producer: 1},
				}},
				map[string]any{"destination": 2, "sender": 1, "receive_order": true, "event": map[string]any{
					"kind": "remove", "sequence": 33, "object": 4, "object_class": 7, "object_name": "deleted",
					"user_supplied_tag": []byte{254, 0},
					"metadata":          &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, Producer: 1},
				}},
			},
		}},
	}
}

func roDocumentFederate(doc map[string]any) map[string]any {
	return doc["federates"].([]any)[0].(map[string]any)
}
func roDocumentQueue(doc map[string]any) []any { return roDocumentFederate(doc)["ro_queue"].([]any) }

func TestROTimeSnapshotRoundTripAndRelease(t *testing.T) {
	s, _, out := newTimeStateCodec(t)
	if err := s.Unmarshal("fed", graphJSON(t, roSnapshotDocument(t))); err != nil {
		t.Fatal(err)
	}
	before, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(before, []byte(`"version":4`)) || !bytes.Contains(before, []byte(`"ro_queue"`)) {
		t.Fatalf("RO queue lost: %s", before)
	}
	commit, err := s.PrepareRestore("fed", before)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 0 {
		t.Fatal("restore prepare emitted callbacks")
	}
	if err := s.Unmarshal("fed", nil); err != nil {
		t.Fatal(err)
	}
	commit()
	commit()
	after, err := s.Marshal("fed")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("unstable roundtrip: %s, %v", after, err)
	}
	if len(out.sent) != 0 {
		t.Fatal("restore commit emitted callbacks")
	}
	if err := s.Manager.EnableAsynchronousDelivery(context.Background(), "fed", 2); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 3 {
		t.Fatalf("restored callbacks: %d", len(out.sent))
	}
	for i, sent := range out.sent {
		e := sent.event.(*object.CallbackEvent)
		if sent.dst != 2 || e.Sequence != uint64(31+i) || e.Metadata.ReceivedOrder != core.OrderReceive || e.Metadata.Producer != 1 {
			t.Fatalf("callback changed: %+v", sent)
		}
	}
	reflectEvent := out.sent[0].event.(*object.CallbackEvent)
	if reflectEvent.LogicalTime != nil || !bytes.Equal(reflectEvent.Attributes[8], []byte{0, 255, 3}) || !bytes.Equal(reflectEvent.UserSuppliedTag, []byte{0, 255, 0}) || !reflect.DeepEqual(reflectEvent.Metadata.Regions, []core.DDMRegionHandleCore{4}) {
		t.Fatalf("RO reflection changed: %+v", reflectEvent)
	}
	interaction := out.sent[1].event.(*object.CallbackEvent)
	if interaction.LogicalTime == nil || *interaction.LogicalTime != 12.5 || !bytes.Equal(interaction.Parameters[10], []byte{255, 0}) {
		t.Fatalf("original timestamp/payload lost: %+v", interaction)
	}
	if out.sent[2].event.(*object.CallbackEvent).LogicalTime != nil {
		t.Fatal("invented removal timestamp")
	}
}

func TestROTimeSnapshotSchemaAndAtomicRejection(t *testing.T) {
	cases := map[string]func(map[string]any){
		"legacy-v1":         func(d map[string]any) { d["version"] = 1 },
		"legacy-v2":         func(d map[string]any) { d["version"] = 2 },
		"legacy-v3":         func(d map[string]any) { d["version"] = 3 },
		"future":            func(d map[string]any) { d["version"] = 5 },
		"wrong-destination": func(d map[string]any) { roDocumentQueue(d)[0].(map[string]any)["destination"] = 3 },
		"timestamp-key":     func(d map[string]any) { roDocumentQueue(d)[0].(map[string]any)["timestamp"] = 1 },
		"retractable":       func(d map[string]any) { roDocumentQueue(d)[0].(map[string]any)["retraction_handle"] = 7 },
		"not-ro":            func(d map[string]any) { roDocumentQueue(d)[0].(map[string]any)["receive_order"] = false },
		"missing-state":     func(d map[string]any) { roDocumentFederate(d)["has_advance_state"] = false },
		"control-callback": func(d map[string]any) {
			roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any)["kind"] = "discover"
		},
		"delete-marker": func(d map[string]any) {
			roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any)["kind"] = "delete_marker"
		},
		"wrong-received-order": func(d map[string]any) {
			roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any)["metadata"].(*core.ObjectCallbackMetadata).ReceivedOrder = core.OrderTimestamp
		},
		"metadata-retraction": func(d map[string]any) {
			m := roDocumentQueue(d)[1].(map[string]any)["event"].(map[string]any)["metadata"].(*core.ObjectCallbackMetadata)
			m.RetractionHandle = 1
		},
		"no-metadata": func(d map[string]any) {
			delete(roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any), "metadata")
		},
		"negative-original-time": func(d map[string]any) {
			roDocumentQueue(d)[0].(map[string]any)["event"].(map[string]any)["logical_time"] = -1
		},
		"tso-without-time": func(d map[string]any) {
			f := roDocumentFederate(d)
			q := roDocumentQueue(d)[0].(map[string]any)
			q["receive_order"] = false
			f["queue"] = []any{q}
			delete(f, "ro_queue")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, _, _ := newTimeStateCodec(t)
			if err := s.Unmarshal("fed", graphJSON(t, roSnapshotDocument(t))); err != nil {
				t.Fatal(err)
			}
			before, err := s.Marshal("fed")
			if err != nil {
				t.Fatal(err)
			}
			doc := roSnapshotDocument(t)
			mutate(doc)
			commit, err := s.PrepareRestore("fed", graphJSON(t, doc))
			if err == nil || commit != nil {
				t.Fatal("invalid RO snapshot accepted")
			}
			after, err := s.Marshal("fed")
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid restore changed live queue")
			}
		})
	}
}

func TestROTimeEventCloneOwnsPayload(t *testing.T) {
	ts := core.LogicalTime(12.5)
	for name, original := range map[string]*core.LogicalTime{"untimed": nil, "original-time": &ts} {
		t.Run(name, func(t *testing.T) {
			e := &object.CallbackEvent{Kind: object.CallbackReflect, Object: 4, ObjectClass: 7, LogicalTime: original,
				Attributes: map[uint64][]byte{8: {0, 255}}, Parameters: map[uint64][]byte{10: {255, 0}}, AttributeHandles: []core.AttributeHandle{8},
				UserSuppliedTag: []byte{0, 255}, ObjectNames: []string{"one"}, CollidingNames: []string{"two"},
				Metadata: &core.ObjectCallbackMetadata{ReceivedOrder: core.OrderReceive, SentOrder: core.OrderReceive, Producer: 1, Regions: []core.DDMRegionHandleCore{4}}}
			cloned, err := cloneTimeEvent(e)
			if err != nil {
				t.Fatal(err)
			}
			got := cloned.(*object.CallbackEvent)
			if !reflect.DeepEqual(e, got) {
				t.Fatalf("clone differs: %+v", got)
			}
			got.Attributes[8][0] = 99
			got.Parameters[10][0] = 99
			got.AttributeHandles[0] = 99
			got.UserSuppliedTag[0] = 99
			got.Metadata.Regions[0] = 99
			got.ObjectNames[0] = testChanged
			got.CollidingNames[0] = testChanged
			if got.LogicalTime != nil {
				*got.LogicalTime = 99
			}
			if e.Attributes[8][0] != 0 || e.Parameters[10][0] != 255 || e.AttributeHandles[0] != 8 || e.UserSuppliedTag[0] != 0 || e.Metadata.Regions[0] != 4 || e.ObjectNames[0] != "one" || e.CollidingNames[0] != "two" || original != nil && *original != 12.5 {
				t.Fatal("clone aliases mutable input")
			}
		})
	}
}

func TestROTimeSnapshotRejectsLegacyNullExtension(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		doc := roSnapshotDocument(t)
		doc["version"] = version
		roDocumentFederate(doc)["ro_queue"] = nil
		data := graphJSON(t, doc)
		s, _, _ := newTimeStateCodec(t)
		if commit, err := s.PrepareRestore("fed", data); err == nil || commit != nil {
			t.Fatalf("version %d accepted null RO extension", version)
		}
		if _, err := (Time{}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h, nil }); err == nil {
			t.Fatalf("version %d rebound null RO extension", version)
		}
	}
}

func TestROTimeSnapshotPreparedImageOwnsPayload(t *testing.T) {
	s, _, _ := newTimeStateCodec(t)
	if err := s.Unmarshal("fed", graphJSON(t, roSnapshotDocument(t))); err != nil {
		t.Fatal(err)
	}
	before, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	exported, err := s.Manager.ExportState("fed", cloneTimeEvent)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := s.Manager.PrepareImportState("fed", exported, cloneTimeEvent)
	if err != nil {
		t.Fatal(err)
	}
	q := exported.Federates[0].ROQueue
	q[0].Recipient = 99
	e := q[0].Event.(*object.CallbackEvent)
	e.Attributes[8][0] = 99
	e.UserSuppliedTag[0] = 99
	e.Metadata.Regions[0] = 99
	*q[1].Event.(*object.CallbackEvent).LogicalTime = 99
	current, err := s.Marshal("fed")
	if err != nil || !bytes.Equal(current, before) {
		t.Fatal("exported RO payload aliases manager")
	}
	if err := s.Unmarshal("fed", nil); err != nil {
		t.Fatal(err)
	}
	commit()
	current, err = s.Marshal("fed")
	if err != nil || !bytes.Equal(current, before) {
		t.Fatal("prepared RO image aliases caller")
	}
}

func TestROTimeSnapshotMixedQueuesPrecedeGrant(t *testing.T) {
	doc := roSnapshotDocument(t)
	roDocumentFederate(doc)["queue"] = []any{map[string]any{"destination": 2, "sender": 1, "timestamp": 3, "event": map[string]any{
		"kind": "receive", "interaction_class": 9, "sequence": 34, "logical_time": 3, "parameters": map[uint64][]byte{10: {4}},
		"metadata": &core.ObjectCallbackMetadata{SentOrder: core.OrderTimestamp, ReceivedOrder: core.OrderTimestamp, Producer: 1},
	}}}
	s, _, out := newTimeStateCodec(t)
	if err := s.Unmarshal("fed", graphJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Unmarshal("fed", data); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.TimeAdvanceRequest(context.Background(), "fed", 2, 5); err != nil {
		t.Fatal(err)
	}
	if len(out.sent) != 5 {
		t.Fatalf("got %d callbacks", len(out.sent))
	}
	for i := 0; i < 3; i++ {
		e, ok := out.sent[i].event.(*object.CallbackEvent)
		if !ok || e.Sequence != uint64(31+i) || e.Metadata.ReceivedOrder != core.OrderReceive {
			t.Fatalf("RO callback %d: %+v", i, out.sent[i])
		}
	}
	if e, ok := out.sent[3].event.(*object.CallbackEvent); !ok || e.Sequence != 34 || e.Metadata.ReceivedOrder != core.OrderTimestamp {
		t.Fatalf("TSO callback: %+v", out.sent[3])
	}
	if g, ok := out.sent[4].event.(*timepkg.TimeAdvanceGrant); !ok || g.Time != 5 {
		t.Fatalf("missing final grant: %+v", out.sent[4])
	}
}

func TestROTimeSnapshotDoesNotRelaxTSOEncoding(t *testing.T) {
	s, _, _ := newTimeStateCodec(t)
	e := &object.CallbackEvent{Kind: object.CallbackReflect, Object: 4, Metadata: &core.ObjectCallbackMetadata{SentOrder: core.OrderReceive, ReceivedOrder: core.OrderReceive, Producer: 1}}
	if _, err := encodeTimeEvent(e); err == nil {
		t.Fatal("TSO encoder accepted untimestamped RO event")
	}
	if _, err := decodeTimeEvent(timeEventSnapshot{Kind: "reflect", Object: 4, Metadata: e.Metadata}); err == nil {
		t.Fatal("TSO decoder accepted missing timestamp")
	}
	if err := s.Manager.EnableConstrained(context.Background(), "fed", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.Manager.BufferTSO(context.Background(), "fed", 2, 8, e); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Marshal("fed"); err == nil {
		t.Fatal("TSO queue marshaled an untimestamped RO event")
	}
}

func TestROTimeSnapshotLegacyVersionsRoundTrip(t *testing.T) {
	for version := 1; version <= 3; version++ {
		doc := roSnapshotDocument(t)
		doc["version"] = version
		f := roDocumentFederate(doc)
		delete(f, "ro_queue")
		f["regulating"] = true
		switch version {
		case 2:
			f["lookahead"] = 5
			f["requested_lookahead"] = 0
			f["lookahead_origin_value"] = 5
		case 3:
			f["strict_zero_lookahead"] = true
		}
		s, _, _ := newTimeStateCodec(t)
		if err := s.Unmarshal("fed", graphJSON(t, doc)); err != nil {
			t.Fatalf("version %d restore: %v", version, err)
		}
		data, err := s.Marshal("fed")
		if err != nil {
			t.Fatal(err)
		}
		var got timeSnapshot
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		wantVersion := uint32(version)
		if version == 3 {
			wantVersion = 5
			if got.Federates[0].PreservedReceiveTime == nil || *got.Federates[0].PreservedReceiveTime != got.Federates[0].CurrentTime {
				t.Fatal("legacy constrained strict history was not preserved")
			}
		}
		if got.Version != wantVersion || len(got.Federates[0].ROQueue) != 0 {
			t.Fatalf("legacy version %d changed: %s", version, data)
		}
		if _, err := s.RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) { return h + 10, nil }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestROTimeSnapshotVersionDoesNotDowngrade(t *testing.T) {
	doc := roSnapshotDocument(t)
	doc["federates"] = append(doc["federates"].([]any), map[string]any{"handle": 3, "has_regulation_state": true, "regulating": true, "has_advance_state": true, "strict_zero_lookahead": true})
	s, _, _ := newTimeStateCodec(t)
	if err := s.Unmarshal("fed", graphJSON(t, doc)); err != nil {
		t.Fatal(err)
	}
	data, err := s.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != float64(4) {
		t.Fatalf("later strict history downgraded version: %s", data)
	}
}

const testChanged = "changed"
