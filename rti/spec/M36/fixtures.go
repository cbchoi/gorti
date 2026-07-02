package m36spec

import (
	"context"
	"encoding/binary"
	"sync"
	"unicode/utf16"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
)

// Class / attribute handles mirroring the merged standard-MIM layout the
// production fomHandle produces for the mom_federation_lifecycle fixture
// FOM (classes name-sorted: HLAfederate=1, HLAfederation=2; attributes in
// MIM declaration order).
const (
	clsHLAfederate   = core.ObjectClassHandle(1)
	clsHLAfederation = core.ObjectClassHandle(2)

	attrFederateHandle = core.AttributeHandle(1)
	attrFederateName   = core.AttributeHandle(2)
	attrFederateType   = core.AttributeHandle(3)

	attrFederationName        = core.AttributeHandle(1)
	attrFederatesInFederation = core.AttributeHandle(2)
)

// momFOMHandle is a core.FOMHandle answering the MOM class/attribute
// lookups with the merged-MIM handle layout above.
type momFOMHandle struct{}

func (momFOMHandle) IsValid() bool { return true }

func (momFOMHandle) LookupObjectClass(name string) (core.ObjectClassHandle, bool) {
	switch name {
	case "HLAobjectRoot.HLAmanager.HLAfederate", "HLAfederate":
		return clsHLAfederate, true
	case "HLAobjectRoot.HLAmanager.HLAfederation", "HLAfederation":
		return clsHLAfederation, true
	}
	return core.InvalidObjectClassHandle, false
}

func (momFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return core.InvalidInteractionClassHandle, false
}

func (momFOMHandle) LookupAttribute(cls core.ObjectClassHandle, name string) (core.AttributeHandle, bool) {
	switch cls {
	case clsHLAfederate:
		switch name {
		case "HLAfederateHandle":
			return attrFederateHandle, true
		case "HLAfederateName":
			return attrFederateName, true
		case "HLAfederateType":
			return attrFederateType, true
		}
	case clsHLAfederation:
		switch name {
		case "HLAfederationName":
			return attrFederationName, true
		case "HLAfederatesInFederation":
			return attrFederatesInFederation, true
		}
	}
	return core.InvalidAttributeHandle, false
}

func (momFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return core.InvalidParameterHandle, false
}

// momFOMRepo returns the momFOMHandle for every federation.
type momFOMRepo struct{}

func (momFOMRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return momFOMHandle{}, nil
}

func (momFOMRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return momFOMHandle{}, nil
}

// fakeOutbox records every Send for assertion. Goroutine-safe.
type sentRecord struct {
	Federation core.FederationName
	Federate   core.FederateHandle
	Event      core.OutboundEvent
}

type fakeOutbox struct {
	mu   sync.Mutex
	sent []sentRecord
}

func newFakeOutbox() *fakeOutbox { return &fakeOutbox{} }

func (o *fakeOutbox) Send(_ context.Context, fed core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, sentRecord{fed, h, evt})
	return nil
}

func (o *fakeOutbox) Sent() []sentRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]sentRecord, len(o.sent))
	copy(out, o.sent)
	return out
}

// SentTo filters the recorded sends down to one recipient federate and
// unwraps the inner FederateEvent protos.
func (o *fakeOutbox) SentTo(h core.FederateHandle) []*rtiv1.FederateEvent {
	var out []*rtiv1.FederateEvent
	for _, rec := range o.Sent() {
		if rec.Federate != h {
			continue
		}
		if carrier, ok := rec.Event.(interface{ Inner() *rtiv1.FederateEvent }); ok {
			out = append(out, carrier.Inner())
		}
	}
	return out
}

// decodeHLAunicodeString is the inverse of the mom package's encoder:
// uint32 big-endian code-unit count + UTF-16BE code units.
func decodeHLAunicodeString(b []byte) (string, bool) {
	if len(b) < 4 {
		return "", false
	}
	n := int(binary.BigEndian.Uint32(b))
	if len(b) < 4+2*n {
		return "", false
	}
	units := make([]uint16, n)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(b[4+2*i:])
	}
	return string(utf16.Decode(units)), true
}
