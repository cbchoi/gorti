package federate

import (
	"context"
	"fmt"
	"time"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// tablesFromFOMView never reparses source: handles reflect the server's live
// composition history and cannot be recovered from sorted model positions.
func tablesFromFOMView(view *rtiv1.FOMView) (*fomTables, error) {
	if view == nil || view.GetRevision() == 0 {
		return nil, fmt.Errorf("missing authoritative FOM revision")
	}
	tables := newFOMTables()
	tables.revision, tables.digest = view.GetRevision(), view.GetDigest()
	if err := readClassBindings(view.GetObjectClasses(), "HLAobjectRoot", tables.objectByName, tables.objectByHandle, tables.objectCanonical, tables.attrByName, tables.attrByHandle, true); err != nil {
		return nil, err
	}
	if err := readClassBindings(view.GetInteractionClasses(), "HLAinteractionRoot", tables.interactionByName, tables.interactionByHandle, tables.interactionCanonical, tables.paramByName, tables.paramByHandle, false); err != nil {
		return nil, err
	}
	for _, d := range view.GetDimensions() {
		if d == nil || d.GetHandle() == 0 || d.GetName() == "" {
			return nil, fmt.Errorf("invalid dimension binding")
		}
		if _, ok := tables.dimensionByName[d.GetName()]; ok {
			return nil, fmt.Errorf("duplicate dimension name %q", d.GetName())
		}
		if _, ok := tables.dimensionByHandle[d.GetHandle()]; ok {
			return nil, fmt.Errorf("duplicate dimension handle %d", d.GetHandle())
		}
		tables.dimensionByName[d.GetName()] = d.GetHandle()
		tables.dimensionByHandle[d.GetHandle()] = d.GetName()
		tables.dimensionUpperBound[d.GetHandle()] = d.GetUpperBound()
	}
	return tables, nil
}

func readClassBindings(classes []*rtiv1.FOMClassBinding, root string, forward map[string]uint64, reverse, canonical map[uint64]string, membersByName map[string]map[string]uint64, membersByHandle map[uint64]map[uint64]string, attributes bool) error {
	if err := readClassAliases(classes, root, forward, canonical); err != nil {
		return err
	}
	for _, c := range classes {
		name := c.GetName()
		if local := model.LocalClassName(name); forward[local] == c.GetHandle() {
			name = local
		}
		reverse[c.GetHandle()] = name
		forwardMembers := make(map[string]uint64, len(c.GetMembers()))
		reverseMembers := make(map[uint64]string, len(c.GetMembers()))
		for _, m := range c.GetMembers() {
			if m == nil || m.GetHandle() == 0 || m.GetName() == "" {
				return fmt.Errorf("invalid member in %s", c.GetName())
			}
			if _, ok := forwardMembers[m.GetName()]; ok {
				return fmt.Errorf("duplicate member %s.%s", c.GetName(), m.GetName())
			}
			if _, ok := reverseMembers[m.GetHandle()]; ok {
				return fmt.Errorf("duplicate member handle %d in %s", m.GetHandle(), c.GetName())
			}
			forwardMembers[m.GetName()] = m.GetHandle()
			reverseMembers[m.GetHandle()] = m.GetName()
		}
		if attributes {
			if h, ok := forwardMembers["HLAprivilegeToDeleteObject"]; ok {
				if legacy, exists := forwardMembers["HLAprivilegeToDelete"]; exists && legacy != h {
					return fmt.Errorf("conflicting privilege alias in %s", c.GetName())
				}
				forwardMembers["HLAprivilegeToDelete"] = h
			}
		}
		membersByName[name] = forwardMembers
		membersByHandle[c.GetHandle()] = reverseMembers
	}
	return nil
}

func (t *fomTables) current() *fomTables {
	if t != nil {
		if latest := t.latest.Load(); latest != nil {
			return latest
		}
	}
	return t
}

func lookupFOM[T any](tables *fomTables, lookup func(*fomTables) (T, bool)) (T, bool) {
	if tables == nil {
		var zero T
		return zero, false
	}
	value, ok := lookup(tables.current())
	if ok || tables.refresh == nil {
		return value, ok
	}
	tables.refresh()
	return lookup(tables.current())
}

func bindingsExtend(old, next *fomTables) bool {
	for h, name := range old.objectCanonical {
		if next.objectCanonical[h] != name {
			return false
		}
	}
	for h, name := range old.interactionCanonical {
		if next.interactionCanonical[h] != name {
			return false
		}
	}
	for c, members := range old.attrByHandle {
		for h, name := range members {
			if next.attrByHandle[c][h] != name {
				return false
			}
		}
	}
	for c, members := range old.paramByHandle {
		for h, name := range members {
			if next.paramByHandle[c][h] != name {
				return false
			}
		}
	}
	for h, name := range old.dimensionByHandle {
		if next.dimensionByHandle[h] != name || next.dimensionUpperBound[h] != old.dimensionUpperBound[h] {
			return false
		}
	}
	return true
}

func (t *fomTables) install(next *fomTables) error {
	for {
		previous := t.latest.Load()
		current := previous
		if current == nil {
			current = t
		}
		if next.revision < current.revision {
			return nil
		}
		if !bindingsExtend(current, next) {
			return fmt.Errorf("authoritative FOM view remaps existing handles")
		}
		if next.revision == current.revision {
			if next.digest != current.digest || !bindingsExtend(next, current) {
				return fmt.Errorf("conflicting authoritative FOM views at revision %d", next.revision)
			}
			return nil
		}
		if t.latest.CompareAndSwap(previous, next) {
			return nil
		}
	}
}

// RefreshFOM fetches and atomically installs the server's authoritative mapping.
// Older concurrent responses cannot replace a newer successfully installed view.
func (f *Federate) RefreshFOM(ctx context.Context) error {
	if f == nil || f.conn == nil || f.conn.fed == nil || f.handles == nil {
		return fmt.Errorf("federate: cannot refresh FOM before joining")
	}
	epoch := f.handles.refreshEpoch.Load()
	view, err := f.conn.fed.GetFOMView(ctx, &rtiv1.GetFOMViewRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName, FederateHandle: f.CurrentHandle()})
	if err != nil {
		return fmt.Errorf("federate: GetFOMView: %w", err)
	}
	tables, err := tablesFromFOMView(view)
	if err != nil {
		return fmt.Errorf("federate: invalid FOM view: %w", err)
	}
	f.handles.publishMu.Lock()
	defer f.handles.publishMu.Unlock()
	if f.handles.refreshEpoch.Load() != epoch {
		return fmt.Errorf("federate: FOM refresh superseded by restore")
	}
	return f.handles.install(tables)
}

