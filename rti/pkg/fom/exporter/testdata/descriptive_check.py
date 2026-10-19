"""Independent local IEEE XSD and bounded R1 semantic oracle."""
import copy
import sys

from lxml import etree
import xmlschema

schema = xmlschema.XMLSchema(sys.argv[1])
document = etree.parse(sys.argv[2])
schema.validate(sys.argv[2])
ns = {"f": "http://standards.ieee.org/IEEE1516-2010"}


def check_semantics(doc):
    def elements(path):
        return doc.xpath(path, namespaces=ns)

    def text(path, expected):
        nodes = elements(path)
        assert len(nodes) == 1, path + " must be present exactly once"
        assert (nodes[0].text or "") == expected, path + " changed"

    text("//f:dimension[f:name='Area']/f:dataType", "Scalar")
    text("//f:dimension[f:name='Area']/f:normalization", "  identity map  ")
    for name, value in (("Area", "[2..8)"), ("ExcludedArea", "Excluded"), ("Single", "0"), ("Open", "[8)")):
        text(f"//f:dimension[f:name='{name}']/f:value", value)
    assert not elements("//f:dimension[f:name='Absent']/f:value")
    text("//f:objectClass[f:name='Sensor']/f:semantics", "  A & B sensor.  ")
    text("//f:attribute[f:name='Reading']/f:updateCondition", "when x < 2")
    text("//f:attribute[f:name='Reading']/f:semantics", "")
    text("//f:attribute[f:name='Quiet']/f:updateCondition", "")
    assert not elements("//f:attribute[f:name='Quiet']/f:semantics")
    text("//f:interactionClass[f:name='Event']/f:semantics", "Event description")
    text("//f:parameter[f:name='Payload']/f:semantics", "  Payload details  ")
    text("//f:parameter[f:name='Empty']/f:semantics", "")
    text("//f:simpleData[f:name='Distance']/f:semantics", "Measured distance")
    text("//f:enumeratedData[f:name='Choice']/f:semantics", "")
    text("//f:arrayData[f:name='List']/f:semantics", "Ordered observations")
    text("//f:fixedRecordData[f:name='Record']/f:semantics", "Record description")
    text("//f:fixedRecordData[f:name='Record']/f:field[f:name='Values']/f:semantics", "  all samples  ")
    text("//f:fixedRecordData[f:name='Record']/f:field[f:name='Empty']/f:semantics", "")
    text("//f:variantRecordData[f:name='Variant']/f:semantics", "Variant description")
    text("//f:variantRecordData[f:name='Variant']/f:alternative[f:name='One']/f:semantics", "Record alternative")
    text("//f:variantRecordData[f:name='Variant']/f:alternative[f:name='Two']/f:semantics", "")
    if sys.argv[3] == "merged":
        text("//f:dimension[f:name='HLAfederate']/f:dataType", "HLAnormalizedFederateHandle")
        text("//f:dimension[f:name='HLAserviceGroup']/f:dataType", "HLAnormalizedServiceGroup")
        for name in ("HLAfederate", "HLAserviceGroup"):
            text(f"//f:dimension[f:name='{name}']/f:value", "Excluded")
        assert elements("//f:attribute[f:name='HLAcurrentFDD']/f:semantics")
        assert elements("//f:attribute[f:name='HLAcurrentFDD']/f:updateCondition")


check_semantics(document)
for mutation in ("drop-datatype", "drop-default", "drop-empty", "trim-normalization", "change-update-condition"):
    changed = copy.deepcopy(document)
    path = {
        "drop-datatype": "//f:dimension[f:name='Area']/f:dataType",
        "drop-default": "//f:dimension[f:name='Area']/f:value",
        "drop-empty": "//f:attribute[f:name='Reading']/f:semantics",
        "trim-normalization": "//f:dimension[f:name='Area']/f:normalization",
        "change-update-condition": "//f:attribute[f:name='Reading']/f:updateCondition",
    }[mutation]
    element = changed.xpath(path, namespaces=ns)[0]
    if mutation.startswith("drop-"):
        element.getparent().remove(element)
    elif mutation == "trim-normalization":
        element.text = element.text.strip()
    else:
        element.text = "changed"
    try:
        check_semantics(changed)
    except AssertionError:
        pass
    else:
        raise AssertionError(mutation + " escaped the semantic oracle")

for mutation in ("wrong-value-name", "wrong-default", "empty-normalization", "semantics-order"):
    changed = copy.deepcopy(document)
    if mutation == "semantics-order":
        element = changed.xpath("//f:attribute[f:name='Reading']/f:semantics", namespaces=ns)[0]
        element.getparent().insert(0, element)
    else:
        element = changed.xpath("//f:dimension[f:name='Area']/f:" + ("normalization" if mutation == "empty-normalization" else "value"), namespaces=ns)[0]
        if mutation == "wrong-value-name":
            element.tag = "{" + ns["f"] + "}defaultValue"
        else:
            element.text = "" if mutation == "empty-normalization" else "Included"
    assert not schema.is_valid(etree.tostring(changed)), mutation + " escaped the schema oracle"

print("IEEE XSD + R1 semantic vectors + 5 semantic/4 schema mutations passed")
