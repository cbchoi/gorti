package savebundle

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type fomDescriptiveNode struct {
	XMLName  xml.Name
	Text     string               `xml:",chardata"`
	Children []fomDescriptiveNode `xml:",any"`
}

// Nil means absent; a pointer to "" means an explicit empty XML element.
// This oracle reads fixture/output XML, never the production FOM model.
func fomDescriptiveRecords(t *testing.T, data []byte) map[string]map[string]*string {
	t.Helper()
	var root fomDescriptiveNode
	if err := xml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if root.XMLName.Local != "objectModel" {
		t.Fatalf("unexpected XML root: %+v", root.XMLName)
	}
	records := make(map[string]map[string]*string)
	var walk func(fomDescriptiveNode, string)
	walk = func(n fomDescriptiveNode, path string) {
		path += "/" + n.XMLName.Local
		var name string
		for _, child := range n.Children {
			if child.XMLName.Local == "name" {
				name = child.Text
				path += "[" + name + "]"
				break
			}
		}
		var fields map[string]*string
		switch n.XMLName.Local {
		case "dimension":
			if name != "" {
				fields = map[string]*string{"dataType": nil, "normalization": nil, "value": nil}
			}
		case "attribute":
			fields = map[string]*string{"semantics": nil, "updateCondition": nil}
		case "objectClass", "interactionClass", "parameter", "simpleData", "enumeratedData", "arrayData", "fixedRecordData", "field", "variantRecordData", "alternative":
			// The MIM supplies root semantics to the fixture's empty containers.
			if name != "HLAobjectRoot" && name != "HLAinteractionRoot" {
				fields = map[string]*string{"semantics": nil}
			}
		}
		if fields != nil {
			for _, child := range n.Children {
				if _, selected := fields[child.XMLName.Local]; selected {
					value := child.Text
					fields[child.XMLName.Local] = &value
				}
			}
			if _, duplicate := records[path]; duplicate {
				t.Fatalf("duplicate descriptive declaration: %s", path)
			}
			records[path] = fields
		}
		for _, child := range n.Children {
			walk(child, path)
		}
	}
	walk(root, "")
	return records
}

func assertFOMDescriptiveRecords(t *testing.T, data []byte, want map[string]map[string]*string) {
	t.Helper()
	got := fomDescriptiveRecords(t, data)
	for path, fields := range want {
		actual, present := got[path]
		if !present {
			t.Errorf("missing descriptive declaration %s", path)
			continue
		}
		for field, expected := range fields {
			value, known := actual[field]
			if !known || !reflect.DeepEqual(value, expected) {
				gotJSON, _ := json.Marshal(value)
				wantJSON, _ := json.Marshal(expected)
				t.Errorf("%s/%s: got %s, want %s (null means absent)", path, field, gotJSON, wantJSON)
			}
		}
	}
	for name, datatype := range map[string]string{"HLAfederate": "HLAnormalizedFederateHandle", "HLAserviceGroup": "HLAnormalizedServiceGroup"} {
		fields := got["/objectModel/dimensions/dimension["+name+"]"]
		if fields["dataType"] == nil || *fields["dataType"] != datatype || fields["value"] == nil || *fields["value"] != "Excluded" {
			t.Errorf("MIM dimension %s lost its datatype/default", name)
		}
	}
}