// restoreFOMRefresh permits an older or smaller mapping only at an explicit
// completed-restore boundary. Callers must suspend operations for restore.
// Epoch checks prevent pre-restore RefreshFOM replies from republishing tables.
func (f *Federate) restoreFOMRefresh(ctx context.Context) error {
	if f == nil || f.conn == nil || f.conn.fed == nil || f.handles == nil {
		return fmt.Errorf("federate: cannot restore FOM before joining")
	}
	f.handles.publishMu.Lock()
	epoch := f.handles.refreshEpoch.Add(1)
	f.handles.publishMu.Unlock()
	view, err := f.conn.fed.GetFOMView(ctx, &rtiv1.GetFOMViewRequest{WireVersion: rtiv1.WireVersion_WIRE_VERSION_V1, FederationName: f.federationName, FederateHandle: f.CurrentHandle()})
	if err != nil {
		return fmt.Errorf("federate: restore GetFOMView: %w", err)
	}
	tables, err := tablesFromFOMView(view)
	if err != nil {
		return fmt.Errorf("federate: invalid restored FOM view: %w", err)
	}
	f.handles.publishMu.Lock()
	defer f.handles.publishMu.Unlock()
	if f.handles.refreshEpoch.Load() != epoch {
		return fmt.Errorf("federate: FOM restore refresh superseded")
	}
	f.handles.latest.Store(tables)
	return nil
}

func (f *Federate) DimensionHandle(name string) (uint64, bool) {
	return lookupFOM(f.handles, func(current *fomTables) (uint64, bool) { h, ok := current.dimensionByName[name]; return h, ok })
}

func (f *Federate) DimensionName(handle uint64) (string, bool) {
	return lookupFOM(f.handles, func(current *fomTables) (string, bool) {
		name, ok := current.dimensionByHandle[handle]
		return name, ok
	})
}

func (f *Federate) DimensionUpperBound(handle uint64) (uint64, bool) {
	return lookupFOM(f.handles, func(current *fomTables) (uint64, bool) {
		bound, ok := current.dimensionUpperBound[handle]
		return bound, ok
	})
}

// Call only during Join initialization, before publishing f to callback readers.
func (f *Federate) bindFOMRefresh() {
	if f.handles == nil {
		return
	}
	f.handles.refresh = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = f.RefreshFOM(ctx)
	}
}

// FOMRevision reports the currently installed authoritative revision. Zero is
// the legacy locally parsed mapping used with servers without FOM views.
func (f *Federate) FOMRevision() uint64 {
	if f == nil || f.handles == nil {
		return 0
	}
	return f.handles.current().revision
}

func readClassAliases(classes []*rtiv1.FOMClassBinding, root string, forward map[string]uint64, canonical map[uint64]string) error {
	names := make(map[string]bool, len(classes))
	for _, c := range classes {
		if c == nil || c.GetHandle() == 0 || c.GetName() == "" {
			return fmt.Errorf("invalid %s class binding", root)
		}
		if _, ok := canonical[c.GetHandle()]; ok {
			return fmt.Errorf("duplicate class handle %d", c.GetHandle())
		}
		if names[c.GetName()] {
			return fmt.Errorf("duplicate class name %q", c.GetName())
		}
		names[c.GetName()] = true
		canonical[c.GetHandle()] = c.GetName()
		addClassAliases(forward, model.LocalClassName(c.GetName()), c.GetName(), root, c.GetHandle())
	}
	return nil
}
