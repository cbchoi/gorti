package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

// RegionDimensionRange is one realized, not pending, dimension interval.
type RegionDimensionRange struct {
	Dimension DDMDimensionHandle `json:"dimension"`
	Bounds    DDMRange           `json:"bounds"`
}

// ConveyedRegionCopy binds an opaque readonly designator to its portable value.
type ConveyedRegionCopy struct {
	Designator  DDMRegionHandleCore       `json:"designator"`
	Realization RegionRealizationSnapshot `json:"realization"`
}

type AttributeRegionDeliverySnapshot struct {
	Regions         []DDMRegionHandleCore
	Copies          []ConveyedRegionCopy
	Subscribers     []FederateHandle
	OverlapsDefault bool
}

func (m *ObjectCallbackMetadata) ConveyedRegionDesignators() []DDMRegionHandleCore {
	if m == nil {
		return nil
	}
	if m.ConveyedRegions == nil {
		return slices.Clone(m.Regions)
	}
	result := make([]DDMRegionHandleCore, len(m.ConveyedRegions))
	for i, copy := range m.ConveyedRegions {
		result[i] = copy.Designator
	}
	return result
}

// RegionRealizationSnapshot owns a copy of the update realization used in an
// overlap calculation. SourceRegion is provenance, not a conveyed designator.
// Its accessors return values/copies; it exposes no range mutation operation.
type RegionRealizationSnapshot struct {
	source     DDMRegionHandleCore
	dimensions []RegionDimensionRange
}

func NewRegionRealizationSnapshot(source DDMRegionHandleCore, dimensions []RegionDimensionRange) (RegionRealizationSnapshot, error) {
	if source == 0 {
		return RegionRealizationSnapshot{}, ErrRegionNotFound
	}
	dimensions = slices.Clone(dimensions)
	slices.SortFunc(dimensions, func(a, b RegionDimensionRange) int {
		if a.Dimension < b.Dimension {
			return -1
		}
		if a.Dimension > b.Dimension {
			return 1
		}
		return 0
	})
	for i, dimension := range dimensions {
		if dimension.Dimension == 0 || i > 0 && dimensions[i-1].Dimension == dimension.Dimension {
			return RegionRealizationSnapshot{}, fmt.Errorf("region realization: invalid or duplicate dimension")
		}
		// FDD defaults can realize a zero-width interval; Set Range Bounds
		// has a stricter admission rule that does not apply to a saved copy.
		if dimension.Bounds.Lower > dimension.Bounds.Upper {
			return RegionRealizationSnapshot{}, ErrInvalidRangeBounds
		}
	}
	return RegionRealizationSnapshot{source: source, dimensions: dimensions}, nil
}

func (s RegionRealizationSnapshot) SourceRegion() DDMRegionHandleCore { return s.source }
func (s RegionRealizationSnapshot) DimensionRanges() []RegionDimensionRange {
	return slices.Clone(s.dimensions)
}

func (s RegionRealizationSnapshot) RangeBounds(dimension DDMDimensionHandle) (DDMRange, bool) {
	i, found := slices.BinarySearchFunc(s.dimensions, dimension, func(value RegionDimensionRange, dimension DDMDimensionHandle) int {
		if value.Dimension < dimension {
			return -1
		}
		if value.Dimension > dimension {
			return 1
		}
		return 0
	})
	if !found {
		return DDMRange{}, false
	}
	return s.dimensions[i].Bounds, true
}

type regionRealizationJSON struct {
	SourceRegion DDMRegionHandleCore    `json:"sourceRegion"`
	Dimensions   []RegionDimensionRange `json:"dimensions"`
}

func (s RegionRealizationSnapshot) MarshalJSON() ([]byte, error) {
	if s.source == 0 {
		return nil, ErrRegionNotFound
	}
	return json.Marshal(regionRealizationJSON{SourceRegion: s.source, Dimensions: s.dimensions})
}

func (s *RegionRealizationSnapshot) UnmarshalJSON(data []byte) error {
	var wire regionRealizationJSON
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("region realization: trailing JSON value")
		}
		return err
	}
	snapshot, err := NewRegionRealizationSnapshot(wire.SourceRegion, wire.Dimensions)
	if err != nil {
		return err
	}
	*s = snapshot
	return nil
}
