package federate

import (
	"context"
	"reflect"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestRestoreRetractionHintPresenceAndPublication(t *testing.T) {
	for _, tc := range []struct {
		name    string
		present bool
		value   uint64
	}{
		{"absent", false, 0}, {"unknown_zero", true, 0}, {"known", true, 81}, {"maximum", true, ^uint64(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tables, err := tablesFromFOMView(authoritativeView(1))
			if err != nil {
				t.Fatal(err)
			}
			f := &Federate{handles: tables, federateHandle: 3, conn: &Connection{fed: fomViewClient{get: func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
				return authoritativeView(1), nil
			}}}}
			wire := &rtiv1.FederationRestored{Label: "saved"}
			encoded, err := proto.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if tc.present {
				encoded = protowire.AppendVarint(protowire.AppendTag(encoded, 4, protowire.VarintType), tc.value)
			}
			if err := proto.Unmarshal(encoded, wire); err != nil {
				t.Fatal(err)
			}
			typed := f.translate(&rtiv1.FederateEvent{Event: &rtiv1.FederateEvent_RestoreCompleted{RestoreCompleted: wire}})
			field := reflect.ValueOf(typed).FieldByName("NextRetractionHandle")
			if !field.IsValid() {
				t.Fatal("restore callback discarded next retraction handle")
			}
			if field.IsNil() == tc.present {
				t.Fatal("next handle presence changed")
			}
			if tc.present && field.Elem().Uint() != tc.value {
				t.Fatal("next handle value changed")
			}
			accessor, ok := any(f).(interface{ NextRetractionHandle() (uint64, bool) })
			if !ok {
				t.Fatal("restored next-ID accessor missing")
			}
			if _, known := accessor.NextRetractionHandle(); known {
				t.Fatal("next ID fabricated before restore")
			}
			_, healthy := f.prepareRestoredCallback(context.Background(), typed)
			if !healthy {
				t.Fatal("restore synchronization failed")
			}
			got, known := accessor.NextRetractionHandle()
			if got != tc.value || known != (tc.present && tc.value != 0) {
				t.Fatalf("next ID=%d,%v", got, known)
			}
			if tc.present {
				field.Elem().SetUint(9)
			}
			if next, _ := accessor.NextRetractionHandle(); next != tc.value {
				t.Fatal("application mutation changed identity hint")
			}
		})
	}
}

func TestRestoreRetractionHintDoesNotRewriteCallerIDs(t *testing.T) {
	tables, err := tablesFromFOMView(authoritativeView(1))
	if err != nil {
		t.Fatal(err)
	}
	client := &captureInteractionClient{}
	f := &Federate{handles: tables, federateHandle: 3, conn: &Connection{obj: client, fed: fomViewClient{get: func(context.Context, *rtiv1.GetFOMViewRequest) (*rtiv1.FOMView, error) {
		return authoritativeView(1), nil
	}}}}
	next := uint64(81)
	if err := f.applyRestoredState(context.Background(), FederationRestored{NextRetractionHandle: &next}); err != nil {
		t.Fatal(err)
	}
	ts := float64(100)
	if err := f.SendInteractionByHandleWithOptions(context.Background(), 27, nil, &ts, MessageOptions{RetractionHandle: 5}); err != nil {
		t.Fatal(err)
	}
	if client.send == nil || client.send.MessageRetractionHandle != 5 {
		t.Fatal("SDK renumbered or rejected a caller ID below the hint")
	}
	if hint, ok := f.NextRetractionHandle(); !ok || hint != 81 {
		t.Fatal("read-only hint became an allocator")
	}
	if err := f.applyRestoredState(context.Background(), FederationRestored{}); err != nil {
		t.Fatal(err)
	}
	if _, known := f.NextRetractionHandle(); known {
		t.Fatal("legacy restore retained stale allocation advice")
	}
}
