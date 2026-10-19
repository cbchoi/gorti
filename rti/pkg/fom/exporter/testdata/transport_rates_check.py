"""Independent transport/rate vectors using the local IEEE XSD and XPath."""
import copy
import sys
from decimal import Decimal

from lxml import etree
import xmlschema

schema = xmlschema.XMLSchema(sys.argv[1])
document = etree.parse(sys.argv[2])
ns = {"f": "http://standards.ieee.org/IEEE1516-2010"}
transports = "/f:objectModel/f:transportations/f:transportation"
rates = "/f:objectModel/f:updateRates/f:updateRate"


def nodes(doc, path):
    return doc.xpath(path, namespaces=ns)


def check(doc):
    values = nodes(doc, rates + "/f:rate/text()")
    assert len(values) == 3 and all(Decimal(value) > 0 for value in values)
    assert sorted(nodes(doc, transports + "/f:name/text()")) == ["HLAbestEffort", "HLAreliable"]
    assert nodes(doc, transports + "[f:name='HLAreliable']/f:reliable/text()") == ["Yes"]
    assert nodes(doc, transports + "[f:name='HLAbestEffort']/f:reliable/text()") == ["No"]
    assert nodes(doc, transports + "[f:name='HLAreliable']/f:semantics/text()") == ["  Delivery & acknowledgement.  "]
    empty_transport = nodes(doc, transports + "[f:name='HLAbestEffort']/f:semantics")
    assert len(empty_transport) == 1 and not empty_transport[0].text
    assert nodes(doc, rates + "[f:name='Precise']/f:rate/text()") == ["1.0000000000000000000000000001"]
    assert nodes(doc, rates + "[f:name='Empty']/f:rate/text()") == ["0.00000000000000000000000000000000000001"]
    assert nodes(doc, rates + "[f:name='Absent']/f:rate/text()") == ["0.25"]
    assert nodes(doc, rates + "[f:name='Precise']/f:semantics/text()") == ["  exact & explicit  "]
    empty_rate = nodes(doc, rates + "[f:name='Empty']/f:semantics")
    assert len(empty_rate) == 1 and not empty_rate[0].text
    assert not nodes(doc, rates + "[f:name='Absent']/f:semantics")


schema.validate(sys.argv[2])
check(document)

# These corruptions remain schema-valid, so only the independent content oracle
# can detect them. In particular, an unused declaration must not disappear.
for mutation in ("unused-transport", "transport-text", "transport-empty", "reliability",
                 "rate-empty", "rate-absent", "rate-rounding", "0", "-0.000", "-0.25"):
    changed = copy.deepcopy(document)
    if mutation == "unused-transport":
        item = nodes(changed, transports + "[f:name='HLAbestEffort']")[0]
        item.getparent().remove(item)
    elif mutation == "transport-text":
        nodes(changed, transports + "[f:name='HLAreliable']/f:semantics")[0].text = "Delivery & acknowledgement."
    elif mutation == "transport-empty":
        item = nodes(changed, transports + "[f:name='HLAbestEffort']/f:semantics")[0]
        item.getparent().remove(item)
    elif mutation == "reliability":
        nodes(changed, transports + "[f:name='HLAreliable']/f:reliable")[0].text = "No"
    elif mutation == "rate-empty":
        item = nodes(changed, rates + "[f:name='Empty']/f:semantics")[0]
        item.getparent().remove(item)
    elif mutation == "rate-absent":
        etree.SubElement(nodes(changed, rates + "[f:name='Absent']")[0], "{" + ns["f"] + "}semantics")
    elif mutation == "rate-rounding":
        nodes(changed, rates + "[f:name='Precise']/f:rate")[0].text = "1"
    else:
        nodes(changed, rates + "[f:name='Absent']/f:rate")[0].text = mutation
    assert schema.is_valid(etree.tostring(changed)), mutation + " is not a semantic-only mutation"
    try:
        check(changed)
    except AssertionError:
        pass
    else:
        raise AssertionError(mutation + " escaped content checks")

for mutation in ("missing-reference-declaration", "missing-reliability", "invalid-reliability", "element-order"):
    changed = copy.deepcopy(document)
    item = nodes(changed, transports + "[f:name='HLAreliable']")[0]
    if mutation == "missing-reference-declaration":
        item.getparent().remove(item)
    elif mutation == "missing-reliability":
        item.remove(nodes(item, "f:reliable")[0])
    elif mutation == "invalid-reliability":
        nodes(item, "f:reliable")[0].text = "true"
    else:
        item.insert(0, nodes(item, "f:semantics")[0])
    assert not schema.is_valid(etree.tostring(changed)), mutation + " escaped schema checks"

print("IEEE XSD + exact positive declaration vectors + 10 semantic and 4 schema mutations passed")