func descriptiveBundleFixture(t *testing.T) (*composition.Repository, core.FederationName, []byte) {
	t.Helper()
	source, err := os.ReadFile("../../../pkg/fom/exporter/testdata/descriptive.xml")
	if err != nil {
		t.Fatal(err)
	}
	if records := fomDescriptiveRecords(t, source); len(records) != 20 {
		t.Fatalf("fixture should exercise all 20 descriptive declarations, got %d", len(records))
	}
	const fed core.FederationName = "descriptive-savebundle"
	repo := composition.NewFOMRepository()
	h, err := repo.Load(context.Background(), []core.FOMModule{{Path: "unreadable/descriptive-source.xml", XML: source}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor(fed, h)
	return repo, fed, source
}

func descriptiveBundleFDD(t *testing.T, repo *composition.Repository, fed core.FederationName) []byte {
	t.Helper()
	data, err := repo.CurrentFDD(context.Background(), fed)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFOMDescriptiveSnapshotRoundTrip(t *testing.T) {
	repo, fed, source := descriptiveBundleFixture(t)
	want := fomDescriptiveRecords(t, source)
	const addition = `<objectModel>
<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Sensor</name><objectClass><name>SavedSensor</name><sharing>PublishSubscribe</sharing><attribute><name>LateValue</name><dataType>HLAinteger32BE</dataType><updateCondition/><transportation>HLAreliable</transportation><order>Receive</order><semantics>  saved addition  </semantics></attribute></objectClass></objectClass></objectClass></objects>
<interactions><interactionClass><name>HLAinteractionRoot</name><interactionClass><name>Event</name><interactionClass><name>SavedEvent</name><sharing>PublishSubscribe</sharing><transportation>HLAreliable</transportation><order>Receive</order><parameter><name>LateParameter</name><dataType>HLAinteger32BE</dataType><semantics/></parameter></interactionClass></interactionClass></interactionClass></interactions>
<dimensions><dimension><name>AAAdded</name><dataType>HLAinteger32BE</dataType><upperBound>100</upperBound><normalization>  saved units  </normalization><value>[5)</value></dimension></dimensions>
</objectModel>`
	for path, fields := range fomDescriptiveRecords(t, []byte(addition)) {
		if _, container := want[path]; !container {
			want[path] = fields
		}
	}
	pending, err := repo.PrepareLiveExtension(context.Background(), fed, []core.FOMModule{{Path: "unreadable/saved-join.xml", XML: []byte(addition)}})
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Release()
	if err := pending.Commit(); err != nil {
		t.Fatal(err)
	}
	before := descriptiveBundleFDD(t, repo, fed)
	assertFOMDescriptiveRecords(t, before, want)
	codec := FOM{Repository: repo}
	snapshot, err := codec.Marshal(fed)
	if err != nil {
		t.Fatal(err)
	}
	var saved core.FOMRepositoryState
	if err := json.Unmarshal(snapshot, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version != 1 || saved.View.Revision != 2 || len(saved.View.Modules) != 2 || !bytes.Equal(saved.View.Modules[0].XML, source) || !bytes.Equal(saved.View.Modules[1].XML, []byte(addition)) {
		t.Fatal("snapshot did not retain both exact committed modules and revision")
	}
	repo.ForgetFor(fed)
	if _, err := repo.CurrentFDD(context.Background(), fed); err == nil {
		t.Fatal("source repository was not removed before independent restore")
	}
	restored := composition.NewFOMRepository()
	restoredCodec := FOM{Repository: restored}
	if err := restoredCodec.Unmarshal(fed, snapshot); err != nil {
		t.Fatal(err)
	}
	after := descriptiveBundleFDD(t, restored, fed)
	assertFOMDescriptiveRecords(t, after, want)
	if !bytes.Equal(before, after) {
		t.Fatal("real savebundle round-trip changed saved FDD")
	}
	view, err := restored.FOMView(context.Background(), fed)
	if err != nil || !reflect.DeepEqual(saved.View, view) {
		t.Fatalf("savebundle restore changed source, revision, or assigned handles: %v", err)
	}
	again, err := restoredCodec.Marshal(fed)
	if err != nil || !bytes.Equal(snapshot, again) {
		t.Fatalf("restored repository did not reproduce the saved snapshot: %v", err)
	}
}

func TestFOMDescriptivePreparedRestoreIsAtomic(t *testing.T) {
	repo, fed, source := descriptiveBundleFixture(t)
	ctx := context.Background()
	codec := FOM{Repository: repo}
	savedFDD := descriptiveBundleFDD(t, repo, fed)
	wantSaved := fomDescriptiveRecords(t, source)
	assertFOMDescriptiveRecords(t, savedFDD, wantSaved)
	snapshot, err := codec.Marshal(fed)
	if err != nil {
		t.Fatal(err)
	}
	replacement := strings.Replace(string(source), "  A &amp; B sensor.  ", "  live replacement  ", 1)
	replacement = strings.Replace(replacement, "<value>[2..8)</value>", "<value>[3..9)</value>", 1)
	h, err := repo.Load(ctx, []core.FOMModule{{Path: "unreadable/live-replacement.xml", XML: []byte(replacement)}})
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor(fed, h)
	liveFDD := descriptiveBundleFDD(t, repo, fed)
	if bytes.Equal(liveFDD, savedFDD) {
		t.Fatal("replacement did not change the live descriptive metadata")
	}
	wantLive := fomDescriptiveRecords(t, []byte(replacement))
	liveSnapshot, err := codec.Marshal(fed)
	if err != nil {
		t.Fatal(err)
	}
	checkUnchanged := func() {
		t.Helper()
		got := descriptiveBundleFDD(t, repo, fed)
		assertFOMDescriptiveRecords(t, got, wantLive)
		if !bytes.Equal(got, liveFDD) {
			t.Fatal("uncommitted restore changed the current FDD")
		}
		current, err := codec.Marshal(fed)
		if err != nil || !bytes.Equal(current, liveSnapshot) {
			t.Fatalf("uncommitted restore changed the live snapshot: %v", err)
		}
	}
	for _, tc := range []struct{ name, old, next string }{
		{"changed_whitespace", "  identity map  ", "identity map"},
		{"changed_default", "[2..8)", "Excluded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var changed core.FOMRepositoryState
			if err := json.Unmarshal(snapshot, &changed); err != nil {
				t.Fatal(err)
			}
			changed.View.Modules[0].XML = []byte(strings.Replace(string(changed.View.Modules[0].XML), tc.old, tc.next, 1))
			corrupt, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := codec.PrepareRestoreState(fed, corrupt)
			if pending != nil {
				pending.Release()
			}
			if err == nil {
				t.Fatal("snapshot source metadata changed without a matching digest")
			}
			checkUnchanged()
		})
	}
	for _, commit := range []bool{false, true} {
		pending, err := codec.PrepareRestoreState(fed, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		defer pending.Release()
		checkUnchanged()
		if !commit {
			pending.Release()
			checkUnchanged()
			continue
		}
		if err := pending.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	got := descriptiveBundleFDD(t, repo, fed)
	assertFOMDescriptiveRecords(t, got, wantSaved)
	if !bytes.Equal(got, savedFDD) {
		t.Fatal("committed restore did not publish the saved FDD")
	}
	again, err := codec.Marshal(fed)
	if err != nil || !bytes.Equal(again, snapshot) {
		t.Fatalf("committed restore changed saved source or bindings: %v", err)
	}
}
