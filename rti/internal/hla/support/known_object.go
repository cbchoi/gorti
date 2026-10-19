package support

import "github.com/cbchoi/gorti/rti/internal/hla/core"

type knownObjectInstances interface {
	LookupKnownObjectInstance(core.FederationName, core.FederateHandle, core.ObjectHandle) (string, core.ObjectClassHandle, bool)
	LookupKnownObjectInstanceByName(core.FederationName, core.FederateHandle, string) (core.ObjectHandle, bool)
}

func (s *Service) KnownObject(fed core.FederationName, caller core.FederateHandle, obj core.ObjectHandle) (string, core.ObjectClassHandle, bool) {
	q, ok := s.instances.(knownObjectInstances)
	if !ok {
		return "", 0, false
	}
	return q.LookupKnownObjectInstance(fed, caller, obj)
}

func (s *Service) KnownObjectHandle(fed core.FederationName, caller core.FederateHandle, name string) (core.ObjectHandle, bool) {
	q, ok := s.instances.(knownObjectInstances)
	if !ok {
		return 0, false
	}
	return q.LookupKnownObjectInstanceByName(fed, caller, name)
}
