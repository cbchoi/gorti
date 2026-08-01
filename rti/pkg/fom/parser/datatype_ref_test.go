package parser

import "testing"

// TestSpec_DataTypeRef_AcceptsMIMReference asserts that an attribute typed
// HLAfloat64BE (a MIM-provided basic) does not produce FOM-001 even though
// no <basicData> declares it locally — MIM names resolve.
func TestSpec_DataTypeRef_AcceptsMIMReference(t *testing.T) {
	t.Parallel()

	res, err := Parse([]Module{{Path: "x.xml", XML: []byte(minimalFOM)}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-001" {
			t.Fatalf("unexpected FOM-001 on minimal good FOM: %+v", d)
		}
	}
}

// TestSpec_DataTypeRef_RejectsUndeclaredAttributeType asserts that an
// attribute referencing a name that is neither a MIM primitive nor a
// FOM-declared dataType produces FOM-001.
func TestSpec_DataTypeRef_RejectsUndeclaredAttributeType(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>X</name>
        <attribute>
          <name>a</name>
          <dataType>NotAType</dataType>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCode("FOM-001") {
		t.Fatalf("want FOM-001, got %v", res.CodeCounts())
	}
	if res.FOM != nil {
		t.Errorf("want FOM nil on rejection")
	}
}

// TestSpec_DataTypeRef_RejectsUndeclaredParameterType is the symmetric
// case for interaction parameters.
func TestSpec_DataTypeRef_RejectsUndeclaredParameterType(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>
  <interactions>
    <interactionClass>
      <name>HLAinteractionRoot</name>
      <interactionClass>
        <name>I</name>
        <parameter>
          <name>p</name>
          <dataType>NoSuchType</dataType>
        </parameter>
      </interactionClass>
    </interactionClass>
  </interactions>
</objectModel>`)

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCode("FOM-001") {
		t.Fatalf("want FOM-001, got %v", res.CodeCounts())
	}
}

// TestSpec_DataTypeRef_AcceptsLocallyDeclaredType asserts a FOM-declared
// type (e.g. SimpleData) resolves; no FOM-001.
func TestSpec_DataTypeRef_AcceptsLocallyDeclaredType(t *testing.T) {
	t.Parallel()

	good := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects>
    <objectClass>
      <name>HLAobjectRoot</name>
      <objectClass>
        <name>X</name>
        <attribute>
          <name>a</name>
          <dataType>Velocity</dataType>
        </attribute>
      </objectClass>
    </objectClass>
  </objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
  <dataTypes>
    <simpleDataTypes>
      <simpleData>
        <name>Velocity</name>
        <representation>HLAfloat64BE</representation>
      </simpleData>
    </simpleDataTypes>
  </dataTypes>
</objectModel>`)

	res, err := Parse([]Module{{Path: "g.xml", XML: good}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-001" {
			t.Fatalf("unexpected FOM-001 on FOM with declared SimpleData: %+v", d)
		}
	}
}
