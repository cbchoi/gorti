package savebundle_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	savepoint "github.com/cbchoi/gorti/rti/internal/hla/save_restore"
)

var _ savepoint.ManagerSnapshotter = savebundle.Declaration{}
var _ savepoint.PreparedSnapshotter = savebundle.Declaration{}

func seedDeclarationCodec(t *testing.T, m *declaration.Manager, members ...core.FederateHandle) {
	t.Helper()
	for _, h := range members {
		for _, err := range []error{
			m.PublishObjectClassAttributes(context.Background(), "fed", h, 7, []core.AttributeHandle{3, 2}),
			m.SubscribeObjectClassAttributes(context.Background(), "fed", h, 7, []core.AttributeHandle{3, 2}),
			m.PublishInteractionClass(context.Background(), "fed", h, 11),
			m.SubscribeInteractionClass(context.Background(), "fed", h, 11),
		} {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestDeclarationSnapshotVersionedRoundTrip(t *testing.T) {
	m := declaration.New()
	codec := savebundle.Declaration{Manager: m}
	high := core.FederateHandle(^uint64(0))
	seedDeclarationCodec(t, m, high, 2)
	want := m.ExportState("fed")
	data, err := codec.Marshal("fed")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"version":4`)) || !bytes.Contains(data, []byte("18446744073709551615")) {
		t.Fatalf("missing version or lossless uint64 handle: %s", data)
	}
	other := declaration.New()
	seedDeclarationCodec(t, other, 2, high)
	otherData, err := (savebundle.Declaration{Manager: other}).Marshal("fed")
	if err != nil || !bytes.Equal(data, otherData) {
		t.Fatalf("non-deterministic encoding: err=%v", err)
	}
	m.OnFederationDestroyed("fed")
	seedDeclarationCodec(t, m, 99)
	before := m.ExportState("fed")
	commit, err := codec.PrepareRestore("fed", data)
	if err != nil || commit == nil {
		t.Fatalf("prepare: commit nil=%v err=%v", commit == nil, err)
	}
	if !reflect.DeepEqual(m.ExportState("fed"), before) {
		t.Fatal("decode changed live declarations before commit")
	}
	clear(data)
	commit()
	if !reflect.DeepEqual(m.ExportState("fed"), want) {
		t.Fatal("round trip lost declaration state or retained the input buffer")
	}
	if err := codec.Unmarshal("fed", otherData); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.ExportState("fed"), want) {
		t.Fatal("direct unmarshal failed to restore all tables")
	}
}

func TestDeclarationSnapshotEmptyRestoreClearsState(t *testing.T) {
	for name, data := range map[string][]byte{"nil": nil, "zero bytes": {}, "versioned empty": []byte(`{"version":1}`)} {
		t.Run(name, func(t *testing.T) {
			m := declaration.New()
			seedDeclarationCodec(t, m, 1)
			codec := savebundle.Declaration{Manager: m}
			before := m.ExportState("fed")
			commit, err := codec.PrepareRestore("fed", data)
			if err != nil || commit == nil {
				t.Fatalf("prepare: commit nil=%v err=%v", commit == nil, err)
			}
			if !reflect.DeepEqual(m.ExportState("fed"), before) {
				t.Fatal("prepare cleared live declarations")
			}
			commit()
			if !reflect.DeepEqual(m.ExportState("fed"), m.ExportState("absent")) {
				t.Fatal("present empty snapshot did not clear post-save declarations")
			}
		})
	}
	// The legacy ManagerSnapshotter direct-call contract treats absent bytes as no-op.
	m := declaration.New()
	seedDeclarationCodec(t, m, 1)
	codec := savebundle.Declaration{Manager: m}
	want := m.ExportState("fed")
	if err := codec.Unmarshal("fed", nil); err != nil || !reflect.DeepEqual(m.ExportState("fed"), want) {
		t.Fatalf("legacy absent snapshot changed state: %v", err)
	}
	emptyData, err := codec.Marshal("absent")
	if err != nil || len(emptyData) == 0 {
		t.Fatalf("empty declaration must have a versioned encoding: %q, %v", emptyData, err)
	}
	if err := codec.Unmarshal("fed", emptyData); err != nil || !reflect.DeepEqual(m.ExportState("fed"), m.ExportState("absent")) {
		t.Fatalf("versioned empty snapshot did not clear state: %v", err)
	}
}

func TestDeclarationSnapshotRejectsInvalidDataBeforeMutation(t *testing.T) {
	cases := map[string]string{
		"malformed":                   `{`,
		"null":                        `null`,
		"whitespace":                  ` `,
		"missing version":             `{}`,
		"wrong version":               `{"version":999}`,
		"unknown field":               `{"version":1,"unexpected":1}`,
		"trailing document":           `{"version":1} {"version":1}`,
		"trailing garbage":            `{"version":1} x`,
		"invalid handle":              `{"version":1,"object_publications":[{"class":0,"attribute":2,"federates":[1]}]}`,
		"overflow handle":             `{"version":1,"interaction_publications":[{"class":1,"federates":[18446744073709551616]}]}`,
		"negative handle":             `{"version":1,"interaction_publications":[{"class":1,"federates":[-1]}]}`,
		"null record":                 `{"version":1,"object_subscriptions":[null]}`,
		"duplicate member":            `{"version":1,"interaction_subscriptions":[{"class":1,"federates":[2,2]}]}`,
		"duplicate record":            `{"version":1,"object_subscriptions":[{"class":1,"attribute":2,"federates":[3]},{"class":1,"attribute":2,"federates":[4]}]}`,
		"late invalid record":         `{"version":1,"object_publications":[{"class":1,"attribute":2,"federates":[3]}],"interaction_subscriptions":[{"class":0,"federates":[4]}]}`,
		"duplicate table field":       `{"version":1,"object_publications":[{"class":0,"attribute":2,"federates":[3]}],"object_publications":[]}`,
		"duplicate record field":      `{"version":1,"object_publications":[{"class":0,"class":1,"attribute":2,"federates":[3]}]}`,
		"case folded duplicate field": `{"version":2,"Version":1}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			m := declaration.New()
			seedDeclarationCodec(t, m, 99)
			codec := savebundle.Declaration{Manager: m}
			before := m.ExportState("fed")
			commit, err := codec.PrepareRestore("fed", []byte(data))
			if err == nil || commit != nil {
				t.Fatalf("invalid snapshot accepted: commit nil=%v, err=%v", commit == nil, err)
			}
			if err := codec.Unmarshal("fed", []byte(data)); err == nil {
				t.Fatal("direct unmarshal accepted invalid snapshot")
			}
			if !reflect.DeepEqual(m.ExportState("fed"), before) {
				t.Fatal("invalid snapshot changed live state")
			}
		})
	}
}

func TestDeclarationSnapshotRequiresManager(t *testing.T) {
	codec := savebundle.Declaration{}
	if _, err := codec.Marshal("fed"); err == nil {
		t.Fatal("missing manager accepted by marshal")
	}
	if commit, err := codec.PrepareRestore("fed", []byte(`{"version":1}`)); err == nil || commit != nil {
		t.Fatal("missing manager accepted by prepare")
	}
}
