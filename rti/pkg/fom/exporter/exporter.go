// Package exporter serializes the retained, merged FOM model as an FDD.
// It does not concatenate source modules or recover metadata discarded upstream.
package exporter

import (
	"encoding/xml"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

const Namespace = "http://standards.ieee.org/IEEE1516-2010"

// ErrUnsupported identifies models that cannot be exported without loss or
// invented metadata. No partial document is returned with this error.
var ErrUnsupported = errors.New("FDD model cannot be exported losslessly")

// Schema vocabulary/order follows the local IEEE1516-FDD-2010.xsd (2010-08-16).
// Reprinted with permission from IEEE 1516.2(TM)-2010.
var switchOrder = []string{
	"autoProvide", "conveyRegionDesignatorSets", "conveyProducingFederate",
	"attributeScopeAdvisory", "attributeRelevanceAdvisory", "objectClassRelevanceAdvisory",
	"interactionRelevanceAdvisory", "serviceReporting", "exceptionReporting", "delaySubscriptionEvaluation",
}

type element struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Text     string     `xml:",chardata"`
	Children []element  `xml:",any"`
}

func node(name string, children ...element) element {
	return element{XMLName: xml.Name{Local: name}, Children: children}
}

func textNode(name, value string) element {
	return element{XMLName: xml.Name{Local: name}, Text: value}
}

func (e *element) optional(name, value string) {
	if value != "" {
		e.Children = append(e.Children, textNode(name, value))
	}
}

func (e *element) descriptive(name string, value model.OptionalString) {
	if value.Present {
		e.Children = append(e.Children, textNode(name, value.Value))
	}
}

func unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, fmt.Sprintf(format, args...))
}

// Marshal exports all retained declarations in deterministic schema order.
// Classes remain hierarchical and only locally declared members are emitted.
// Record field order is preserved because it determines the encoding.
// Required switches and transport/order values must be explicitly modeled;
// unknown switches, unknown transports, ambiguous hierarchies and conflicting
// declarations fail instead of being dropped or assigned invented defaults.
// Metadata absent from model.FOM cannot be reconstructed; see README.md.
func Marshal(f *model.FOM) ([]byte, error) {
	if f == nil {
		return nil, unsupported("nil FOM")
	}
	b := builder{types: map[string]model.DataType{}, dimensions: map[string]bool{}, transports: map[string]bool{}}
	transportations, err := b.transportations(f.Transportations())
	if err != nil {
		return nil, err
	}
	if err := b.indexDataTypes(f.DataTypes()); err != nil {
		return nil, err
	}
	dimensions, err := b.exportDimensions(f.Dimensions())
	if err != nil {
		return nil, err
	}
	objects, err := b.objects(f.ObjectClasses())
	if err != nil {
		return nil, err
	}
	interactions, err := b.interactions(f.InteractionClasses())
	if err != nil {
		return nil, err
	}
	switches, err := exportSwitches(f)
	if err != nil {
		return nil, err
	}
	root := node("objectModel", objects, interactions, dimensions, transportations, switches)
	root.Attrs = []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: Namespace}}
	if len(f.UpdateRates()) != 0 {
		rates, err := exportUpdateRates(f.UpdateRates())
		if err != nil {
			return nil, err
		}
		root.Children = append(root.Children, rates)
	}
	types, err := b.dataTypes()
	if err != nil {
		return nil, err
	}
	if len(types.Children) != 0 {
		root.Children = append(root.Children, types)
	}
	if err := validateXMLValues(root); err != nil {
		return nil, err
	}
	data, err := xml.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, unsupported("XML encoding: %v", err)
	}
	return append(append([]byte(xml.Header), data...), '\n'), nil
}

type builder struct {
	types      map[string]model.DataType
	dimensions map[string]bool
	transports map[string]bool
}

func (b *builder) dataReference(name string) error {
	if name != "" && b.types[name] == nil {
		return unsupported("undefined datatype %q", name)
	}
	return nil
}

