package parser

import (
	"encoding/xml"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// xmlObjectModel mirrors the root <objectModel> element of IEEE 1516-2010
// DIF XML (Annex A) for cut-1 parsing. Only sections needed by the current
// diagnostic passes are mapped; later tasks add more nodes.
type xmlObjectModel struct {
	XMLName      xml.Name         `xml:"objectModel"`
	Objects      *xmlObjects      `xml:"objects"`
	Interactions *xmlInteractions `xml:"interactions"`
	DataTypes    *xmlDataTypes    `xml:"dataTypes"`
}

type xmlObjects struct {
	ObjectClass *xmlObjectClass `xml:"objectClass"`
}

type xmlObjectClass struct {
	Name       string           `xml:"name"`
	Sharing    string           `xml:"sharing"`
	Attributes []xmlAttribute   `xml:"attribute"`
	Children   []xmlObjectClass `xml:"objectClass"`
}

type xmlAttribute struct {
	Name           string `xml:"name"`
	DataType       string `xml:"dataType"`
	UpdateType     string `xml:"updateType"`
	Ownership      string `xml:"ownership"`
	Sharing        string `xml:"sharing"`
	Transportation string `xml:"transportation"`
	Order          string `xml:"order"`
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
}

type xmlParameter struct {
	Name     string `xml:"name"`
	DataType string `xml:"dataType"`
}

// flattenObjectClasses walks the recursive xmlObjectClass tree and emits a
// flat slice of model.ObjectClass values, populating ParentName for
// non-root nodes. The XML schema permits exactly one root child of
// <objects>; deeper trees recurse.
func flattenObjectClasses(node *xmlObjectClass, parent string, out *[]model.ObjectClass) {
	if node == nil {
		return
	}
	*out = append(*out, model.ObjectClass{
		Name:       node.Name,
		ParentName: parent,
		Attributes: convertAttributes(node.Attributes),
	})
	for i := range node.Children {
		flattenObjectClasses(&node.Children[i], node.Name, out)
	}
}

func convertAttributes(in []xmlAttribute) []model.Attribute {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.Attribute, len(in))
	for i, a := range in {
		out[i] = model.Attribute{
			Name:           a.Name,
			DataType:       a.DataType,
			UpdateType:     a.UpdateType,
			Ownership:      a.Ownership,
			Sharing:        a.Sharing,
			Transportation: a.Transportation,
			Order:          a.Order,
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
		ParentName:     parent,
		Sharing:        node.Sharing,
		Transportation: node.Transportation,
		Order:          node.Order,
		Parameters:     convertParameters(node.Parameters),
	})
	for i := range node.Children {
		flattenInteractionClasses(&node.Children[i], node.Name, out)
	}
}

func convertParameters(in []xmlParameter) []model.Parameter {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.Parameter, len(in))
	for i, p := range in {
		out[i] = model.Parameter{
			Name:     p.Name,
			DataType: p.DataType,
		}
	}
	return out
}

// xmlDataTypes mirrors <dataTypes> per Annex A. Only the sections needed by
// the current diagnostic passes are parsed; future tasks (encoder M1) add
// more.
type xmlDataTypes struct {
	BasicDataRepresentations *xmlBasicDataReps    `xml:"basicDataRepresentations"`
	SimpleDataTypes          *xmlSimpleDataTypes  `xml:"simpleDataTypes"`
	EnumeratedDataTypes      *xmlEnumDataTypes    `xml:"enumeratedDataTypes"`
	ArrayDataTypes           *xmlArrayDataTypes   `xml:"arrayDataTypes"`
	FixedRecordDataTypes     *xmlFixedRecTypes    `xml:"fixedRecordDataTypes"`
	VariantRecordDataTypes   *xmlVariantRecTypes  `xml:"variantRecordDataTypes"`
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
	Name           string `xml:"name"`
	Representation string `xml:"representation"`
	Units          string `xml:"units"`
	Resolution     string `xml:"resolution"`
	Accuracy       string `xml:"accuracy"`
}

type xmlEnumDataTypes struct {
	EnumeratedData []xmlEnumeratedData `xml:"enumeratedData"`
}

type xmlEnumeratedData struct {
	Name           string         `xml:"name"`
	Representation string         `xml:"representation"`
	Enumerator     []xmlEnumerator `xml:"enumerator"`
}

type xmlEnumerator struct {
	Name   string `xml:"name"`
	Values string `xml:"values"`
}

type xmlArrayDataTypes struct {
	ArrayData []xmlArrayData `xml:"arrayData"`
}

type xmlArrayData struct {
	Name        string `xml:"name"`
	DataType    string `xml:"dataType"`
	Cardinality string `xml:"cardinality"`
	Encoding    string `xml:"encoding"`
}

type xmlFixedRecTypes struct {
	FixedRecordData []xmlFixedRecordData `xml:"fixedRecordData"`
}

type xmlFixedRecordData struct {
	Name     string         `xml:"name"`
	Encoding string         `xml:"encoding"`
	Field    []xmlRecField  `xml:"field"`
}

type xmlRecField struct {
	Name     string `xml:"name"`
	DataType string `xml:"dataType"`
}

type xmlVariantRecTypes struct {
	VariantRecordData []xmlVariantRecordData `xml:"variantRecordData"`
}

type xmlVariantRecordData struct {
	Name             string             `xml:"name"`
	Encoding         string             `xml:"encoding"`
	DiscriminantName string             `xml:"discriminant"`
	DataType         string             `xml:"dataType"`
	Alternative      []xmlVariantAlt    `xml:"alternative"`
}

type xmlVariantAlt struct {
	Enumerator string `xml:"enumerator"`
	Name       string `xml:"name"`
	DataType   string `xml:"dataType"`
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
		}
		out = append(out, &model.EnumeratedData{
			NameField:      e.Name,
			Representation: e.Representation,
			Enumerators:    enums,
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
			fields[i] = model.RecordField{Name: ff.Name, DataType: ff.DataType}
		}
		out = append(out, &model.FixedRecordData{
			NameField: f.Name,
			Fields:    fields,
			Encoding:  f.Encoding,
		})
	}
	return out
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
			}
		}
		out = append(out, &model.VariantRecordData{
			NameField:        v.Name,
			DiscriminantName: v.DiscriminantName,
			DiscriminantType: v.DataType,
			Alternatives:     alts,
			Encoding:         v.Encoding,
		})
	}
	return out
}
