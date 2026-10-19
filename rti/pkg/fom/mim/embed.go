// Package mim embeds the standard MIM and HLAstandardMIM XML modules and
// exposes them as parsed *model.FOM handles for downstream validators.
//
// Provenance:
//
//   - standard-mim.xml: IEEE 1516.1-2010 download archive member
//     1516.1-2010_downloads/HLAstandardMIM.xml, verified 2026-10-04.
//     Source: standards.ieee.org/wp-content/uploads/import/download/1516.1-2010_downloads.zip
//     SHA256: 839f092b4b3e02a1363ec4463efcb1aae7fbfdfb17778b159065f277a3f2b023.
//     Its IEEE attribution and exact source bytes are preserved. The parsed
//     model applies Clause 11 table precedence; see CanonicalAttributeName.
//
//   - hla-standard-mim.xml — empty wrapper module (the standard MIM is
//     self-sufficient for cut-1; this file exists so the //go:embed
//     pair stays uniform with downstream MIM-merge logic).
//
// Both files are vendored according to the repository dependency policy (row
// added 2026-05-02); this package reads them via //go:embed but never modifies
// them. Each XML file carries its own provenance comment block.
//
// Determinism: this package is pure data. StandardMIMBytes and
// HLAStandardMIMBytes return defensive copies of the embedded byte slices
// so no caller can mutate the singleton view across goroutines.
// StandardMIMHandle memoizes the parsed model so the lookup is O(1) after
// first call; the underlying FOM is immutable by construction (see
// rti/pkg/fom/model — exported fields, no setters).
//
// Note: this package decodes the embedded XML through encoding/xml directly
// rather than calling parser.Parse, so that the parser package can in turn
// import mim to wire FOM-101 detection without an import cycle.
package mim

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"fmt"
	"strings"
	"sync"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

//go:embed standard-mim.xml
var standardMIMXML []byte

//go:embed hla-standard-mim.xml
var hlaStandardMIMXML []byte

// StandardMIMBytes returns the unmodified IEEE 1516.1-2010 Annex G source
// as a fresh byte slice. The effective model applies Clause 11 precedence;
// source-reporting services still return these original bytes.
func StandardMIMBytes() []byte {
	return append([]byte(nil), standardMIMXML...)
}

// HLAStandardMIMBytes returns the embedded HLAstandardMIM XML
// (1516.1-2010 §4.13, interim) as a fresh byte slice on every call.
func HLAStandardMIMBytes() []byte {
	return append([]byte(nil), hlaStandardMIMXML...)
}

// standardMIM holds the lazily parsed standard MIM. Populated once via
// standardMIMOnce; subsequent reads observe the same *model.FOM pointer.
var (
	standardMIMOnce sync.Once
	standardMIMFOM  *model.FOM
	standardMIMErr  error
)

// StandardMIMHandle parses the embedded standard MIM XML once and returns
// the resulting *model.FOM. Subsequent calls return the same pointer.
//
// Returns a non-nil error only if the embedded XML cannot be decoded; in
// normal operation that would indicate a corrupted vendored artifact.
func StandardMIMHandle() (*model.FOM, error) {
	standardMIMOnce.Do(func() {
		fm, err := decodeMIM(standardMIMXML)
		if err != nil {
			standardMIMErr = fmt.Errorf("mim: decode embedded standard MIM: %w", err)
			return
		}
		standardMIMFOM = fm
	})
	return standardMIMFOM, standardMIMErr
}

