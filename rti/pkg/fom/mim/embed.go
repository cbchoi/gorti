// Package mim embeds the standard MIM and HLAstandardMIM XML modules and
// exposes them as parsed *model.FOM handles for downstream validators.
//
// Provenance: the two XML files in this directory (standard-mim.xml and
// hla-standard-mim.xml) are interim, hand-derived approximations of the
// IEEE-published artifacts (1516.2-2010 Annex B and 1516.1-2010 §4.13). They
// are orchestrator-vendored per docs/ORTHOGONALITY.md §2 (row added
// 2026-05-02); Agent B reads them via //go:embed but never modifies them.
// Each XML file carries its own <!-- PROVENANCE NOTICE --> comment block;
// canonical sourcing is tracked in https://github.com/cbchoi/gorti/issues/1
// and is scheduled for post-M1 work.
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
	"sync"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

//go:embed standard-mim.xml
var standardMIMXML []byte

//go:embed hla-standard-mim.xml
var hlaStandardMIMXML []byte

// StandardMIMBytes returns the embedded standard MIM XML (1516.2-2010 Annex
// B, interim per the package provenance notice) as a fresh byte slice on
// every call. Callers may mutate the returned slice without affecting the
// embedded source.
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
	return model.NewFOM(objectClasses, interactionClasses, dataTypes), nil
}

// The mimXML* types below mirror the subset of IEEE 1516-2010 DIF the
// vendored MIM uses. They are intentionally local: the parser package owns
// its own (richer) walk-tree types and does not export them, so duplication
// is the canonical break-the-cycle move per CODING_CONVENTIONS.md (3
// duplicated lines beats premature shared abstraction).

type mimXMLObjectModel struct {
	XMLName      xml.Name            `xml:"objectModel"`
	Objects      *mimXMLObjects      `xml:"objects"`
	Interactions *mimXMLInteractions `xml:"interactions"`
	DataTypes    *mimXMLDataTypes    `xml:"dataTypes"`
}

type mimXMLObjects struct {
	ObjectClass *mimXMLObjectClass `xml:"objectClass"`
}

type mimXMLObjectClass struct {
	Name       string              `xml:"name"`
	Sharing    string              `xml:"sharing"`
	Attributes []mimXMLAttribute   `xml:"attribute"`
	Children   []mimXMLObjectClass `xml:"objectClass"`
}

type mimXMLAttribute struct {
	Name           string `xml:"name"`
	DataType       string `xml:"dataType"`
	UpdateType     string `xml:"updateType"`
	Ownership      string `xml:"ownership"`
	Sharing        string `xml:"sharing"`
	Transportation string `xml:"transportation"`
	Order          string `xml:"order"`
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
}

type mimXMLParameter struct {
	Name     string `xml:"name"`
	DataType string `xml:"dataType"`
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
	Name           string `xml:"name"`
	Representation string `xml:"representation"`
	Units          string `xml:"units"`
	Resolution     string `xml:"resolution"`
	Accuracy       string `xml:"accuracy"`
}

type mimXMLEnumDataTypes struct {
	EnumeratedData []mimXMLEnumeratedData `xml:"enumeratedData"`
}

type mimXMLEnumeratedData struct {
	Name           string             `xml:"name"`
	Representation string             `xml:"representation"`
	Enumerator     []mimXMLEnumerator `xml:"enumerator"`
}

type mimXMLEnumerator struct {
	Name   string `xml:"name"`
	Values string `xml:"values"`
}

type mimXMLArrayDataTypes struct {
	ArrayData []mimXMLArrayData `xml:"arrayData"`
}

type mimXMLArrayData struct {
	Name        string `xml:"name"`
	DataType    string `xml:"dataType"`
	Cardinality string `xml:"cardinality"`
	Encoding    string `xml:"encoding"`
}

type mimXMLFixedRecTypes struct {
	FixedRecordData []mimXMLFixedRecordData `xml:"fixedRecordData"`
}

type mimXMLFixedRecordData struct {
	Name     string           `xml:"name"`
	Encoding string           `xml:"encoding"`
	Field    []mimXMLRecField `xml:"field"`
}

type mimXMLRecField struct {
	Name     string `xml:"name"`
	DataType string `xml:"dataType"`
}

type mimXMLVariantRecTypes struct {
	VariantRecordData []mimXMLVariantRecordData `xml:"variantRecordData"`
}

type mimXMLVariantRecordData struct {
	Name             string            `xml:"name"`
	Encoding         string            `xml:"encoding"`
	DiscriminantName string            `xml:"discriminant"`
	DataType         string            `xml:"dataType"`
	Alternative      []mimXMLVariantAlt `xml:"alternative"`
}

type mimXMLVariantAlt struct {
	Enumerator string `xml:"enumerator"`
	Name       string `xml:"name"`
	DataType   string `xml:"dataType"`
}

func flattenMIMObjectClasses(node *mimXMLObjectClass, parent string, out *[]model.ObjectClass) {
	if node == nil {
		return
	}
	*out = append(*out, model.ObjectClass{
		Name:       node.Name,
		ParentName: parent,
		Attributes: convertMIMAttributes(node.Attributes),
	})
	for i := range node.Children {
		flattenMIMObjectClasses(&node.Children[i], node.Name, out)
	}
}

func convertMIMAttributes(in []mimXMLAttribute) []model.Attribute {
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

func flattenMIMInteractionClasses(node *mimXMLInteractionClass, parent string, out *[]model.InteractionClass) {
	if node == nil {
		return
	}
	*out = append(*out, model.InteractionClass{
		Name:           node.Name,
		ParentName:     parent,
		Sharing:        node.Sharing,
		Transportation: node.Transportation,
		Order:          node.Order,
		Parameters:     convertMIMParameters(node.Parameters),
	})
	for i := range node.Children {
		flattenMIMInteractionClasses(&node.Children[i], node.Name, out)
	}
}

func convertMIMParameters(in []mimXMLParameter) []model.Parameter {
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
		}
		out = append(out, &model.EnumeratedData{
			NameField:      e.Name,
			Representation: e.Representation,
			Enumerators:    enums,
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
