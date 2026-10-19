package composition

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/exporter"
)

const descriptiveJoinXML = `<objectModel>
<objects><objectClass><name>HLAobjectRoot</name>
<objectClass><name>AAJoined</name><semantics/></objectClass>
<objectClass><name>Sensor</name><objectClass><name>JoinedSensor</name><sharing>PublishSubscribe</sharing><attribute><name>Joined</name><dataType>HLAinteger32BE</dataType><updateCondition>  on join &amp; change  </updateCondition><dimensions><dimension>JoinedArea</dimension></dimensions><transportation>HLAreliable</transportation><order>Receive</order><semantics/></attribute></objectClass></objectClass>
</objectClass></objects>
<interactions><interactionClass><name>HLAinteractionRoot</name>
<interactionClass><name>AAJoinedEvent</name><transportation>HLAreliable</transportation><order>Receive</order><semantics>  joined event  </semantics></interactionClass>
<interactionClass><name>Event</name><interactionClass><name>JoinedEvent</name><sharing>PublishSubscribe</sharing><transportation>HLAreliable</transportation><order>Receive</order><parameter><name>JoinedPayload</name><dataType>HLAinteger32BE</dataType><semantics>  joined payload  </semantics></parameter></interactionClass></interactionClass>
</interactionClass></interactions>
<dimensions><dimension><name>JoinedArea</name><dataType>HLAinteger32BE</dataType><upperBound>128</upperBound><normalization>  joined scale  </normalization><value>[1..9)</value></dimension></dimensions>
<dataTypes><simpleDataTypes><simpleData><name>JoinedScalar</name><representation>HLAinteger32BE</representation><semantics>  joined scalar  </semantics></simpleData></simpleDataTypes></dataTypes>
</objectModel>`

func descriptiveRepository(t *testing.T) (*Repository, *fomHandle, []byte) {
	t.Helper()
	source, err := os.ReadFile("../../../pkg/fom/exporter/testdata/descriptive.xml")
	if err != nil {
		t.Fatal(err)
	}
	r := NewFOMRepository()
	h, err := r.Load(context.Background(), []core.FOMModule{{Path: "unreadable/descriptive-source.xml", XML: source}})
	if err != nil {
		t.Fatal(err)
	}
	r.RememberFor("fdd", h)
	return r, h.(*fomHandle), source
}

// Decode output independently of the FOM parser so dropped fields, absent
// elements and explicit empty strings cannot agree through a shared model bug.
type fddDescriptionNode struct {
	XMLName  xml.Name
	Text     string               `xml:",chardata"`
	Children []fddDescriptionNode `xml:",any"`
}

func descriptiveFDDScalars(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var root fddDescriptionNode
	if err := xml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if root.XMLName.Local != "objectModel" || root.XMLName.Space != exporter.Namespace {
		t.Fatalf("unexpected FDD root: %+v", root.XMLName)
	}
	values := make(map[string]string)
	var walk func(fddDescriptionNode, string)
	walk = func(n fddDescriptionNode, path string) {
		path += "/" + n.XMLName.Local
		for _, child := range n.Children {
			if child.XMLName.Local == "name" {
				path += "[" + child.Text + "]"
				break
			}
		}
		if len(n.Children) == 0 {
			values[path] = n.Text
		}
		for _, child := range n.Children {
			walk(child, path)
		}
	}
	walk(root, "")
	return values
}