// decodeMIM parses the embedded MIM XML into a *model.FOM directly via
// encoding/xml without depending on rti/pkg/fom/parser. The set of element
// names mirrors the vendored XML schema used by the parser's structural
// walk — the two structs cannot share a type (parser's are unexported and
// owned by the parser package) but they decode the same DIF subset.
func decodeMIM(b []byte) (*model.FOM, error) {
	var raw mimXMLObjectModel
	dec := xml.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	var (
		objectClasses      []model.ObjectClass
		interactionClasses []model.InteractionClass
	)
	if raw.Objects != nil {
		flattenMIMObjectClasses(raw.Objects.ObjectClass, "", &objectClasses)
	}
	if raw.Interactions != nil {
		flattenMIMInteractionClasses(raw.Interactions.InteractionClass, "", &interactionClasses)
	}
	dataTypes := convertMIMDataTypes(raw.DataTypes)
	dimensions, err := decodeMIMDimensions(raw.Dimensions)
	if err != nil {
		return nil, err
	}
	values, action, err := decodeMIMSwitches(raw.Switches)
	if err != nil {
		return nil, err
	}
	var transportations []model.Transportation
	for _, declaration := range raw.Transportations {
		transportation := model.Transportation{Name: strings.TrimSpace(declaration.Name), Reliable: declaration.Reliable, Semantics: model.OptionalStringFrom(declaration.Semantics)}
		if err := model.ValidateTransportation(transportation); err != nil {
			return nil, err
		}
		transportations = append(transportations, transportation)
	}
	transportations, err = model.MergeTransportations(nil, transportations)
	if err != nil {
		return nil, err
	}
	var rates []model.UpdateRate
	for _, declaration := range raw.UpdateRates {
		semantics := model.OptionalStringFrom(declaration.Semantics)
		rate, err := model.NewUpdateRate(declaration.Name, declaration.Rate, semantics.Value)
		if err != nil {
			return nil, err
		}
		rate.SemanticsPresent = semantics.Present
		rates = append(rates, rate)
	}
	rates, err = model.MergeUpdateRates(nil, rates)
	if err != nil {
		return nil, err
	}
	return model.NewFOMWithUpdateRates(objectClasses, interactionClasses, dataTypes, dimensions, rates).WithTransportations(transportations).WithAdvisorySwitches(values).WithAutomaticResignAction(action), nil
}

// The mimXML* types below mirror the subset of IEEE 1516-2010 DIF the
// vendored MIM uses. They are intentionally local: the parser package owns
// its own (richer) walk-tree types and does not export them, so duplication
// is the canonical break-the-cycle move per CODING_CONVENTIONS.md (3
// duplicated lines beats premature shared abstraction).

type mimXMLObjectModel struct {
	XMLName         xml.Name               `xml:"objectModel"`
	Objects         *mimXMLObjects         `xml:"objects"`
	Interactions    *mimXMLInteractions    `xml:"interactions"`
	DataTypes       *mimXMLDataTypes       `xml:"dataTypes"`
	Dimensions      []mimXMLDimension      `xml:"dimensions>dimension"`
	Switches        *mimXMLSwitches        `xml:"switches"`
	Transportations []mimXMLTransportation `xml:"transportations>transportation"`
	UpdateRates     []mimXMLUpdateRate     `xml:"updateRates>updateRate"`
}

type mimXMLTransportation struct {
	Name      string  `xml:"name"`
	Reliable  string  `xml:"reliable"`
	Semantics *string `xml:"semantics"`
}

type mimXMLUpdateRate struct {
	Name      string  `xml:"name"`
	Rate      string  `xml:"rate"`
	Semantics *string `xml:"semantics"`
}

type mimXMLSwitches struct {
	Values []mimXMLSwitch `xml:",any"`
}

type mimXMLSwitch struct {
	XMLName  xml.Name
	Enabled  *string    `xml:"isEnabled,attr"`
	Action   *string    `xml:"resignAction,attr"`
	Content  string     `xml:",chardata"`
	Children []struct{} `xml:",any"`
}

