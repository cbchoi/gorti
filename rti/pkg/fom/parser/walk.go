package parser

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/cbchoi/gorti/rti/pkg/fom/mim"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// xmlObjectModel mirrors the root <objectModel> element of IEEE 1516-2010
// DIF XML (Annex A) for cut-1 parsing. Only sections needed by the current
// diagnostic passes are mapped; later tasks add more nodes.
type xmlObjectModel struct {
	XMLName         xml.Name             `xml:"objectModel"`
	Objects         *xmlObjects          `xml:"objects"`
	Interactions    *xmlInteractions     `xml:"interactions"`
	DataTypes       *xmlDataTypes        `xml:"dataTypes"`
	Dimensions      *xmlDimensions       `xml:"dimensions"`
	Time            *xmlTime             `xml:"time"`
	UpdateRates     *xmlUpdateRates      `xml:"updateRates"`
	Switches        *xmlAdvisorySwitches `xml:"switches"`
	Transportations *xmlTransportations  `xml:"transportations"`
}

// Time descriptions participate in reference validation, not runtime factory
// selection. The exported FOM model currently has no descriptive metadata API.
type xmlTime struct {
	TimeStamp *xmlTimeRepresentation `xml:"timeStamp"`
	Lookahead *xmlTimeRepresentation `xml:"lookahead"`
}

type xmlTimeRepresentation struct {
	DataType  *string `xml:"dataType"`
	Semantics string  `xml:"semantics"`
}

// xmlDimensions mirrors the <dimensions> block (1516.2-2010 Annex A).
// M10 / FR-DDM-1. In 1516-2010 dimensions are flat (no routing-space
// grouping element); each <dimension> is its own one-axis routing
// space.
type xmlDimensions struct {
	Dimension []xmlDimension `xml:"dimension"`
}

type xmlDimension struct {
	Name          string  `xml:"name"`
	DataType      *string `xml:"dataType"`
	UpperBound    *string `xml:"upperBound"`
	Normalization *string `xml:"normalization"`
	DefaultValue  *string `xml:"value"`
}

type xmlObjects struct {
	ObjectClass *xmlObjectClass `xml:"objectClass"`
}

type xmlObjectClass struct {
	Name       string           `xml:"name"`
	Sharing    string           `xml:"sharing"`
	Semantics  *string          `xml:"semantics"`
	Attributes []xmlAttribute   `xml:"attribute"`
	Children   []xmlObjectClass `xml:"objectClass"`
}

type xmlAttribute struct {
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

type xmlInteractions struct {
	InteractionClass *xmlInteractionClass `xml:"interactionClass"`
}

type xmlInteractionClass struct {
	Name           string                `xml:"name"`
	Sharing        string                `xml:"sharing"`
	Transportation string                `xml:"transportation"`
	Order          string                `xml:"order"`
	Parameters     []xmlParameter        `xml:"parameter"`
	Children       []xmlInteractionClass `xml:"interactionClass"`
	Dimensions     []string              `xml:"dimensions>dimension"`
	Semantics      *string               `xml:"semantics"`
}

type xmlParameter struct {
	Name      string  `xml:"name"`
	DataType  string  `xml:"dataType"`
	Semantics *string `xml:"semantics"`
}

// flattenObjectClasses walks the recursive xmlObjectClass tree and emits a
// flat slice of model.ObjectClass values, populating ParentName for
// non-root nodes. The XML schema permits exactly one root child of
// <objects>; deeper trees recurse.
func flattenObjectClasses(node *xmlObjectClass, parent string, out *[]model.ObjectClass) {
	if node == nil {
		return
	}
	class := model.ClassPath(parent, node.Name)
	attributes := convertAttributes(node.Attributes)
	for i := range attributes {
		attributes[i].Name = mim.CanonicalAttributeName(class, attributes[i].Name)
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
		flattenObjectClasses(&node.Children[i], model.ClassPath(parent, node.Name), out)
	}
}

func convertAttributes(in []xmlAttribute) []model.Attribute {
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
			Dimensions:      convertDimensionReferences(a.Dimensions),
		}
	}
	return out
}

