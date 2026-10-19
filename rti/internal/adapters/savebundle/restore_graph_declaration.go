package savebundle

import (
	"fmt"
	"maps"
	"math"
	"slices"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"google.golang.org/protobuf/proto"
)

func (v *graphValidationOf[T]) validateDDM(data []byte) error {
	v.regionalObjects = map[[3]uint64]bool{}
	v.regionalInteractions = map[[2]uint64]bool{}
	var s rtiv1.DDMManagerState
	if err := proto.Unmarshal(data, &s); err != nil {
		return err
	}
	if err := validateDDMTemplates(&s); err != nil {
		return err
	}
	copiedState, err := decodeDDMState("", data)
	if err != nil {
		return err
	}
	if err := validateDDMCopiedFOM(copiedState, v.model); err != nil {
		return err
	}
	for _, copy := range copiedState.ConveyedRegions {
		for _, receiver := range copy.Recipients {
			if !v.member(receiver.Federate) {
				return fmt.Errorf("copied region receiver is not a saved participant")
			}
		}
	}
	if s.NextRegionHandle == math.MaxUint64 {
		return fmt.Errorf("exhausted region counter")
	}
	if err := v.validateDDMRegions(&s); err != nil {
		return err
	}
	if err := v.validateDDMObjectSubscriptions(&s); err != nil {
		return err
	}
	if err := v.validateDDMInteractionSubscriptions(&s); err != nil {
		return err
	}
	if err := v.validateDDMPublications(&s); err != nil {
		return err
	}
	if err := v.validateDDMInactiveAssociations(&s); err != nil {
		return err
	}
	return nil
}

func (v *graphValidationOf[T]) inactiveAssociationKnown(obj core.ObjectHandle, attr core.AttributeHandle, caller core.FederateHandle) bool {
	known := v.knownClasses[[2]uint64{uint64(obj), uint64(caller)}]
	actual := v.objects[obj].Class
	if known == 0 || !v.validKnownClass(actual, known) {
		return false
	}
	// Tombstone knowledge also serves retraction validation, but forgotten
	// recipients cannot retain an inactive update-region association.
	for _, recipient := range v.deletions[obj].Recipients {
		if recipient.Recipient == caller && recipient.Forgotten {
			return false
		}
	}
	name, ok := v.names.AttributeName(actual, attr)
	if !ok {
		return false
	}
	_, ok = v.model.LookupAttribute(known, name)
	return ok
}

func (v *graphValidationOf[T]) regionSet(owner core.FederateHandle, handles []uint64, dims []core.DDMDimensionHandle, strict bool) error {
	seen := map[uint64]bool{}
	for _, h := range handles {
		r := v.regions[h]
		if r == nil || r.GetTemplate() || r.OwnerFederate != uint64(owner) || seen[h] {
			return fmt.Errorf("region association owner/reference mismatch")
		}
		seen[h] = true
		if strict {
			if len(dims) == 0 {
				return fmt.Errorf("region context has no available dimensions")
			}
			// Region dimensions may be a proper subset of the available set.
			for _, dim := range r.DimensionHandles {
				if !slices.Contains(dims, core.DDMDimensionHandle(dim)) {
					return fmt.Errorf("region dimension context mismatch")
				}
			}
		}
	}
	return nil
}
func (v *graphValidationOf[T]) rate(name string) error {
	if name == "" {
		return nil
	}
	rates, ok := v.model.(core.FOMUpdateRates)
	if !ok {
		return fmt.Errorf("saved FOM has no update rates")
	}
	value, ok := rates.UpdateRateValue(name)
	if !ok || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("unknown update rate %q", name)
	}
	return nil
}

func (v *graphValidationOf[T]) validateDeclaration(fed core.FederationName, data []byte) error {
	s, err := decodeDeclarationState(fed, data)
	if err != nil {
		return err
	}
	// Use the manager's structural/history validation without committing state.
	if _, err := declaration.New().PrepareRestoreWithFOM(fed, s, v.model); err != nil {
		return err
	}
	for _, entry := range s.ObjectPublicationHistory {
		if _, ok := v.names.ObjectClassName(entry.Class); !ok || !v.member(entry.Federate) && !(entry.Federate == ^core.FederateHandle(0) && v.management(entry.Class)) {
			return fmt.Errorf("publication history has invalid class or participant")
		}
	}
	if err := v.validateRegionalInterests(s); err != nil {
		return err
	}
	if err := v.validateObjectDeclarations(s); err != nil {
		return err
	}
	if err := v.validateInteractionDeclarations(s); err != nil {
		return err
	}
	if err := v.validateDeclarationOptions(s); err != nil {
		return err
	}
	if err := v.validateDeclarationUpdateRates(s); err != nil {
		return err
	}
	if err := v.validatePendingAdvisories(s); err != nil {
		return err
	}
	return nil
}