func decodeMIMSwitches(raw *mimXMLSwitches) ([]model.AdvisorySwitch, string, error) {
	if raw == nil {
		return nil, "", nil
	}
	var result []model.AdvisorySwitch
	var action string
	seen := map[string]bool{}
	for _, s := range raw.Values {
		name := s.XMLName.Local
		if seen[name] || strings.TrimSpace(s.Content) != "" || len(s.Children) != 0 {
			return nil, "", fmt.Errorf("invalid MIM switch %q", name)
		}
		seen[name] = true
		if name == "automaticResignAction" {
			action = "NoAction"
			if s.Action != nil {
				action = strings.TrimSpace(*s.Action)
			}
			switch action {
			case "UnconditionallyDivestAttributes", "DeleteObjects", "CancelPendingOwnershipAcquisitions", "DeleteObjectsThenDivest", "CancelThenDeleteThenDivest", "NoAction":
			default:
				return nil, "", fmt.Errorf("invalid MIM resign action %q", action)
			}
			continue
		}
		switch name {
		case "autoProvide", "conveyRegionDesignatorSets", "conveyProducingFederate", "attributeScopeAdvisory", "attributeRelevanceAdvisory", "objectClassRelevanceAdvisory", "interactionRelevanceAdvisory", "serviceReporting", "exceptionReporting", "delaySubscriptionEvaluation":
		default:
			return nil, "", fmt.Errorf("unknown MIM switch %q", name)
		}
		enabled := false
		if s.Enabled != nil {
			switch strings.TrimSpace(*s.Enabled) {
			case "true", "1":
				enabled = true
			case "false", "0":
			default:
				return nil, "", fmt.Errorf("invalid MIM boolean switch %q", name)
			}
		}
		result = append(result, model.AdvisorySwitch{Name: name, Enabled: enabled})
	}
	return result, action, nil
}

type mimXMLDimension struct {
	Name          string  `xml:"name"`
	DataType      *string `xml:"dataType"`
	UpperBound    *uint64 `xml:"upperBound"`
	Normalization *string `xml:"normalization"`
	DefaultValue  *string `xml:"value"`
}

type mimXMLObjects struct {
	ObjectClass *mimXMLObjectClass `xml:"objectClass"`
}

type mimXMLObjectClass struct {
	Name       string              `xml:"name"`
	Sharing    string              `xml:"sharing"`
	Semantics  *string             `xml:"semantics"`
	Attributes []mimXMLAttribute   `xml:"attribute"`
	Children   []mimXMLObjectClass `xml:"objectClass"`
}

type mimXMLAttribute struct {
	Name            string   `xml:"name"`
	DataType        string   `xml:"dataType"`
	UpdateType      string   `xml:"updateType"`
	UpdateCondition *string  `xml:"updateCondition"`
	Ownership       string   `xml:"ownership"`
	Sharing         string   `xml:"sharing"`
	Transportation  string   `xml:"transportation"`
	Order           string   `xml:"order"`
	Dimensions      []string `xml:"dimensions>dimension"`
	Semantics       *string  `xml:"semantics"`
}

type mimXMLInteractions struct {
	InteractionClass *mimXMLInteractionClass `xml:"interactionClass"`
}

type mimXMLInteractionClass struct {
	Name           string                   `xml:"name"`
	Sharing        string                   `xml:"sharing"`
	Transportation string                   `xml:"transportation"`
	Order          string                   `xml:"order"`
	Parameters     []mimXMLParameter        `xml:"parameter"`
	Children       []mimXMLInteractionClass `xml:"interactionClass"`
	Dimensions     []string                 `xml:"dimensions>dimension"`
	Semantics      *string                  `xml:"semantics"`
}

type mimXMLParameter struct {
	Name      string  `xml:"name"`
	DataType  string  `xml:"dataType"`
	Semantics *string `xml:"semantics"`
}

type mimXMLDataTypes struct {
	BasicDataRepresentations *mimXMLBasicDataReps   `xml:"basicDataRepresentations"`
	SimpleDataTypes          *mimXMLSimpleDataTypes `xml:"simpleDataTypes"`
	EnumeratedDataTypes      *mimXMLEnumDataTypes   `xml:"enumeratedDataTypes"`
	ArrayDataTypes           *mimXMLArrayDataTypes  `xml:"arrayDataTypes"`
	FixedRecordDataTypes     *mimXMLFixedRecTypes   `xml:"fixedRecordDataTypes"`
	VariantRecordDataTypes   *mimXMLVariantRecTypes `xml:"variantRecordDataTypes"`
}