// flattenInteractionClasses walks the recursive xmlInteractionClass tree
// and emits a flat slice with ParentName populated for non-root nodes.
func flattenInteractionClasses(node *xmlInteractionClass, parent string, out *[]model.InteractionClass) {
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
		Parameters:     convertParameters(node.Parameters),
		Dimensions:     convertDimensionReferences(node.Dimensions),
	})
	for i := range node.Children {
		flattenInteractionClasses(&node.Children[i], model.ClassPath(parent, node.Name), out)
	}
}

func convertParameters(in []xmlParameter) []model.Parameter {
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

// xmlDataTypes mirrors <dataTypes> per Annex A. Only the sections needed by
// the current diagnostic passes are parsed; future tasks (encoder M1) add
// more.
type xmlDataTypes struct {
	BasicDataRepresentations *xmlBasicDataReps   `xml:"basicDataRepresentations"`
	SimpleDataTypes          *xmlSimpleDataTypes `xml:"simpleDataTypes"`
	EnumeratedDataTypes      *xmlEnumDataTypes   `xml:"enumeratedDataTypes"`
	ArrayDataTypes           *xmlArrayDataTypes  `xml:"arrayDataTypes"`
	FixedRecordDataTypes     *xmlFixedRecTypes   `xml:"fixedRecordDataTypes"`
	VariantRecordDataTypes   *xmlVariantRecTypes `xml:"variantRecordDataTypes"`
}

type xmlBasicDataReps struct {
	BasicData []xmlBasicData `xml:"basicData"`
}

type xmlBasicData struct {
	Name           string `xml:"name"`
	Size           int    `xml:"size"`
	Endian         string `xml:"endian"`
	Interpretation string `xml:"interpretation"`
	Encoding       string `xml:"encoding"`
}

type xmlSimpleDataTypes struct {
	SimpleData []xmlSimpleData `xml:"simpleData"`
}

type xmlSimpleData struct {
	Name           string  `xml:"name"`
	Representation string  `xml:"representation"`
	Units          string  `xml:"units"`
	Resolution     string  `xml:"resolution"`
	Accuracy       string  `xml:"accuracy"`
	Semantics      *string `xml:"semantics"`
}

type xmlEnumDataTypes struct {
	EnumeratedData []xmlEnumeratedData `xml:"enumeratedData"`
}

type xmlEnumeratedData struct {
	Name           string          `xml:"name"`
	Representation string          `xml:"representation"`
	Semantics      *string         `xml:"semantics"`
	Enumerator     []xmlEnumerator `xml:"enumerator"`
}

type xmlEnumerator struct {
	Name      string   `xml:"name"`
	Values    string   `xml:"values"`
	ValueList []string `xml:"value"`
}

type xmlArrayDataTypes struct {
	ArrayData []xmlArrayData `xml:"arrayData"`
}

type xmlArrayData struct {
	Name        string  `xml:"name"`
	DataType    string  `xml:"dataType"`
	Cardinality string  `xml:"cardinality"`
	Encoding    string  `xml:"encoding"`
	Semantics   *string `xml:"semantics"`
}

type xmlFixedRecTypes struct {
	FixedRecordData []xmlFixedRecordData `xml:"fixedRecordData"`
}

type xmlFixedRecordData struct {
	Name      string        `xml:"name"`
	Encoding  string        `xml:"encoding"`
	Semantics *string       `xml:"semantics"`
	Field     []xmlRecField `xml:"field"`
}

type xmlRecField struct {
	Name      string  `xml:"name"`
	DataType  string  `xml:"dataType"`
	Semantics *string `xml:"semantics"`
}

type xmlVariantRecTypes struct {
	VariantRecordData []xmlVariantRecordData `xml:"variantRecordData"`
}

type xmlVariantRecordData struct {
	Name             string          `xml:"name"`
	Encoding         string          `xml:"encoding"`
	Semantics        *string         `xml:"semantics"`
	DiscriminantName string          `xml:"discriminant"`
	DataType         string          `xml:"dataType"`
	Alternative      []xmlVariantAlt `xml:"alternative"`
}

type xmlVariantAlt struct {
	Enumerator string  `xml:"enumerator"`
	Name       string  `xml:"name"`
	DataType   string  `xml:"dataType"`
	Semantics  *string `xml:"semantics"`
}

// convertDataTypes flattens the <dataTypes> XML section into a slice of
// model.DataType. Returns nil if no data types are declared.
func convertDataTypes(in *xmlDataTypes) []model.DataType {
	if in == nil {
		return nil
	}
	var out []model.DataType
	out = append(out, convertBasicData(in.BasicDataRepresentations)...)
	out = append(out, convertSimpleData(in.SimpleDataTypes)...)
	out = append(out, convertEnumeratedData(in.EnumeratedDataTypes)...)
	out = append(out, convertArrayData(in.ArrayDataTypes)...)
	out = append(out, convertFixedRecordData(in.FixedRecordDataTypes)...)
	out = append(out, convertVariantRecordData(in.VariantRecordDataTypes)...)
	return out
}

func convertBasicData(in *xmlBasicDataReps) []model.DataType {
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

func convertSimpleData(in *xmlSimpleDataTypes) []model.DataType {
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

func convertEnumeratedData(in *xmlEnumDataTypes) []model.DataType {
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

func convertArrayData(in *xmlArrayDataTypes) []model.DataType {
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

func convertFixedRecordData(in *xmlFixedRecTypes) []model.DataType {
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

func convertDimensionReferences(in []string) []string {
	var out []string
	for _, name := range in {
		out = append(out, strings.TrimSpace(name))
	}
	return out
}

// The official FDD 2010 dimensionType makes upperBound optional, but a
// supplied value must be a positive integer representable by our uint64 model.
func convertDimensions(in *xmlDimensions) ([]model.Dimension, []Diagnostic) {
	if in == nil || len(in.Dimension) == 0 {
		return nil, nil
	}
	out := make([]model.Dimension, 0, len(in.Dimension))
	var diags []Diagnostic
	for _, d := range in.Dimension {
		// Existing production modules use NA as an unspecified legacy value.
		// Retain it verbatim; the FDD exporter still rejects this non-XSD value.
		if invalidDimensionMetadata(d) {
			diags = append(diags, Diagnostic{Code: "FOM-015", Message: fmt.Sprintf("dimension %q has invalid dataType, normalization, or value metadata", d.Name)})
			continue
		}
		var upperBound uint64
		if d.UpperBound != nil {
			var err error
			upperBound, err = parseUpperBound(*d.UpperBound)
			if err != nil {
				diags = append(diags, Diagnostic{Code: "FOM-015", Message: fmt.Sprintf("dimension %q has invalid upperBound %q: %v", d.Name, *d.UpperBound, err)})
				continue
			}
		}
		dimension := model.Dimension{
			Name:       strings.TrimSpace(d.Name),
			UpperBound: upperBound,
		}
		if d.DataType != nil {
			dimension.DataType = strings.TrimSpace(*d.DataType)
		}
		if d.Normalization != nil {
			dimension.NormalizationKey = *d.Normalization
		}
		if d.DefaultValue != nil {
			dimension.DefaultValue = *d.DefaultValue
		}
		if _, _, _, err := model.DimensionDefaultRange(dimension); err != nil {
			diags = append(diags, Diagnostic{Code: "FOM-015", Message: fmt.Sprintf("dimension %q has invalid default range: %v", d.Name, err)})
			continue
		}
		out = append(out, dimension)
	}
	return out, diags
}

func parseUpperBound(s string) (uint64, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "+")
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	if v == 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return v, nil
}

func convertVariantRecordData(in *xmlVariantRecTypes) []model.DataType {
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

func invalidDimensionMetadata(d xmlDimension) bool {
	return d.DataType != nil && strings.TrimSpace(*d.DataType) == "" || d.Normalization != nil && *d.Normalization == "" || d.DefaultValue != nil && *d.DefaultValue != "NA" && !model.ValidDimensionDefaultValue(*d.DefaultValue)
}