func assertDescriptiveCurrentFDD(t *testing.T, data []byte, joined bool) {
	t.Helper()
	const objects = "/objectModel/objects/objectClass[HLAobjectRoot]"
	const sensor = objects + "/objectClass[Sensor]"
	const interactions = "/objectModel/interactions/interactionClass[HLAinteractionRoot]"
	const event = interactions + "/interactionClass[Event]"
	const dimensions = "/objectModel/dimensions"
	const types = "/objectModel/dataTypes"
	const record = types + "/fixedRecordDataTypes/fixedRecordData[Record]"
	const variant = types + "/variantRecordDataTypes/variantRecordData[Variant]"
	want := map[string]string{
		sensor + "/semantics":                                           "  A & B sensor.  ",
		sensor + "/attribute[Reading]/semantics":                        "",
		sensor + "/attribute[Reading]/updateCondition":                  "when x < 2",
		sensor + "/attribute[Quiet]/updateCondition":                    "",
		event + "/semantics":                                            "Event description",
		event + "/parameter[Payload]/semantics":                         "  Payload details  ",
		event + "/parameter[Empty]/semantics":                           "",
		dimensions + "/dimension[Area]/dataType":                        "Scalar",
		dimensions + "/dimension[Area]/upperBound":                      "100",
		dimensions + "/dimension[Area]/normalization":                   "  identity map  ",
		dimensions + "/dimension[Area]/value":                           "[2..8)",
		dimensions + "/dimension[ExcludedArea]/dataType":                "Scalar",
		dimensions + "/dimension[ExcludedArea]/value":                   "Excluded",
		dimensions + "/dimension[Single]/value":                         "0",
		dimensions + "/dimension[Open]/value":                           "[8)",
		dimensions + "/dimension[Absent]/name":                          "Absent",
		dimensions + "/dimension[HLAfederate]/dataType":                 "HLAnormalizedFederateHandle",
		dimensions + "/dimension[HLAfederate]/value":                    "Excluded",
		dimensions + "/dimension[HLAserviceGroup]/dataType":             "HLAnormalizedServiceGroup",
		dimensions + "/dimension[HLAserviceGroup]/value":                "Excluded",
		types + "/simpleDataTypes/simpleData[Distance]/semantics":       "Measured distance",
		types + "/enumeratedDataTypes/enumeratedData[Choice]/semantics": "",
		types + "/arrayDataTypes/arrayData[List]/semantics":             "Ordered observations",
		record + "/semantics":                                           "Record description",
		record + "/field[Values]/semantics":                             "  all samples  ",
		record + "/field[Empty]/semantics":                              "",
		variant + "/semantics":                                          "Variant description",
		variant + "/alternative[One]/semantics":                         "Record alternative",
		variant + "/alternative[Two]/semantics":                         "",
	}
	absent := []string{
		sensor + "/attribute[Quiet]/semantics",
		dimensions + "/dimension[Absent]/dataType",
		dimensions + "/dimension[Absent]/normalization",
		dimensions + "/dimension[Absent]/value",
		dimensions + "/dimension[Absent]/upperBound",
		dimensions + "/dimension[Single]/dataType",
		dimensions + "/dimension[Open]/normalization",
		types + "/basicDataRepresentations/basicData[Scalar]/semantics",
	}
	added := map[string]string{
		objects + "/objectClass[AAJoined]/semantics":                                "",
		sensor + "/objectClass[JoinedSensor]/attribute[Joined]/semantics":           "",
		sensor + "/objectClass[JoinedSensor]/attribute[Joined]/updateCondition":     "  on join & change  ",
		interactions + "/interactionClass[AAJoinedEvent]/semantics":                 "  joined event  ",
		event + "/interactionClass[JoinedEvent]/parameter[JoinedPayload]/semantics": "  joined payload  ",
		dimensions + "/dimension[JoinedArea]/dataType":                              "HLAinteger32BE",
		dimensions + "/dimension[JoinedArea]/normalization":                         "  joined scale  ",
		dimensions + "/dimension[JoinedArea]/value":                                 "[1..9)",
		types + "/simpleDataTypes/simpleData[JoinedScalar]/semantics":               "  joined scalar  ",
	}
	for path, value := range added {
		if joined {
			want[path] = value
		} else {
			absent = append(absent, path)
		}
	}
	got := descriptiveFDDScalars(t, data)
	for path, value := range want {
		if actual, present := got[path]; !present || actual != value {
			t.Errorf("%s: got %q (present=%v), want %q", path, actual, present, value)
		}
	}
	for _, path := range absent {
		if value, present := got[path]; present {
			t.Errorf("%s should be absent, got %q", path, value)
		}
	}
}