type mimXMLBasicDataReps struct {
	BasicData []mimXMLBasicData `xml:"basicData"`
}

type mimXMLBasicData struct {
	Name           string `xml:"name"`
	Size           int    `xml:"size"`
	Endian         string `xml:"endian"`
	Interpretation string `xml:"interpretation"`
	Encoding       string `xml:"encoding"`
}

type mimXMLSimpleDataTypes struct {
	SimpleData []mimXMLSimpleData `xml:"simpleData"`
}

type mimXMLSimpleData struct {
	Name           string  `xml:"name"`
	Representation string  `xml:"representation"`
	Units          string  `xml:"units"`
	Resolution     string  `xml:"resolution"`
	Accuracy       string  `xml:"accuracy"`
	Semantics      *string `xml:"semantics"`
}

type mimXMLEnumDataTypes struct {
	EnumeratedData []mimXMLEnumeratedData `xml:"enumeratedData"`
}

type mimXMLEnumeratedData struct {
	Name           string             `xml:"name"`
	Representation string             `xml:"representation"`
	Semantics      *string            `xml:"semantics"`
	Enumerator     []mimXMLEnumerator `xml:"enumerator"`
}

type mimXMLEnumerator struct {
	Name      string   `xml:"name"`
	Values    string   `xml:"values"`
	ValueList []string `xml:"value"`
}

type mimXMLArrayDataTypes struct {
	ArrayData []mimXMLArrayData `xml:"arrayData"`
}

type mimXMLArrayData struct {
	Name        string  `xml:"name"`
	DataType    string  `xml:"dataType"`
	Cardinality string  `xml:"cardinality"`
	Encoding    string  `xml:"encoding"`
	Semantics   *string `xml:"semantics"`
}

type mimXMLFixedRecTypes struct {
	FixedRecordData []mimXMLFixedRecordData `xml:"fixedRecordData"`
}

type mimXMLFixedRecordData struct {
	Name      string           `xml:"name"`
	Encoding  string           `xml:"encoding"`
	Semantics *string          `xml:"semantics"`
	Field     []mimXMLRecField `xml:"field"`
}

type mimXMLRecField struct {
	Name      string  `xml:"name"`
	DataType  string  `xml:"dataType"`
	Semantics *string `xml:"semantics"`
}

type mimXMLVariantRecTypes struct {
	VariantRecordData []mimXMLVariantRecordData `xml:"variantRecordData"`
}

type mimXMLVariantRecordData struct {
	Name             string             `xml:"name"`
	Encoding         string             `xml:"encoding"`
	Semantics        *string            `xml:"semantics"`
	DiscriminantName string             `xml:"discriminant"`
	DataType         string             `xml:"dataType"`
	Alternative      []mimXMLVariantAlt `xml:"alternative"`
}

type mimXMLVariantAlt struct {
	Enumerator string  `xml:"enumerator"`
	Name       string  `xml:"name"`
	DataType   string  `xml:"dataType"`
	Semantics  *string `xml:"semantics"`
}

func flattenMIMObjectClasses(node *mimXMLObjectClass, parent string, out *[]model.ObjectClass) {
	if node == nil {
		return
	}
	class := model.ClassPath(parent, node.Name)
	attributes := convertMIMAttributes(node.Attributes)
	for i := range attributes {
		attributes[i].Name = CanonicalAttributeName(class, attributes[i].Name)
	}
	*out = append(*out, model.ObjectClass{
		Name:          node.Name,
		ParentName:    model.LocalClassName(parent),
		QualifiedName: model.ClassPath(parent, node.Name),
		Sharing:       node.Sharing,
		Semantics:     model.OptionalStringFrom(node.Semantics),
		Attributes:    attributes,
	})
	for i := range node.Children {
		flattenMIMObjectClasses(&node.Children[i], model.ClassPath(parent, node.Name), out)
	}
}

