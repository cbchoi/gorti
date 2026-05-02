package parser

import (
	"encoding/xml"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// xmlObjectModel mirrors the root <objectModel> element of IEEE 1516-2010
// DIF XML (Annex A) for cut-1 parsing. Only sections needed for TASK-001
// are mapped; later tasks add more nodes.
type xmlObjectModel struct {
	XMLName      xml.Name         `xml:"objectModel"`
	Objects      *xmlObjects      `xml:"objects"`
	Interactions *xmlInteractions `xml:"interactions"`
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
