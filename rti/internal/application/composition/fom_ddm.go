package composition

import (
	"slices"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// AttributeDimensions resolves the attribute's own declaration, including
// inherited attributes, using the same flattened handles as LookupAttribute.
func (h *fomHandle) AttributeDimensions(cls core.ObjectClassHandle, attr core.AttributeHandle) ([]core.DDMDimensionHandle, bool) {
	declaration, ok := h.attributeDeclaration(cls, attr)
	if !ok {
		return nil, false
	}
	result := make([]core.DDMDimensionHandle, 0, len(declaration.Dimensions))
	for _, name := range declaration.Dimensions {
		dim, ok := h.LookupDimension(name)
		if !ok {
			return nil, false
		}
		result = append(result, core.DDMDimensionHandle(dim))
	}
	slices.Sort(result)
	return slices.Compact(result), true
}

func (h *fomHandle) attributeDeclaration(cls core.ObjectClassHandle, attr core.AttributeHandle) (model.Attribute, bool) {
	attributes := h.effectiveAttributes(cls)
	if attr == 0 || uint64(attr) > uint64(len(attributes)) {
		return model.Attribute{}, false
	}
	return attributes[attr-1], true
}

// AttributeHandles enumerates the complete class view instead of a fixed
// attribute-number probe, which cannot represent larger FOMs.
func (h *fomHandle) AttributeHandles(cls core.ObjectClassHandle) []core.AttributeHandle {
	count := len(h.effectiveAttributes(cls))
	attrs := make([]core.AttributeHandle, count)
	for i := range attrs {
		attrs[i] = core.AttributeHandle(i) + 1 //nolint:gosec // G115: a slice index is nonnegative and fits the uint64 handle.
	}
	return attrs
}
