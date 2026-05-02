package parser

import "testing"

// TestSpec_VariantDiscriminator_AcceptsDeclaredDiscriminant asserts a
// variantRecord with a non-empty <discriminant> does not produce FOM-013.
func TestSpec_VariantDiscriminator_AcceptsDeclaredDiscriminant(t *testing.T) {
	t.Parallel()

	good := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
  <dataTypes>
    <variantRecordDataTypes>
      <variantRecordData>
        <name>OK</name>
        <discriminant>kind</discriminant>
        <dataType>HLAinteger32BE</dataType>
        <encoding>HLAvariantRecord</encoding>
        <alternative>
          <enumerator>1</enumerator>
          <name>v1</name>
          <dataType>HLAinteger32BE</dataType>
        </alternative>
      </variantRecordData>
    </variantRecordDataTypes>
  </dataTypes>
</objectModel>`)

	res, err := Parse([]Module{{Path: "g.xml", XML: good}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "FOM-013" {
			t.Fatalf("unexpected FOM-013 with declared discriminant: %+v", d)
		}
	}
}

// TestSpec_VariantDiscriminator_RejectsMissingDiscriminant asserts a
// variantRecord without <discriminant> produces FOM-013.
func TestSpec_VariantDiscriminator_RejectsMissingDiscriminant(t *testing.T) {
	t.Parallel()

	bad := []byte(`<?xml version="1.0"?>
<objectModel xmlns="http://standards.ieee.org/IEEE1516-2010">
  <modelIdentification><name>x</name><type>FOM</type></modelIdentification>
  <objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>
  <interactions><interactionClass><name>HLAinteractionRoot</name></interactionClass></interactions>
  <dataTypes>
    <variantRecordDataTypes>
      <variantRecordData>
        <name>Bad</name>
        <dataType>HLAinteger32BE</dataType>
        <encoding>HLAvariantRecord</encoding>
        <alternative>
          <enumerator>1</enumerator>
          <name>v1</name>
          <dataType>HLAinteger32BE</dataType>
        </alternative>
      </variantRecordData>
    </variantRecordDataTypes>
  </dataTypes>
</objectModel>`)

	res, err := Parse([]Module{{Path: "bad.xml", XML: bad}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !res.HasCode("FOM-013") {
		t.Fatalf("want FOM-013, got %v", res.CodeCounts())
	}
}