func convertMIMAttributes(in []mimXMLAttribute) []model.Attribute {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.Attribute, len(in))
	for i, a := range in {
		out[i] = model.Attribute{
			Name:            a.Name,
			DataType:        a.DataType,
			UpdateType:      a.UpdateType,
			UpdateCondition: model.OptionalStringFrom(a.UpdateCondition),
			Semantics:       model.OptionalStringFrom(a.Semantics),
			Ownership:       a.Ownership,
			Sharing:         a.Sharing,
			Transportation:  a.Transportation,
			Order:           a.Order,
			Dimensions:      convertMIMDimensionReferences(a.Dimensions),
		}
	}
	return out
}

func flattenMIMInteractionClasses(node *mimXMLInteractionClass, parent string, out *[]model.InteractionClass) {
	if node == nil {
		return
	}
	*out = append(*out, model.InteractionClass{
		Name:           node.Name,
		ParentName:     model.LocalClassName(parent),
		QualifiedName:  model.ClassPath(parent, node.Name),
		Sharing:        node.Sharing,
		Semantics:      model.OptionalStringFrom(node.Semantics),
		Transportation: node.Transportation,
		Order:          node.Order,
		Parameters:     convertMIMParameters(node.Parameters),
		Dimensions:     convertMIMDimensionReferences(node.Dimensions),
	})
	for i := range node.Children {
		flattenMIMInteractionClasses(&node.Children[i], model.ClassPath(parent, node.Name), out)
	}
}

func convertMIMParameters(in []mimXMLParameter) []model.Parameter {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.Parameter, len(in))
	for i, p := range in {
		out[i] = model.Parameter{
			Name:      p.Name,
			DataType:  p.DataType,
			Semantics: model.OptionalStringFrom(p.Semantics),
		}
	}
	return out
}

func convertMIMDimensionReferences(in []string) []string {
	var out []string
	for _, name := range in {
		out = append(out, strings.TrimSpace(name))
	}
	return out
}

func convertMIMDataTypes(in *mimXMLDataTypes) []model.DataType {
	if in == nil {
		return nil
	}
	var out []model.DataType
	out = append(out, convertMIMBasicData(in.BasicDataRepresentations)...)
	out = append(out, convertMIMSimpleData(in.SimpleDataTypes)...)
	out = append(out, convertMIMEnumeratedData(in.EnumeratedDataTypes)...)
	out = append(out, convertMIMArrayData(in.ArrayDataTypes)...)
	out = append(out, convertMIMFixedRecordData(in.FixedRecordDataTypes)...)
	out = append(out, convertMIMVariantRecordData(in.VariantRecordDataTypes)...)
	return out
}

func convertMIMBasicData(in *mimXMLBasicDataReps) []model.DataType {
	if in == nil {
		return nil
	}
	out := make([]model.DataType, 0, len(in.BasicData))
	for _, b := range in.BasicData {
		out = append(out, &model.BasicData{
			NameField:      b.Name,
			Size:           b.Size,
			Endian:         b.Endian,
			Interpretation: b.Interpretation,
			Encoding:       b.Encoding,
		})
	}
	return out
}

func convertMIMSimpleData(in *mimXMLSimpleDataTypes) []model.DataType {
	if in == nil {
		return nil
	}
	out := make([]model.DataType, 0, len(in.SimpleData))
	for _, s := range in.SimpleData {
		out = append(out, &model.SimpleData{
			NameField:      s.Name,
			Representation: s.Representation,
			Units:          s.Units,
			Resolution:     s.Resolution,
			Accuracy:       s.Accuracy,
			Semantics:      model.OptionalStringFrom(s.Semantics),
		})
	}
	return out
}