func TestCurrentFDDDescriptiveJoinPendingCommitAndRestore(t *testing.T) {
	r, base, _ := descriptiveRepository(t)
	ctx := context.Background()
	created := currentFDD(t, r)
	assertDescriptiveCurrentFDD(t, created, false)
	snapshot, err := r.ExportState(ctx, "fdd")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var saved FOMRepositoryState
	if err := json.Unmarshal(encoded, &saved); err != nil {
		t.Fatal(err)
	}
	checkCommitted := func(want []byte, state *FOMRepositoryState, joined bool) {
		t.Helper()
		got := currentFDD(t, r)
		assertDescriptiveCurrentFDD(t, got, joined)
		if !bytes.Equal(got, want) {
			t.Fatal("pending candidate changed committed FDD")
		}
		actual, err := r.ExportState(ctx, "fdd")
		if err != nil || !reflect.DeepEqual(actual, state) {
			t.Fatalf("committed snapshot changed: %v", err)
		}
	}
	module := core.FOMModule{Path: "unreadable/join.xml", XML: []byte(descriptiveJoinXML)}
	for _, commit := range []bool{false, true} {
		pending, err := r.PrepareLiveExtension(ctx, "fdd", []core.FOMModule{module})
		if err != nil {
			t.Fatal(err)
		}
		defer pending.Release()
		candidate := pending.FOM().(*fomHandle)
		data, err := exporter.Marshal(candidate.fom)
		if err != nil {
			t.Fatal(err)
		}
		assertDescriptiveCurrentFDD(t, data, true)
		assertStableBindings(t, base.View(), candidate)
		if pending.View().Revision != saved.View.Revision+1 {
			t.Fatal("join candidate did not advance the revision")
		}
		checkCommitted(created, &saved, false)
		if err := pending.PrepareCommit(); err != nil {
			t.Fatal(err)
		}
		checkCommitted(created, &saved, false)
		if !commit {
			pending.Release()
			if err := pending.Commit(); !errors.Is(err, ErrFOMExtensionReleased) {
				t.Fatalf("released join committed: %v", err)
			}
			checkCommitted(created, &saved, false)
			continue
		}
		if err := pending.Commit(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, currentFDD(t, r)) {
			t.Fatal("commit did not publish the checked candidate")
		}
	}
	joined := currentFDD(t, r)
	assertDescriptiveCurrentFDD(t, joined, true)
	joinedState, err := r.ExportState(ctx, "fdd")
	if err != nil {
		t.Fatal(err)
	}
	for _, commit := range []bool{false, true} {
		pending, err := r.PrepareImportState(ctx, "fdd", &saved)
		if err != nil {
			t.Fatal(err)
		}
		defer pending.Release()
		candidate := pending.FOM().(*fomHandle)
		data, err := exporter.Marshal(candidate.fom)
		if err != nil {
			t.Fatal(err)
		}
		assertDescriptiveCurrentFDD(t, data, false)
		assertStableBindings(t, saved.View, candidate)
		if !bytes.Equal(created, data) {
			t.Fatal("restore candidate did not recover saved metadata")
		}
		checkCommitted(joined, joinedState, true)
		if err := pending.PrepareCommit(); err != nil {
			t.Fatal(err)
		}
		checkCommitted(joined, joinedState, true)
		if !commit {
			pending.Release()
			checkCommitted(joined, joinedState, true)
			continue
		}
		if err := pending.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	checkCommitted(created, &saved, false)
}

// IEEE 1516.1-2010 4.1.4-4.1.4.1, PDF pp. 47-48: description-only
// differences do not reject FDD combination. The first-description winner
// is the implementation's retention policy, not a normative winner rule.
func TestCurrentFDDCompatibleDescriptionsRetainFirst(t *testing.T) {
	for _, tc := range []struct{ name, old, next string }{
		{"object_semantics", "  A &amp; B sensor.  ", ""},
		{"attribute_empty", "<semantics></semantics>", "<semantics>changed</semantics>"},
		{"update_condition", "when x &lt; 2", ""},
		{"interaction_semantics", "Event description", "different event"},
		{"parameter_whitespace", "  Payload details  ", "Payload details"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, base, source := descriptiveRepository(t)
			ctx := context.Background()
			before := currentFDD(t, r)
			state, err := r.ExportState(ctx, "fdd")
			if err != nil {
				t.Fatal(err)
			}
			changed := strings.Replace(string(source), tc.old, tc.next, 1)
			if changed == string(source) {
				t.Fatal("description fixture unchanged")
			}
			module := core.FOMModule{Path: tc.name + ".xml", XML: []byte(changed)}
			for _, commit := range []bool{false, true} {
				pending, err := r.PrepareLiveExtension(ctx, "fdd", []core.FOMModule{module})
				if err != nil {
					t.Fatal(err)
				}
				defer pending.Release()
				candidate := pending.FOM().(*fomHandle)
				assertStableBindings(t, base.View(), candidate)
				data, err := exporter.Marshal(candidate.fom)
				if err != nil || !bytes.Equal(data, before) {
					t.Fatalf("description merge changed retained FDD: %v", err)
				}
				assertDescriptiveCurrentFDD(t, data, false)
				view := pending.View()
				if view.Revision != state.View.Revision+1 || len(view.Modules) != 2 || view.Digest == state.View.Digest || !bytes.Equal(view.Modules[0].XML, source) || !bytes.Equal(view.Modules[1].XML, module.XML) {
					t.Fatal("candidate lost source modules, digest, or revision")
				}
				if err := pending.PrepareCommit(); err != nil {
					t.Fatal(err)
				}
				actual, err := r.ExportState(ctx, "fdd")
				if err != nil || !reflect.DeepEqual(actual, state) || !bytes.Equal(currentFDD(t, r), before) {
					t.Fatalf("uncommitted descriptive join changed live state: %v", err)
				}
				if !commit {
					pending.Release()
					if err := pending.Commit(); !errors.Is(err, ErrFOMExtensionReleased) {
						t.Fatalf("released join committed: %v", err)
					}
					actual, err := r.ExportState(ctx, "fdd")
					if err != nil || !reflect.DeepEqual(actual, state) {
						t.Fatalf("released join changed live state: %v", err)
					}
					continue
				}
				if err := pending.Commit(); err != nil {
					t.Fatal(err)
				}
				actual, err = r.ExportState(ctx, "fdd")
				if err != nil || !reflect.DeepEqual(actual.View, view) || !bytes.Equal(currentFDD(t, r), before) {
					t.Fatalf("commit lost first descriptions or candidate state: %v", err)
				}
				restored := NewFOMRepository()
				restore, err := restored.PrepareImportState(ctx, "fdd", actual)
				if err != nil {
					t.Fatal(err)
				}
				defer restore.Release()
				if _, err := restored.Get(ctx, "fdd"); !errors.Is(err, core.ErrFederationNotFound) {
					t.Fatal("prepared restore published early")
				}
				if err := restore.Commit(); err != nil {
					t.Fatal(err)
				}
				roundTrip, err := restored.ExportState(ctx, "fdd")
				if err != nil || !reflect.DeepEqual(roundTrip, actual) || !bytes.Equal(currentFDD(t, restored), before) {
					t.Fatalf("restore changed descriptions, handles, or committed source: %v", err)
				}
			}
		})
	}
}

