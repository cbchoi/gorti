// MOM object-instance fan-out (M36 DD-2).
//
// IEEE 1516-2010 §10 / 1516.1-2010 §11: the RTI maintains one
// HLAmanager.HLAfederation object instance per federation and one
// HLAmanager.HLAfederate instance per joined federate, delivered to
// subscribers through the STANDARD object-management callbacks
// (discoverObjectInstance / reflectAttributeValues /
// removeObjectInstance) — no bespoke MOM API.
//
// This file closes the deferred follow-up documented on Manager
// (manager.go: "the subscriber fan-out is a follow-up"): the MOM now
// registers its instances through the standard object.Registry path so
// the normal discover/reflect/remove fan-out fires, with the RTI
// itself acting as the producing "federate" (momProducer).
//
// Late-subscriber semantics: a federate that subscribes to a MOM
// object class AFTER instances already exist receives a retroactive
// Discover + Reflect pair per existing instance, sent directly through
// the Outbox. Successful discoveries are recorded in the object registry
// so its scope and checkpoint state share the same object knowledge. The hook is
// declaration.Manager.SetOnSubscribeObjectClass → ObjectClassSubscribed.

package mom

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"unicode/utf16"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// momProducer is the internal FederateHandle the RTI uses as the
// producer/owner of MOM object instances. Max-uint64 can never collide
// with real federate handles (the federation manager allocates small
// monotonic handles starting at 1) and is excluded from fan-out only
// as "the producer", which no real subscriber ever equals.
const momProducer = ^core.FederateHandle(0)

// Standard MOM attribute names this cut maintains on the wire.
const (
	attrNameFederationName        = "HLAfederationName"
	attrNameFederatesInFederation = "HLAfederatesInFederation"
	attrNameFederateHandle        = "HLAfederateHandle"
	attrNameFederateName          = "HLAfederateName"
	attrNameFederateType          = "HLAfederateType"
)

// instanceFanoutDeps bundles the composition-time dependencies for the
// object-instance fan-out. All three are required for the fan-out to
// engage; when unset (nil zero value) every fan-out step is a silent
// no-op and the Manager behaves exactly as pre-M36 (snapshot only).
type instanceFanoutDeps struct {
	objects   core.ObjectRegistry
	decls     core.DeclarationManagement
	foms      core.FOMRepository
	publisher core.MOMFederateAttributePublisher
}

// momClassHandles is the per-federation FOM-resolved handle set plus
// the registered HLAfederation instance. Stored on momState after
// FederationCreated resolves it; ok=false means the federation's FOM
// does not declare the MOM classes (e.g. no MIM merged) and fan-out is
// disabled for that federation.
type momClassHandles struct {
	ok bool

	federationCls core.ObjectClassHandle
	federateCls   core.ObjectClassHandle

	attrFederationName        core.AttributeHandle
	attrFederatesInFederation core.AttributeHandle
	attrFederateHandle        core.AttributeHandle
	attrFederateName          core.AttributeHandle
	attrFederateType          core.AttributeHandle

	federationObj     core.ObjectHandle
	federationObjName string
}

// EnableInstanceFanout wires the object-registry-backed MOM instance
// fan-out. Called once from cmd/rtid composition AFTER the object
// registry is constructed (the MOM Manager itself is constructed
// before the registry because the registry's ManagementDispatch
// depends on the MOM dispatcher). Not goroutine-safe against
// concurrent hook invocations — call during composition, before the
// server starts accepting RPCs.
func (m *Manager) EnableInstanceFanout(
	objects core.ObjectRegistry,
	decls core.DeclarationManagement,
	foms core.FOMRepository,
) {
	m.fanout.objects, m.fanout.decls, m.fanout.foms = objects, decls, foms
}

