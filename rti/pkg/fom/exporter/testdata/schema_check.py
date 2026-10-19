"""Independent IEEE schema and semantic checks; never downloads the schema."""
import copy
import sys
from lxml import etree
import xmlschema

schema = xmlschema.XMLSchema(sys.argv[1])
document = etree.parse(sys.argv[2])
schema.validate(sys.argv[2])
ns = {"f": "http://standards.ieee.org/IEEE1516-2010"}
def query(path):
    return document.xpath(path, namespaces=ns)

assert query("/f:objectModel/f:updateRates/f:updateRate[f:name='Precise']/f:rate/text()") == ["1.0000000000000000000000000001"]
assert query("//f:enumeratedData[f:name='Choice']/f:enumerator[f:name='First']/f:value/text()") == ["1", "2"]
assert query("//f:fixedRecordData[f:name='Record']/f:field/f:name/text()") == ["ZFirst", "ASecond"]
assert query("//f:variantRecordData[f:name='Variant']/f:alternative/f:name/text()") == ["One", "Two"]
assert query("/f:objectModel/f:switches/f:autoProvide/@isEnabled") == ["true"]
assert query("/f:objectModel/f:switches/f:exceptionReporting/@isEnabled") == ["false"]
assert query("/f:objectModel/f:switches/f:automaticResignAction/@resignAction") == ["DeleteObjectsThenDivest"]
assert query("/f:objectModel/f:dimensions/f:dimension[f:name='Area']/f:upperBound/text()") == ["18446744073709551615"]
assert query("//f:objectClass[f:name='Parent']/f:attribute/f:name/text()") == ["Inherited"]
assert query("//f:objectClass[f:name='Parent']/f:sharing/text()") == ["PublishSubscribe"]
assert query("//f:objectClass[f:name='Parent']/f:objectClass/f:attribute/f:name/text()") == ["Local"]
assert query("//f:interactionClass[f:name='Event']/f:parameter/f:name/text()") == ["Inherited"]
assert query("//f:interactionClass[f:name='Event']/f:interactionClass/f:parameter/f:name/text()") == ["Local"]
if sys.argv[3] == "merged":
    assert query("//f:enumeratedData[f:name='HLAboolean']/f:enumerator[f:name='HLAtrue']/f:value/text()") == ["1"]
    assert query("//f:objectClass[f:name='HLAfederation']")

# Sensitivity checks ensure the independent schema catches the observed
# vocabulary, order, reference, and required-switch regressions.
for mutation in ("plural-value", "missing-switch", "broken-dimension", "section-order"):
    changed = copy.deepcopy(document)
    if mutation == "plural-value":
        changed.xpath("//f:enumerator/f:value", namespaces=ns)[0].tag = "{" + ns["f"] + "}values"
    elif mutation == "missing-switch":
        element = changed.xpath("/f:objectModel/f:switches/f:autoProvide", namespaces=ns)[0]
        element.getparent().remove(element)
    elif mutation == "broken-dimension":
        changed.xpath("//f:attribute/f:dimensions/f:dimension", namespaces=ns)[0].text = "Unknown"
    else:
        root = changed.getroot()
        root.insert(0, root[-1])
    assert not schema.is_valid(etree.tostring(changed)), mutation + " escaped the oracle"
print("IEEE XSD + independent semantic assertions + 4 schema mutations passed")