func (v *graphValidationOf[T]) validateDDMRegions(s *rtiv1.DDMManagerState) error {
	for _, r := range s.Regions {
		if r == nil || r.RegionHandle == 0 || r.RegionHandle > s.NextRegionHandle || v.regions[r.RegionHandle] != nil || !v.member(core.FederateHandle(r.OwnerFederate)) || r.RoutingSpaceHandle != 1 {
			return fmt.Errorf("invalid region identity/owner/context")
		}
		if err := v.validateDDMRegionDimensions(r); err != nil {
			return err
		}
		v.regions[r.RegionHandle] = r
	}
	return nil
}

func (v *graphValidationOf[T]) validateDDMObjectSubscriptions(s *rtiv1.DDMManagerState) error {
	objDims, hasObjDims := v.model.(core.FOMAttributeDimensions)
	seenObjectRegions := map[[4]uint64]bool{}
	for _, s := range s.ObjectSubscriptions {
		if s == nil {
			return fmt.Errorf("nil DDM object subscription")
		}
		key := [3]uint64{s.ObjectClassHandle, s.AttributeHandle, s.SubscriberFederate}
		cls, attr, h := core.ObjectClassHandle(s.ObjectClassHandle), core.AttributeHandle(s.AttributeHandle), core.FederateHandle(s.SubscriberFederate)
		if !v.member(h) || !v.attribute(cls, attr) || len(s.RegionHandles) == 0 {
			return fmt.Errorf("invalid DDM object subscription")
		}
		// A subscriber may have disjoint rows with different options (9.8).
		// Only repeated class/attribute/region triples are invalid.
		for _, region := range s.RegionHandles {
			triple := [4]uint64{s.ObjectClassHandle, s.AttributeHandle, s.SubscriberFederate, region}
			if seenObjectRegions[triple] {
				return fmt.Errorf("duplicate DDM object subscription region")
			}
			seenObjectRegions[triple] = true
		}
		if !s.Passive {
			v.regionalObjects[key] = true
		}
		var dims []core.DDMDimensionHandle
		if hasObjDims {
			var ok bool
			dims, ok = objDims.AttributeDimensions(cls, attr)
			if !ok {
				return fmt.Errorf("missing attribute dimension context")
			}
		}
		if err := v.regionSet(h, s.RegionHandles, dims, hasObjDims); err != nil {
			return err
		}
		if err := v.rate(s.UpdateRateDesignator); err != nil {
			return err
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateDDMInteractionSubscriptions(s *rtiv1.DDMManagerState) error {
	intDims, hasIntDims := v.model.(core.FOMInteractionDimensions)
	seenInteractionRegions := map[[3]uint64]bool{}
	for _, s := range s.InteractionSubscriptions {
		if s == nil {
			return fmt.Errorf("nil DDM interaction subscription")
		}
		cls, h := core.InteractionClassHandle(s.InteractionClassHandle), core.FederateHandle(s.SubscriberFederate)
		if _, ok := v.names.InteractionClassName(cls); !ok || !v.member(h) || len(s.RegionHandles) == 0 {
			return fmt.Errorf("invalid DDM interaction subscription")
		}
		// Disjoint option groups are valid; each class/region pair is unique
		// per subscriber, regardless of its active/passive option (9.10).
		for _, region := range s.RegionHandles {
			key := [3]uint64{s.InteractionClassHandle, s.SubscriberFederate, region}
			if seenInteractionRegions[key] {
				return fmt.Errorf("duplicate DDM interaction subscription region")
			}
			seenInteractionRegions[key] = true
		}
		if !s.Passive {
			v.regionalInteractions[[2]uint64{s.InteractionClassHandle, s.SubscriberFederate}] = true
		}
		var dims []core.DDMDimensionHandle
		if hasIntDims {
			var ok bool
			dims, ok = intDims.InteractionDimensions(cls)
			if !ok {
				return fmt.Errorf("missing interaction dimension context")
			}
		}
		if err := v.regionSet(h, s.RegionHandles, dims, hasIntDims); err != nil {
			return err
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateDDMPublications(s *rtiv1.DDMManagerState) error {
	objDims, hasObjDims := v.model.(core.FOMAttributeDimensions)
	seen := map[[3]uint64]bool{}
	for _, p := range s.ObjectPublications {
		if p == nil {
			return fmt.Errorf("nil region association")
		}
		key := [3]uint64{p.ObjectHandle, p.AttributeHandle}
		obj, attr := core.ObjectHandle(p.ObjectHandle), core.AttributeHandle(p.AttributeHandle)
		if seen[key] || !v.objectAttribute(obj, attr) {
			return fmt.Errorf("invalid region association")
		}
		seen[key] = true
		owner := v.owners[[2]uint64{p.ObjectHandle, p.AttributeHandle}]
		var dims []core.DDMDimensionHandle
		if hasObjDims {
			var ok bool
			dims, ok = objDims.AttributeDimensions(v.objects[obj].Class, attr)
			if !ok {
				return fmt.Errorf("missing associated attribute dimensions")
			}
		}
		if err := v.regionSet(owner, p.RegionHandles, dims, hasObjDims); err != nil {
			return err
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateDDMInactiveAssociations(s *rtiv1.DDMManagerState) error {
	objDims, hasObjDims := v.model.(core.FOMAttributeDimensions)
	seenInactive := map[[3]uint64]bool{}
	for _, p := range s.InactiveAssociations {
		if p == nil {
			return fmt.Errorf("nil inactive region association")
		}
		key := [3]uint64{p.ObjectHandle, p.AttributeHandle, p.AssociatingFederate}
		obj, attr, associator := core.ObjectHandle(p.ObjectHandle), core.AttributeHandle(p.AttributeHandle), core.FederateHandle(p.AssociatingFederate)
		if seenInactive[key] || !v.member(associator) || !v.objectAttribute(obj, attr) || len(p.RegionHandles) == 0 {
			return fmt.Errorf("invalid inactive region association")
		}
		seenInactive[key] = true
		if v.owners[[2]uint64{p.ObjectHandle, p.AttributeHandle}] == associator {
			return fmt.Errorf("inactive region association belongs to current attribute owner")
		}
		if !v.inactiveAssociationKnown(obj, attr, associator) {
			return fmt.Errorf("inactive region association is outside associator's object knowledge")
		}
		var dims []core.DDMDimensionHandle
		if hasObjDims {
			var ok bool
			dims, ok = objDims.AttributeDimensions(v.objects[obj].Class, attr)
			if !ok {
				return fmt.Errorf("missing inactive attribute dimensions")
			}
		}
		if err := v.regionSet(associator, p.RegionHandles, dims, hasObjDims); err != nil {
			return err
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateDDMRegionDimensions(r *rtiv1.DDMRegionEntry) error {
	dims := map[uint64]uint64{}
	for _, d := range r.DimensionHandles {
		upper, ok := v.names.DimensionUpperBound(core.DimensionHandle(d))
		if !ok || d == 0 {
			return fmt.Errorf("region references unknown dimension")
		}
		if _, dup := dims[d]; dup {
			return fmt.Errorf("duplicate region dimension")
		}
		dims[d] = upper
	}
	if err := validateDDMRegionRanges(r, dims); err != nil {
		return err
	}
	return nil
}

func validateDDMRegionRanges(r *rtiv1.DDMRegionEntry, dims map[uint64]uint64) error {
	for i, ranges := range [][]*rtiv1.DDMRangeEntry{r.Committed, r.Pending} {
		if i == 0 && len(ranges) != len(dims) {
			return fmt.Errorf("committed region ranges do not cover dimensions")
		}
		seen := map[uint64]bool{}
		for _, b := range ranges {
			if b == nil {
				return fmt.Errorf("nil region range")
			}
			upper, ok := dims[b.DimensionHandle]
			if !ok || seen[b.DimensionHandle] || b.Lower > b.Upper || b.Upper > upper {
				return fmt.Errorf("invalid region range")
			}
			if r.GetTemplate() && i == 0 && b.Upper != upper {
				return fmt.Errorf("template initial range is not the full dimension range")
			}
			seen[b.DimensionHandle] = true
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateRegionalInterests(s *declaration.ManagerState) error {
	regionalObjects := map[[3]uint64]bool{}
	for _, entry := range s.RegionalObjectInterests {
		for _, h := range entry.Federates {
			regionalObjects[[3]uint64{uint64(entry.Class), uint64(entry.Attribute), uint64(h)}] = true
		}
	}
	regionalInteractions := map[[2]uint64]bool{}
	for _, entry := range s.RegionalInteractionInterests {
		for _, h := range entry.Federates {
			regionalInteractions[[2]uint64{uint64(entry.Class), uint64(h)}] = true
		}
	}
	if !maps.Equal(regionalObjects, v.regionalObjects) || !maps.Equal(regionalInteractions, v.regionalInteractions) {
		return fmt.Errorf("DDM and declaration regional interest disagree")
	}
	return nil
}

func (v *graphValidationOf[T]) validateObjectDeclarations(s *declaration.ManagerState) error {
	for _, group := range [][]declaration.ObjectDeclarationState{s.ObjectPublications, s.ObjectSubscriptions, s.RegionalObjectInterests} {
		for _, e := range group {
			if !v.attribute(e.Class, e.Attribute) {
				return fmt.Errorf("declaration references unknown attribute")
			}
			seen := map[core.FederateHandle]bool{}
			for _, h := range e.Federates {
				if seen[h] || (!v.member(h) && !(h == ^core.FederateHandle(0) && v.management(e.Class))) {
					return fmt.Errorf("declaration has invalid participant")
				}
				seen[h] = true
			}
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateInteractionDeclarations(s *declaration.ManagerState) error {
	for _, group := range [][]declaration.InteractionDeclarationState{s.InteractionPublications, s.InteractionSubscriptions, s.RegionalInteractionInterests} {
		for _, e := range group {
			if _, ok := v.names.InteractionClassName(e.Class); !ok {
				return fmt.Errorf("declaration references unknown interaction")
			}
			for _, h := range e.Federates {
				if !v.member(h) {
					return fmt.Errorf("interaction declaration has unknown participant")
				}
			}
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateDeclarationOptions(s *declaration.ManagerState) error {
	for _, e := range s.ObjectOptions {
		if !v.member(e.Federate) || !v.attribute(e.Class, e.Attribute) {
			return fmt.Errorf("invalid object subscription options reference")
		}
		if err := v.rate(e.Options.UpdateRateDesignator); err != nil {
			return err
		}
	}
	for _, e := range s.InteractionOptions {
		if _, ok := v.names.InteractionClassName(e.Class); !ok || !v.member(e.Federate) {
			return fmt.Errorf("invalid interaction subscription options reference")
		}
	}
	for _, e := range s.AdvisorySwitches {
		if !v.member(e.Federate) && e.Federate != ^core.FederateHandle(0) {
			return fmt.Errorf("invalid advisory switch participant")
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validateDeclarationUpdateRates(s *declaration.ManagerState) error {
	for _, e := range s.UpdateRateObjects {
		if v.objects[e.Object].Handle == 0 || v.objects[e.Object].Class != e.Class {
			return fmt.Errorf("update rate object class mismatch")
		}
		for _, o := range e.Owners {
			if !v.attribute(e.Class, o.Attribute) || v.owners[[2]uint64{uint64(e.Object), uint64(o.Attribute)}] != o.Owner {
				return fmt.Errorf("update rate ownership mismatch")
			}
		}
	}
	for _, e := range s.RegionalUpdateRates {
		if !v.objectAttribute(e.Object, e.Attribute) || !v.member(e.Subscriber) {
			return fmt.Errorf("invalid regional update-rate reference")
		}
		if err := v.rate(e.Options.UpdateRateDesignator); err != nil {
			return err
		}
	}
	return nil
}

func (v *graphValidationOf[T]) validatePendingAdvisories(s *declaration.ManagerState) error {
	for _, e := range s.PendingAdvisories {
		if !v.member(e.Recipient) {
			return fmt.Errorf("pending advisory recipient is not joined")
		}
		if e.Object != 0 && !v.objectAttribute(e.Object, e.Attribute) {
			return fmt.Errorf("pending advisory unknown object attribute")
		}
	}
	return nil
}