// SetFederateAttributePublisher installs 11.4.1(h) point delivery before joins.
func (m *Manager) SetFederateAttributePublisher(publisher core.MOMFederateAttributePublisher) error {
	if publisher == nil {
		return fmt.Errorf("mom: federate attribute publisher is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, st := range m.fed {
		if len(st.federates) != 0 {
			return fmt.Errorf("mom: federate attribute publisher must be configured before joins")
		}
	}
	m.fanout.publisher = publisher
	return nil
}

func (m *Manager) fanoutEnabled() bool {
	return m.fanout.objects != nil && m.fanout.decls != nil && m.fanout.foms != nil
}

// --- FederationCreated fan-out ---------------------------------------------

// setupFederationInstance resolves the MOM class/attribute handles for
// fed's FOM, publishes them for momProducer, registers the singleton
// HLAfederation instance, and pushes the initial attribute values.
// Called by FederationCreated OUTSIDE the manager mutex (the registry
// re-enters the Manager via the OnUpdateSent counter hook, which takes
// the manager RLock — holding the write lock here would deadlock).
//
// Idempotent: a second call for the same federation is a no-op (the
// resolved handle set is already stored).
func (m *Manager) setupFederationInstance(ctx context.Context, fed core.FederationName) error {
	if !m.fanoutEnabled() {
		return nil
	}
	m.mu.RLock()
	st, ok := m.fed[fed]
	already := ok && st.objects.ok
	m.mu.RUnlock()
	if !ok || already {
		return nil
	}

	fomH, err := m.fanout.foms.Get(ctx, fed)
	if err != nil || fomH == nil || !fomH.IsValid() {
		return nil // no FOM recorded (yet) — fan-out stays off for fed
	}
	var h momClassHandles
	h.federationCls, ok = fomH.LookupObjectClass(ClassHLAfederation)
	if !ok {
		return nil // FOM has no MIM — silently keep snapshot-only mode
	}
	h.federateCls, ok = fomH.LookupObjectClass(ClassHLAfederate)
	if !ok {
		return nil
	}
	h.attrFederationName, _ = fomH.LookupAttribute(h.federationCls, attrNameFederationName)
	h.attrFederatesInFederation, _ = fomH.LookupAttribute(h.federationCls, attrNameFederatesInFederation)
	h.attrFederateHandle, _ = fomH.LookupAttribute(h.federateCls, attrNameFederateHandle)
	h.attrFederateName, _ = fomH.LookupAttribute(h.federateCls, attrNameFederateName)
	h.attrFederateType, _ = fomH.LookupAttribute(h.federateCls, attrNameFederateType)
	if err := m.prepareInternalProducer(ctx, fed); err != nil {
		return err
	}

	// Publish every predefined attribute before registration establishes
	// ownership; participant additions must not become RTI-owned.
	if err := m.fanout.decls.PublishObjectClassAttributes(
		ctx, fed, momProducer, h.federationCls,
		predefinedMOMAttributes(fomH, h.federationCls, federationMetricNames),
	); err != nil {
		return fmt.Errorf("mom: publish HLAfederation attrs: %w", err)
	}
	if err := m.fanout.decls.PublishObjectClassAttributes(
		ctx, fed, momProducer, h.federateCls,
		predefinedMOMAttributes(fomH, h.federateCls, federateMetricNames),
	); err != nil {
		return fmt.Errorf("mom: publish HLAfederate attrs: %w", err)
	}

	// Register the singleton HLAfederation instance through the standard
	// registry path (normal Discover fan-out fires for subscribers).
	h.federationObjName = "HLAfederation." + string(fed)
	obj, _, err := m.fanout.objects.Register(ctx, fed, momProducer, h.federationCls, h.federationObjName)
	if err != nil {
		return fmt.Errorf("mom: register HLAfederation instance: %w", err)
	}
	h.federationObj = obj
	h.ok = true

	m.mu.Lock()
	if st, ok := m.fed[fed]; ok {
		st.objects = h
	}
	m.mu.Unlock()

	// Initial attribute state (standard Reflect to any subscriber).
	return m.updateFederationInstance(ctx, fed)
}

func predefinedMOMAttributes(fom core.FOMHandle, class core.ObjectClassHandle, names string) []core.AttributeHandle {
	var result []core.AttributeHandle
	for _, name := range strings.Fields(names) {
		if attr, ok := fom.LookupAttribute(class, name); ok {
			result = append(result, attr)
		}
	}
	return result
}

// updateFederationInstance re-sends HLAfederationName +
// HLAfederatesInFederation as a standard RO attribute update.
func (m *Manager) updateFederationInstance(ctx context.Context, fed core.FederationName) error {
	m.mu.RLock()
	st, ok := m.fed[fed]
	if !ok || !st.objects.ok {
		m.mu.RUnlock()
		return nil
	}
	h := st.objects
	handles := make([]core.FederateHandle, len(st.federation.federateHandles))
	copy(handles, st.federation.federateHandles)
	m.mu.RUnlock()

	attrs := map[core.AttributeHandle][]byte{}
	if h.attrFederationName != core.InvalidAttributeHandle {
		attrs[h.attrFederationName] = encodeHLAunicodeString(string(fed))
	}
	if h.attrFederatesInFederation != core.InvalidAttributeHandle {
		attrs[h.attrFederatesInFederation] = encodeFederateHandleList(handles)
	}
	if len(attrs) == 0 {
		return nil
	}
	if err := m.fanout.objects.UpdateAttributes(ctx, fed, momProducer, h.federationObj, attrs, nil); err != nil {
		return fmt.Errorf("mom: update HLAfederation attrs: %w", err)
	}
	return nil
}

// --- FederateJoined / FederateResigned fan-out ------------------------------

// registerFederateInstance registers the joining federate's HLAfederate
// instance and reflects its initial attributes, then refreshes the
// federation instance's HLAfederatesInFederation. Called OUTSIDE the
// manager mutex (see setupFederationInstance).
func (m *Manager) registerFederateInstance(
	ctx context.Context,
	fed core.FederationName,
	fh core.FederateHandle,
	name string,
	federateType string,
) error {
	if !m.fanoutEnabled() {
		return nil
	}
	m.mu.RLock()
	st, ok := m.fed[fed]
	var h momClassHandles
	if ok {
		h = st.objects
	}
	m.mu.RUnlock()
	if !ok || !h.ok {
		return nil
	}

	instName := "HLAfederate." + itoa(int(fh))
	obj, _, err := m.fanout.objects.Register(ctx, fed, momProducer, h.federateCls, instName)
	if err != nil {
		return fmt.Errorf("mom: register HLAfederate instance for federate %d: %w", fh, err)
	}

	m.mu.Lock()
	if st, ok := m.fed[fed]; ok {
		if fs, ok := st.federates[fh]; ok {
			fs.objectHandle = obj
			fs.objectName = instName
		}
	}
	m.mu.Unlock()

	attrs := federateAttrValues(h, fh, name, federateType)
	if len(attrs) > 0 {
		if err := m.publishInstanceAttributes(ctx, fed, obj, h.federateCls, attrs); err != nil {
			return fmt.Errorf("mom: update HLAfederate attrs for federate %d: %w", fh, err)
		}
	}
	return m.updateFederationInstance(ctx, fed)
}

func (m *Manager) publishInstanceAttributes(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, cls core.ObjectClassHandle, attrs map[core.AttributeHandle][]byte) error {
	m.mu.RLock()
	publisher := m.fanout.publisher
	var subject core.FederateHandle
	st := m.fed[fed]
	isFederate := st != nil && st.objects.federateCls == cls
	if isFederate && publisher != nil {
		for handle, fs := range st.federates {
			if fs.objectHandle == obj {
				subject = handle
				break
			}
		}
	}
	m.mu.RUnlock()
	if isFederate && publisher != nil {
		if subject == 0 {
			return core.ErrObjectHandleInvalid
		}
		return publisher.PublishFederateAttributes(ctx, fed, subject, obj, cls, attrs)
	}
	return m.fanout.objects.UpdateAttributes(ctx, fed, momProducer, obj, attrs, nil)
}

// removeFederateInstance deletes the resigned federate's HLAfederate
// instance (standard Remove fan-out) and refreshes the federation
// instance's HLAfederatesInFederation. The object handle must be
// captured by the caller BEFORE the snapshot entry is deleted.
func (m *Manager) removeFederateInstance(
	ctx context.Context,
	fed core.FederationName,
	obj core.ObjectHandle,
) error {
	if !m.fanoutEnabled() || obj == core.InvalidObjectHandle {
		return nil
	}
	if err := m.fanout.objects.Delete(ctx, fed, momProducer, obj, nil, nil); err != nil {
		return fmt.Errorf("mom: delete HLAfederate instance %d: %w", obj, err)
	}
	return m.updateFederationInstance(ctx, fed)
}

// federateAttrValues builds the encoded attribute map for one
// HLAfederate instance. Attributes whose handles failed FOM resolution
// are skipped.
func federateAttrValues(
	h momClassHandles,
	fh core.FederateHandle,
	name string,
	federateType string,
) map[core.AttributeHandle][]byte {
	attrs := map[core.AttributeHandle][]byte{}
	if h.attrFederateHandle != core.InvalidAttributeHandle {
		attrs[h.attrFederateHandle] = encodeHLAhandle(fh)
	}
	if h.attrFederateName != core.InvalidAttributeHandle {
		attrs[h.attrFederateName] = encodeHLAunicodeString(name)
	}
	if h.attrFederateType != core.InvalidAttributeHandle {
		attrs[h.attrFederateType] = encodeHLAunicodeString(federateType)
	}
	return attrs
}

// --- Late-subscriber retroactive Discover + Reflect -------------------------

// ObjectClassSubscribed is the declaration-manager post-subscribe hook
// (declaration.Manager.SetOnSubscribeObjectClass). When a federate
// subscribes to HLAmanager.HLAfederate or .HLAfederation AFTER
// instances already exist, it receives a retroactive Discover +
// Reflect pair per existing instance so late subscribers converge on
// the same MOM view a from-the-start subscriber has.
//
// Events are sent directly through the Outbox with MOM-scoped seq
// numbers (same pattern as the HLAreport* ResponseEmitter); the
// registry's per-federation outbound seq is not consumed.
func (m *Manager) ObjectClassSubscribed(
	ctx context.Context,
	fed core.FederationName,
	sub core.FederateHandle,
	cls core.ObjectClassHandle,
	subscribed []core.AttributeHandle,
) {
	if !m.fanoutEnabled() {
		return
	}

	type retroInstance struct {
		obj     core.ObjectHandle
		name    string
		attrs   map[core.AttributeHandle][]byte
		subject core.FederateHandle
	}
	var pending []retroInstance

	m.mu.RLock()
	st, ok := m.fed[fed]
	if !ok || !st.objects.ok {
		m.mu.RUnlock()
		return
	}
	h := st.objects
	publisher := m.fanout.publisher
	switch cls {
	case h.federateCls:
		// Sorted federate-handle order for deterministic delivery.
		for _, fh := range st.federation.federateHandles {
			fs, ok := st.federates[fh]
			if !ok || fs.objectHandle == core.InvalidObjectHandle {
				continue
			}
			pending = append(pending, retroInstance{
				obj:     fs.objectHandle,
				name:    fs.objectName,
				attrs:   federateAttrValues(h, fs.handle, fs.name, fs.federateType),
				subject: fs.handle,
			})
		}
	case h.federationCls:
		handles := make([]core.FederateHandle, len(st.federation.federateHandles))
		copy(handles, st.federation.federateHandles)
		attrs := map[core.AttributeHandle][]byte{}
		if h.attrFederationName != core.InvalidAttributeHandle {
			attrs[h.attrFederationName] = encodeHLAunicodeString(string(fed))
		}
		if h.attrFederatesInFederation != core.InvalidAttributeHandle {
			attrs[h.attrFederatesInFederation] = encodeFederateHandleList(handles)
		}
		pending = append(pending, retroInstance{
			obj:   h.federationObj,
			name:  h.federationObjName,
			attrs: attrs,
		})
	default:
		m.mu.RUnlock()
		return
	}
	m.mu.RUnlock()

	for _, inst := range pending {
		if inst.subject != 0 && publisher != nil {
			values := make(map[core.AttributeHandle][]byte)
			for attr, value := range inst.attrs {
				if len(subscribed) == 0 || slices.Contains(subscribed, attr) {
					values[attr] = append([]byte(nil), value...)
				}
			}
			if len(values) != 0 {
				_ = publisher.ReflectFederateAttributes(ctx, fed, inst.subject, sub, inst.obj, cls, values)
			}
			continue
		}
		discover := &DiscoverEvent{Sequence: atomic.AddUint64(&momSeq, 1), Object: inst.obj, Class: cls, Name: inst.name}
		if err := m.opts.Outbox.Send(ctx, fed, sub, discover); err != nil {
			continue
		}
		if recorder, ok := m.fanout.objects.(core.RTIObjectDiscoveryRecorder); ok {
			if err := recorder.RecordRTIObjectDiscovery(fed, sub, inst.obj); err != nil {
				continue
			}
		}
		if len(inst.attrs) == 0 {
			continue
		}
		values := make(map[core.AttributeHandle][]byte, len(inst.attrs))
		for a, v := range inst.attrs {
			values[a] = append([]byte(nil), v...)
		}
		reflect := &ReflectEvent{Sequence: atomic.AddUint64(&momSeq, 1), Object: inst.obj, Class: cls, Attributes: values}
		_ = m.opts.Outbox.Send(ctx, fed, sub, reflect)
	}
}

// --- Wire encoders (IEEE 1516.2-2010 §4.13) ---------------------------------

// encodeHLAunicodeString encodes s as HLAunicodeString: uint32 big-
// endian code-unit count followed by UTF-16BE code units. Matches the
// cppsdk HLAunicodeString decoder (cppsdk/src/dlc/BasicDataElements.cpp).
func encodeHLAunicodeString(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 4+2*len(units))
	binary.BigEndian.PutUint32(out, uint32(len(units)))
	for i, u := range units {
		binary.BigEndian.PutUint16(out[4+2*i:], u)
	}
	return out
}

// encodeFederateHandleList encodes an HLAvariableArray of HLAhandle:
// uint32 big-endian element count followed by each separately framed
// eight-byte SDK handle, as used for HLAreport* parameters.
func encodeFederateHandleList(handles []core.FederateHandle) []byte {
	out := make([]byte, 4, 4+12*len(handles))
	binary.BigEndian.PutUint32(out, uint32(len(handles)))
	for _, h := range handles {
		out = append(out, encodeHLAhandle(h)...)
	}
	return out
}

// dropInvalidAttrs filters out InvalidAttributeHandle entries (attrs
// whose FOM lookup failed) so declaration publish sets stay clean.
func dropInvalidAttrs(attrs ...core.AttributeHandle) []core.AttributeHandle {
	out := make([]core.AttributeHandle, 0, len(attrs))
	for _, a := range attrs {
		if a != core.InvalidAttributeHandle {
			out = append(out, a)
		}
	}
	return out
}