func (b *builder) dimensionsNode(names []string) (element, error) {
	e := node("dimensions")
	names = append([]string(nil), names...)
	sort.Strings(names)
	for i, name := range names {
		if !b.dimensions[name] {
			return element{}, unsupported("undefined dimension %q", name)
		}
		if i > 0 && names[i-1] == name {
			return element{}, unsupported("duplicate dimension reference %q", name)
		}
		e.Children = append(e.Children, textNode("dimension", name))
	}
	return e, nil
}

func (b *builder) delivery(e *element, transport, order string) error {
	if transport != "HLAreliable" && transport != "HLAbestEffort" {
		return unsupported("%s has unsupported or absent transportation %q", e.XMLName.Local, transport)
	}
	if order != "Receive" && order != "TimeStamp" {
		return unsupported("%s has unsupported or absent order %q", e.XMLName.Local, order)
	}
	if !b.transports[transport] {
		return unsupported("%s references transportation %q without a retained declaration", e.XMLName.Local, transport)
	}
	e.optional("transportation", transport)
	e.optional("order", order)
	return nil
}

func (b *builder) transportations(values []model.Transportation) (element, error) {
	e := node("transportations")
	for _, value := range values {
		if err := model.ValidateTransportation(value); err != nil {
			return element{}, unsupported("%v", err)
		}
		if value.Name != "HLAreliable" && value.Name != "HLAbestEffort" {
			return element{}, unsupported("unsupported transportation declaration %q", value.Name)
		}
		if b.transports[value.Name] {
			return element{}, unsupported("duplicate transportation %q", value.Name)
		}
		b.transports[value.Name] = true
		d := node("transportation", textNode("name", value.Name), textNode("reliable", value.Reliable))
		d.descriptive("semantics", value.Semantics)
		e.Children = append(e.Children, d)
	}
	return e, nil
}

func exportSwitches(f *model.FOM) (element, error) {
	e := node("switches")
	values := map[string]bool{}
	for _, s := range f.AdvisorySwitches() {
		if _, exists := values[s.Name]; exists {
			return element{}, unsupported("duplicate switch %q", s.Name)
		}
		values[s.Name] = s.Enabled
	}
	for _, name := range switchOrder {
		value, ok := values[name]
		if !ok {
			return element{}, unsupported("required switch %q is not retained", name)
		}
		delete(values, name)
		s := node(name)
		s.Attrs = []xml.Attr{{Name: xml.Name{Local: "isEnabled"}, Value: strconv.FormatBool(value)}}
		e.Children = append(e.Children, s)
	}
	if len(values) != 0 {
		names := make([]string, 0, len(values))
		for name := range values {
			names = append(names, name)
		}
		sort.Strings(names)
		return element{}, unsupported("unknown switch %q", names[0])
	}
	if action := f.AutomaticResignAction(); action != "" {
		switch action {
		case "UnconditionallyDivestAttributes", "DeleteObjects", "CancelPendingOwnershipAcquisitions", "DeleteObjectsThenDivest", "CancelThenDeleteThenDivest", "NoAction":
		default:
			return element{}, unsupported("unknown automatic resign action %q", action)
		}
		s := node("automaticResignAction")
		s.Attrs = []xml.Attr{{Name: xml.Name{Local: "resignAction"}, Value: action}}
		e.Children = append(e.Children, s)
	}
	return e, nil
}

// encoding/xml replaces invalid XML characters with U+FFFD. Reject them first
// so a successful export never changes the model's text silently.
func validateXMLValues(e element) error {
	values := []string{e.Text}
	for _, a := range e.Attrs {
		values = append(values, a.Value)
	}
	for _, value := range values {
		if !utf8.ValidString(value) {
			return unsupported("invalid UTF-8 in %s", e.XMLName.Local)
		}
		for _, r := range value {
			if !validXMLRune(r) {
				return unsupported("invalid XML character in %s", e.XMLName.Local)
			}
		}
	}
	if e.XMLName.Local == "name" || e.XMLName.Local == "discriminant" {
		if !ncName(e.Text) {
			return unsupported("invalid identifier %q", e.Text)
		}
	}
	for _, child := range e.Children {
		if err := validateXMLValues(child); err != nil {
			return err
		}
	}
	return nil
}

func ncName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		start := ncNameStart(r)
		if !start && (i == 0 || !ncNameContinuation(r)) {
			return false
		}
	}
	return true
}

