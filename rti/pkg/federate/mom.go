package federate

import (
	"context"
	codec "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/pkg/logicaltime"
	"maps"
)

// MOM query values reflect only fields supplied by the server. A nil time is
// unavailable, not an observed zero. Counters retain their wire width.
type MOMFederationAttributes struct {
	FederationName        string
	FederateHandles       []uint64
	FOMModuleNames        []string
	AttributeValues       map[string][]byte
	UnavailableAttributes map[string]string
}

type MOMFederateAttributes struct {
	Found                                                                    bool
	FederateHandle                                                           uint64
	FederateName, FederateType                                               string
	TimeRegulating, TimeConstrained                                          bool
	LogicalTime, Lookahead                                                   *float64
	LogicalTimeValue                                                         *logicaltime.Time
	LookaheadValue                                                           *logicaltime.Interval
	InteractionsSent, InteractionsReceived, UpdatesSent, ReflectionsReceived uint32
	AttributeValues                                                          map[string][]byte
	UnavailableAttributes                                                    map[string]string
}

type MOMInstance struct {
	ClassName      string
	FederateHandle uint64
	InstanceName   string
}

func (f *Federate) momClient() (rtiv1.MomServiceClient, error) {
	if f == nil || f.conn == nil || f.conn.cc == nil || f.CurrentHandle() == 0 || f.localLRCClosing.Load() {
		return nil, ErrNotJoined
	}
	return rtiv1.NewMomServiceClient(f.conn.rpcConnection()), nil
}

func (f *Federate) QueryFederationAttributes(ctx context.Context) (MOMFederationAttributes, error) {
	c, err := f.momClient()
	if err != nil {
		return MOMFederationAttributes{}, err
	}
	r, err := c.QueryFederationAttributes(f.sessionContext(ctx), &rtiv1.QueryFederationAttributesRequest{WireVersion: 1, FederationName: f.federationName})
	if err != nil {
		return MOMFederationAttributes{}, wrapStatusErr(err)
	}
	return MOMFederationAttributes{FederationName: r.GetFederationName(), FederateHandles: append([]uint64(nil), r.GetFederateHandles()...), FOMModuleNames: append([]string(nil), r.GetFomModuleNames()...), AttributeValues: cloneMOMAttributeValues(r.GetAttributeValues()), UnavailableAttributes: maps.Clone(r.GetUnavailableAttributes())}, nil
}

func (f *Federate) QueryFederateAttributes(ctx context.Context, target uint64) (MOMFederateAttributes, error) {
	c, err := f.momClient()
	if err != nil {
		return MOMFederateAttributes{}, err
	}
	r, err := c.QueryFederateAttributes(f.sessionContext(ctx), &rtiv1.QueryFederateAttributesRequest{WireVersion: 1, FederationName: f.federationName, FederateHandle: target})
	if err != nil {
		return MOMFederateAttributes{}, wrapStatusErr(err)
	}
	out := MOMFederateAttributes{Found: r.GetFound(), FederateHandle: r.GetFederateHandle(), FederateName: r.GetFederateName(), FederateType: r.GetFederateType(), TimeRegulating: r.GetTimeRegulating(), TimeConstrained: r.GetTimeConstrained(), InteractionsSent: r.GetInteractionsSent(), InteractionsReceived: r.GetInteractionsReceived(), UpdatesSent: r.GetUpdatesSent(), ReflectionsReceived: r.GetReflectionsReceived()}
	out.AttributeValues = cloneMOMAttributeValues(r.GetAttributeValues())
	out.UnavailableAttributes = maps.Clone(r.GetUnavailableAttributes())
	if r.LogicalTime != nil {
		if f.legacyFinalTime(r.LogicalTime.Value, r.LogicalTime.LogicalTimeValue) {
			legacy := r.LogicalTime.Value
			out.LogicalTime = &legacy
		} else {
			value, decodeErr := codec.DecodeLogicalTimePair(f.timeRepresentation(), r.LogicalTime.Value, r.LogicalTime.LogicalTimeValue)
			if decodeErr != nil {
				return MOMFederateAttributes{}, decodeErr
			}
			out.LogicalTimeValue = &value
			if f.timeRepresentation() == logicaltime.HLAfloat64Time {
				legacy, _ := value.Float64()
				out.LogicalTime = &legacy
			}
		}
	}
	if r.Lookahead != nil {
		value, decodeErr := codec.DecodeLogicalTimeIntervalPair(f.timeRepresentation(), r.Lookahead.Value, r.Lookahead.LogicalTimeValue)
		if decodeErr != nil {
			return MOMFederateAttributes{}, decodeErr
		}
		out.LookaheadValue = &value
		if f.timeRepresentation() == logicaltime.HLAfloat64Time {
			legacy, _ := value.Float64()
			out.Lookahead = &legacy
		}
	}
	return out, nil
}

func cloneMOMAttributeValues(input map[string][]byte) map[string][]byte {
	if input == nil {
		return nil
	}
	out := make(map[string][]byte, len(input))
	for name, value := range input {
		out[name] = append([]byte(nil), value...)
	}
	return out
}

func (f *Federate) EnumerateMOMInstances(ctx context.Context) ([]MOMInstance, error) {
	c, err := f.momClient()
	if err != nil {
		return nil, err
	}
	r, err := c.EnumerateMomInstances(f.sessionContext(ctx), &rtiv1.EnumerateMomInstancesRequest{WireVersion: 1, FederationName: f.federationName})
	if err != nil {
		return nil, wrapStatusErr(err)
	}
	out := make([]MOMInstance, 0, len(r.GetInstances()))
	for _, i := range r.GetInstances() {
		out = append(out, MOMInstance{ClassName: i.GetClassName(), FederateHandle: i.GetFederateHandle(), InstanceName: i.GetInstanceName()})
	}
	return out, nil
}
