package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestTransportationCopiesAndJSON(t *testing.T) {
	values := []Transportation{{Name: "HLAreliable", Reliable: "Yes", Semantics: OptionalString{Value: "  reliable  ", Present: true}}, {Name: "HLAbestEffort", Reliable: "No", Semantics: OptionalString{Present: true}}}
	base := NewFOM(nil, nil, nil)
	f := base.WithTransportations(values)
	values[0].Reliable = "No"
	view := f.Transportations()
	view[0].Semantics.Value = testChangedMetadata
	want := []Transportation{{Name: "HLAbestEffort", Reliable: "No", Semantics: OptionalString{Present: true}}, {Name: "HLAreliable", Reliable: "Yes", Semantics: OptionalString{Value: "  reliable  ", Present: true}}}
	if len(base.Transportations()) != 0 || !reflect.DeepEqual(f.Transportations(), want) {
		t.Fatal("metadata copies leaked")
	}
	data, err := json.Marshal(f.Transportations())
	if err != nil {
		t.Fatal(err)
	}
	var fresh []Transportation
	if err := json.Unmarshal(data, &fresh); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh, want) {
		t.Fatalf("snapshot changed declarations: %+v", fresh)
	}
}

func TestTransportationFDDMerge(t *testing.T) {
	states := []OptionalString{{}, {Present: true}, {Present: true, Value: "one"}, {Present: true, Value: " one "}}
	for _, a := range states {
		for _, b := range states {
			old := Transportation{Name: "HLAreliable", Reliable: "Yes", Semantics: a}
			incoming := Transportation{Name: "HLAreliable", Reliable: "Yes", Semantics: b}
			base, next := []Transportation{old}, []Transportation{incoming}
			merged, err := MergeTransportations(base, next)
			if err != nil {
				t.Fatalf("%+v + %+v: result=%+v err=%v", a, b, merged, err)
			}
			want := a
			if !a.Present {
				want = b
			}
			if len(merged) != 1 || merged[0].Semantics != want {
				t.Fatalf("presence lost: %+v", merged)
			}
			if base[0] != old || next[0] != incoming {
				t.Fatal("merge mutated input")
			}
		}
	}
	for _, values := range [][]Transportation{
		{{Name: "X", Reliable: "Yes"}, {Name: "X", Reliable: "No"}},
		{{Name: "X"}}, {{Reliable: "Yes"}}, {{Name: "X", Reliable: "true"}},
	} {
		for _, pair := range [][2][]Transportation{{values, nil}, {nil, values}} {
			if out, err := MergeTransportations(pair[0], pair[1]); err == nil || out != nil {
				t.Fatalf("invalid metadata accepted: %+v, %v", out, err)
			}
		}
	}
}

func TestUpdateRateSemanticsAndExactJSON(t *testing.T) {
	for _, semantics := range []OptionalString{{}, {Present: true}, {Present: true, Value: "  description  "}} {
		rate, err := NewUpdateRate("Precise", testPreciseDecimal, semantics.Value)
		if err != nil {
			t.Fatal(err)
		}
		rate.SemanticsPresent = semantics.Present
		data, err := json.Marshal(rate)
		if err != nil {
			t.Fatal(err)
		}
		var fresh UpdateRate
		if err := json.Unmarshal(data, &fresh); err != nil {
			t.Fatal(err)
		}
		decimal, err := fresh.DecimalString()
		if err != nil || decimal != testPreciseDecimal || fresh.SemanticsDeclaration() != semantics {
			t.Errorf("snapshot lost retained values: %s %+v %q %v", data, fresh, decimal, err)
		}
	}
	for _, tc := range []struct {
		json string
		want OptionalString
	}{
		{`{"Name":"Legacy","Rate":2,"Semantics":""}`, OptionalString{}},
		{`{"Name":"Legacy","Rate":2}`, OptionalString{}},
		{`{"Name":"Legacy","Rate":2,"Semantics":"old"}`, OptionalString{Value: "old", Present: true}},
		{`{"Name":"Legacy","Rate":2,"Semantics":"","SemanticsPresent":true}`, OptionalString{Present: true}},
	} {
		var fresh UpdateRate
		if err := json.Unmarshal([]byte(tc.json), &fresh); err != nil {
			t.Fatal(err)
		}
		if got := fresh.SemanticsDeclaration(); got != tc.want {
			t.Fatalf("legacy JSON %s = %+v", tc.json, got)
		}
	}
}

func TestUpdateRateJSONRejectsCorruptExactDecimalAtomically(t *testing.T) {
	beforeNull, _ := NewUpdateRate("Before", "1.25", "kept")
	afterNull := beforeNull
	if err := json.Unmarshal([]byte("null"), &afterNull); err != nil || afterNull != beforeNull {
		t.Errorf("JSON null changed the existing value: %+v %v", afterNull, err)
	}
	for _, text := range []string{
		`{"Name":"Bad","Rate":1,"ExactRate":"2"}`,
		`{"Name":"Bad","Rate":1,"ExactRate":""}`,
		`{"Name":"Bad","Rate":1,"ExactRate":"1e0"}`,
		`{"Name":"Bad","Rate":1,"ExactRate":true}`,
	} {
		before, _ := NewUpdateRate("Before", "1.25", "")
		fresh := before
		if err := json.Unmarshal([]byte(text), &fresh); err == nil || fresh != before {
			t.Fatalf("corrupt JSON accepted or partially assigned: %+v", fresh)
		}
	}
	mutated, _ := NewUpdateRate("Mutated", "1.25", "")
	mutated.Rate = 2
	if _, err := json.Marshal(mutated); err == nil {
		t.Fatal("JSON hid a mutated exact rate")
	}
}

func TestUpdateRateMergeRetainsFirstDescriptionAcrossBothDeclarationSets(t *testing.T) {
	empty := UpdateRate{Name: "Fast", Rate: 1, SemanticsPresent: true}
	nonempty := UpdateRate{Name: "Fast", Rate: 1, Semantics: "specified"}
	for _, pair := range [][2][]UpdateRate{
		{{empty, nonempty}, nil}, {nil, {empty, nonempty}},
		{{empty}, {nonempty}}, {{nonempty}, {empty}},
	} {
		var first UpdateRate
		if len(pair[0]) > 0 {
			first = pair[0][0]
		} else {
			first = pair[1][0]
		}
		if result, err := MergeUpdateRates(pair[0], pair[1]); err != nil || len(result) != 1 || result[0] != first {
			t.Errorf("description changed or caused FDD rejection: %+v %v", result, err)
		}
	}
}