func convertMIMEnumeratedData(in *mimXMLEnumDataTypes) []model.DataType {
	if in == nil {
		return nil
	}
	out := make([]model.DataType, 0, len(in.EnumeratedData))
	for _, e := range in.EnumeratedData {
		enums := make([]model.Enumerator, len(e.Enumerator))
		for i, en := range e.Enumerator {
			enums[i] = model.Enumerator{Name: en.Name, Values: en.Values}
			if len(en.ValueList) > 0 {
				enums[i].ValueList = append([]string(nil), en.ValueList...)
				if enums[i].Values == "" {
					enums[i].Values = en.ValueList[0]
				}
			}
		}
		out = append(out, &model.EnumeratedData{
			NameField:      e.Name,
			Representation: e.Representation,
			Enumerators:    enums,
			Semantics:      model.OptionalStringFrom(e.Semantics),
		})
	}
	return out
}

func convertMIMArrayData(in *mimXMLArrayDataTypes) []model.DataType {
	if in == nil {
		return nil
	}
	out := make([]model.DataType, 0, len(in.ArrayData))
	for _, a := range in.ArrayData {
		out = append(out, &model.ArrayData{
			NameField:   a.Name,
			DataType:    a.DataType,
			Cardinality: a.Cardinality,
			Semantics:   model.OptionalStringFrom(a.Semantics),
			Encoding:    a.Encoding,
		})
	}
	return out
}

func convertMIMFixedRecordData(in *mimXMLFixedRecTypes) []model.DataType {
	if in == nil {
		return nil
	}
	out := make([]model.DataType, 0, len(in.FixedRecordData))
	for _, f := range in.FixedRecordData {
		fields := make([]model.RecordField, len(f.Field))
		for i, ff := range f.Field {
			fields[i] = model.RecordField{Name: ff.Name, DataType: ff.DataType, Semantics: model.OptionalStringFrom(ff.Semantics)}
		}
		out = append(out, &model.FixedRecordData{
			NameField: f.Name,
			Fields:    fields,
			Semantics: model.OptionalStringFrom(f.Semantics),
			Encoding:  f.Encoding,
		})
	}
	return out
}

func convertMIMVariantRecordData(in *mimXMLVariantRecTypes) []model.DataType {
	if in == nil {
		return nil
	}
	out := make([]model.DataType, 0, len(in.VariantRecordData))
	for _, v := range in.VariantRecordData {
		alts := make([]model.VariantAlternative, len(v.Alternative))
		for i, a := range v.Alternative {
			alts[i] = model.VariantAlternative{
				Enumerator: a.Enumerator,
				Name:       a.Name,
				DataType:   a.DataType,
				Semantics:  model.OptionalStringFrom(a.Semantics),
			}
		}
		out = append(out, &model.VariantRecordData{
			NameField:        v.Name,
			DiscriminantName: v.DiscriminantName,
			DiscriminantType: v.DataType,
			Alternatives:     alts,
			Semantics:        model.OptionalStringFrom(v.Semantics),
			Encoding:         v.Encoding,
		})
	}
	return out
}

func decodeMIMDimensions(values []mimXMLDimension) ([]model.Dimension, error) {
	var dimensions []model.Dimension
	for _, d := range values {
		// Match the parser's legacy NA retention without widening FDD export.
		if d.DataType != nil && strings.TrimSpace(*d.DataType) == "" || d.Normalization != nil && *d.Normalization == "" || d.DefaultValue != nil && *d.DefaultValue != "NA" && !model.ValidDimensionDefaultValue(*d.DefaultValue) {
			return nil, fmt.Errorf("dimension %q has invalid dataType, normalization, or value metadata", d.Name)
		}
		var upperBound uint64
		if d.UpperBound != nil {
			upperBound = *d.UpperBound
			if upperBound == 0 {
				return nil, fmt.Errorf("dimension %q upperBound must be positive", d.Name)
			}
		}
		dimension := model.Dimension{Name: strings.TrimSpace(d.Name), UpperBound: upperBound}
		if d.DataType != nil {
			dimension.DataType = strings.TrimSpace(*d.DataType)
		}
		if d.Normalization != nil {
			dimension.NormalizationKey = *d.Normalization
		}
		if d.DefaultValue != nil {
			dimension.DefaultValue = *d.DefaultValue
		}
		dimensions = append(dimensions, dimension)
	}
	return dimensions, nil
}
