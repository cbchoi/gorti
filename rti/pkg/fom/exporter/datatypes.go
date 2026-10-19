package exporter

import (
	"regexp"
	"sort"
	"strconv"

	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

var cardinality = regexp.MustCompile(`^(Dynamic|[0-9]+|\[[0-9]+\.\.[0-9]+\])(,(Dynamic|[0-9]+|\[[0-9]+\.\.[0-9]+\]))*$`)

func (b *builder) dataTypes() (element, error) {
	groups := []element{node("basicDataRepresentations"), node("simpleDataTypes"), node("enumeratedDataTypes"), node("arrayDataTypes"), node("fixedRecordDataTypes"), node("variantRecordDataTypes")}
	names := make([]string, 0, len(b.types))
	for name := range b.types {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := b.types[name]
		var e element
		var group int
		var refs []string
		var err error
		switch d := data.(type) {
		case *model.BasicData:
			group = 0
			e, refs, err = exportBasicData(name, d)
		case *model.SimpleData:
			group = 1
			e, refs, err = exportSimpleData(name, d)
		case *model.EnumeratedData:
			group = 2
			e, refs, err = exportEnumeratedData(name, d)
		case *model.ArrayData:
			group = 3
			e, refs, err = exportArrayData(name, d)
		case *model.FixedRecordData:
			group = 4
			e, refs, err = exportFixedRecordData(name, d)
		case *model.VariantRecordData:
			group = 5
			e, refs, err = exportVariantRecordData(name, d)
		default:
			return element{}, unsupported("unknown datatype implementation %T", data)
		}
		if err != nil {
			return element{}, err
		}
		for _, ref := range refs {
			if err := b.dataReference(ref); err != nil {
				return element{}, err
			}
		}
		groups[group].Children = append(groups[group].Children, e)
	}
	result := node("dataTypes")
	for _, group := range groups {
		if len(group.Children) != 0 {
			result.Children = append(result.Children, group)
		}
	}
	return result, nil
}

func exportBasicData(name string, d *model.BasicData) (element, []string, error) {
	var refs []string
	e := node("basicData", textNode("name", name))
	if !oneOf(d.Endian, "Big|Little") {
		return element{}, nil, unsupported("invalid endian %q on %s", d.Endian, name)
	}
	if d.Size != 0 {
		e.optional("size", strconv.Itoa(d.Size))
	}
	e.optional("interpretation", d.Interpretation)
	e.optional("endian", d.Endian)
	e.optional("encoding", d.Encoding)
	return e, refs, nil
}

func exportSimpleData(name string, d *model.SimpleData) (element, []string, error) {
	var refs []string
	e := node("simpleData", textNode("name", name))
	refs = append(refs, d.Representation)
	e.optional("representation", d.Representation)
	e.optional("units", d.Units)
	e.optional("resolution", d.Resolution)
	e.optional("accuracy", d.Accuracy)
	e.descriptive("semantics", d.Semantics)
	return e, refs, nil
}

func exportEnumeratedData(name string, d *model.EnumeratedData) (element, []string, error) {
	var refs []string
	e := node("enumeratedData", textNode("name", name))
	refs = append(refs, d.Representation)
	e.optional("representation", d.Representation)
	e.descriptive("semantics", d.Semantics)
	values := append([]model.Enumerator(nil), d.Enumerators...)
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	for i, value := range values {
		if i > 0 && values[i-1].Name == value.Name {
			return element{}, nil, unsupported("duplicate enumerator %s.%s", name, value.Name)
		}
		item := node("enumerator", textNode("name", value.Name))
		if len(value.ValueList) != 0 {
			if value.Values != "" && value.Values != value.ValueList[0] {
				return element{}, nil, unsupported("conflicting enumerator values %s.%s", name, value.Name)
			}
			for _, v := range value.ValueList {
				item.Children = append(item.Children, textNode("value", v))
			}
		} else {
			item.optional("value", value.Values)
		}
		e.Children = append(e.Children, item)
	}
	return e, refs, nil
}

func exportArrayData(name string, d *model.ArrayData) (element, []string, error) {
	var refs []string
	e := node("arrayData", textNode("name", name))
	if d.Cardinality != "" && !cardinality.MatchString(d.Cardinality) {
		return element{}, nil, unsupported("unsupported array cardinality %q on %s", d.Cardinality, name)
	}
	refs = append(refs, d.DataType)
	e.optional("dataType", d.DataType)
	e.optional("cardinality", d.Cardinality)
	e.optional("encoding", d.Encoding)
	e.descriptive("semantics", d.Semantics)
	return e, refs, nil
}

func exportFixedRecordData(name string, d *model.FixedRecordData) (element, []string, error) {
	var refs []string
	e := node("fixedRecordData", textNode("name", name))
	e.optional("encoding", d.Encoding)
	e.descriptive("semantics", d.Semantics)
	seen := map[string]bool{}
	for _, field := range d.Fields {
		if seen[field.Name] {
			return element{}, nil, unsupported("duplicate record field %s.%s", name, field.Name)
		}
		seen[field.Name] = true
		refs = append(refs, field.DataType)
		item := node("field", textNode("name", field.Name))
		item.optional("dataType", field.DataType)
		item.descriptive("semantics", field.Semantics)
		e.Children = append(e.Children, item)
	}
	return e, refs, nil
}

func exportVariantRecordData(name string, d *model.VariantRecordData) (element, []string, error) {
	var refs []string
	e := node("variantRecordData", textNode("name", name))
	refs = append(refs, d.DiscriminantType)
	e.optional("discriminant", d.DiscriminantName)
	e.optional("dataType", d.DiscriminantType)
	for _, alternative := range d.Alternatives {
		refs = append(refs, alternative.DataType)
		item := node("alternative")
		item.optional("enumerator", alternative.Enumerator)
		item.optional("name", alternative.Name)
		item.optional("dataType", alternative.DataType)
		item.descriptive("semantics", alternative.Semantics)
		e.Children = append(e.Children, item)
	}
	e.optional("encoding", d.Encoding)
	e.descriptive("semantics", d.Semantics)
	return e, refs, nil
}