func oneOf(value, choices string) bool {
	if value == "" {
		return true
	}
	for _, choice := range strings.Split(choices, "|") {
		if value == choice {
			return true
		}
	}
	return false
}

func (b *builder) indexDataTypes(types []model.DataType) error {
	for _, d := range types {
		if d == nil || reflect.ValueOf(d).Kind() == reflect.Pointer && reflect.ValueOf(d).IsNil() {
			return unsupported("nil datatype")
		}
		if previous, exists := b.types[d.Name()]; exists && !reflect.DeepEqual(previous, d) {
			return unsupported("conflicting datatype %q", d.Name())
		}
		b.types[d.Name()] = d
	}
	return nil
}

func (b *builder) exportDimensions(values []model.Dimension) (element, error) {
	dimensions := node("dimensions")
	for _, d := range values {
		if b.dimensions[d.Name] {
			return element{}, unsupported("duplicate dimension %q", d.Name)
		}
		b.dimensions[d.Name] = true
		e := node("dimension", textNode("name", d.Name))
		if err := b.dataReference(d.DataType); err != nil {
			return element{}, err
		}
		e.optional("dataType", d.DataType)
		// Zero is the model's sentinel for an absent upperBound, not a bound.
		if d.UpperBound != 0 {
			e.optional("upperBound", strconv.FormatUint(d.UpperBound, 10))
		}
		e.optional("normalization", d.NormalizationKey)
		if d.DefaultValue != "" && !model.ValidDimensionDefaultValue(d.DefaultValue) {
			return element{}, unsupported("invalid dimension value %q on %s", d.DefaultValue, d.Name)
		}
		if _, _, _, err := model.DimensionDefaultRange(d); err != nil {
			return element{}, unsupported("invalid dimension range on %s: %v", d.Name, err)
		}
		e.optional("value", d.DefaultValue)
		dimensions.Children = append(dimensions.Children, e)
	}
	return dimensions, nil
}

func validXMLRune(r rune) bool {
	return r == 9 || r == 10 || r == 13 || runeInRanges(r, [2]rune{0x20, 0xD7FF}, [2]rune{0xE000, 0xFFFD}, [2]rune{0x10000, 0x10FFFF})
}

func runeInRanges(r rune, ranges ...[2]rune) bool {
	for _, interval := range ranges {
		if r >= interval[0] && r <= interval[1] {
			return true
		}
	}
	return false
}

func ncNameStart(r rune) bool {
	return r == '_' || runeInRanges(r,
		[2]rune{'A', 'Z'}, [2]rune{'a', 'z'}, [2]rune{0xC0, 0xD6}, [2]rune{0xD8, 0xF6},
		[2]rune{0xF8, 0x2FF}, [2]rune{0x370, 0x37D}, [2]rune{0x37F, 0x1FFF},
		[2]rune{0x200C, 0x200D}, [2]rune{0x2070, 0x218F}, [2]rune{0x2C00, 0x2FEF},
		[2]rune{0x3001, 0xD7FF}, [2]rune{0xF900, 0xFDCF}, [2]rune{0xFDF0, 0xFFFD},
		[2]rune{0x10000, 0xEFFFF})
}

func ncNameContinuation(r rune) bool {
	return r == '-' || r == '.' || r == 0xB7 || runeInRanges(r, [2]rune{'0', '9'}, [2]rune{0x300, 0x36F}, [2]rune{0x203F, 0x2040})
}

func exportUpdateRates(values []model.UpdateRate) (element, error) {
	rates := node("updateRates")
	seen := map[string]bool{}
	for _, rate := range values {
		if seen[rate.Name] {
			return element{}, unsupported("duplicate update rate %q", rate.Name)
		}
		seen[rate.Name] = true
		decimal, err := rate.DecimalString()
		if err != nil {
			return element{}, unsupported("%v", err)
		}
		r := node("updateRate", textNode("name", rate.Name), textNode("rate", decimal))
		r.descriptive("semantics", rate.SemanticsDeclaration())
		rates.Children = append(rates.Children, r)
	}
	return rates, nil
}