func TestCurrentFDDDescriptiveConflictingJoinIsInvisible(t *testing.T) {
	r, _, source := descriptiveRepository(t)
	ctx := context.Background()
	before := currentFDD(t, r)
	state, err := r.ExportState(ctx, "fdd")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, next string }{
		{"dimension_datatype", "<name>Area</name><dataType>Scalar</dataType>", "<name>Area</name><dataType>Distance</dataType>"},
		{"dimension_default", "<value>[2..8)</value>", "<value>Excluded</value>"},
		{"normalization_whitespace", "  identity map  ", "identity map"},
		{"attribute_order", "<order>Receive</order>", "<order>TimeStamp</order>"},
		{"attribute_transportation", "<transportation>HLAreliable</transportation>", "<transportation>HLAbestEffort</transportation>"},
		{"attribute_member_set", "<name>Reading</name>", "<name>DifferentReading</name>"},
		{"parameter_member_set", "<name>Payload</name>", "<name>DifferentPayload</name>"},
		{"simple_semantics", "Measured distance", "different distance"},
		{"enumerated_empty_absent", "<name>Choice</name><representation>Scalar</representation><semantics/>", "<name>Choice</name><representation>Scalar</representation>"},
		{"array_semantics", "Ordered observations", "reversed observations"},
		{"record_semantics", "Record description", "different record"},
		{"record_field_whitespace", "  all samples  ", "all samples"},
		{"variant_semantics", "Variant description", "different variant"},
		{"alternative_semantics", "Record alternative", "different alternative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := strings.Replace(string(source), tc.old, tc.next, 1)
			if changed == string(source) {
				t.Fatal("conflict fixture did not change")
			}
			pending, err := r.PrepareLiveExtension(ctx, "fdd", []core.FOMModule{{Path: tc.name + ".xml", XML: []byte(changed)}})
			if pending != nil {
				pending.Release()
			}
			if err == nil {
				t.Fatal("conflicting metadata accepted")
			}
			got := currentFDD(t, r)
			assertDescriptiveCurrentFDD(t, got, false)
			if !bytes.Equal(before, got) {
				t.Fatal("failed join changed current FDD")
			}
			actual, err := r.ExportState(ctx, "fdd")
			if err != nil || !reflect.DeepEqual(state, actual) {
				t.Fatalf("failed join changed committed snapshot: %v", err)
			}
		})
	}
}
