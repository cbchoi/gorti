// Package support implements transport-neutral IEEE 1516 support-service
// lookups over the federation object model and live object instances.
package support

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

var (
	ErrFederationNameRequired = errors.New("support: federation name is required")
	ErrNameLookupUnavailable  = errors.New("support: FOM handle does not provide name lookup")
)

type Service struct {
	foms      core.FOMRepository
	instances core.ObjectInstanceQuery
}

func New(foms core.FOMRepository, instances core.ObjectInstanceQuery) *Service {
	return &Service{foms: foms, instances: instances}
}

func (s *Service) Lookup(ctx context.Context, fed core.FederationName) (core.FOMHandle, core.FOMHandleNameLookup, error) {
	if strings.TrimSpace(string(fed)) == "" {
		return nil, nil, ErrFederationNameRequired
	}
	handle, err := s.foms.Get(ctx, fed)
	if err != nil {
		return nil, nil, fmt.Errorf("support: federation %q: %w", fed, err)
	}
	names, ok := handle.(core.FOMHandleNameLookup)
	if !ok {
		return nil, nil, ErrNameLookupUnavailable
	}
	return handle, names, nil
}

func (s *Service) ObjectInstanceHandle(fed core.FederationName, name string) (core.ObjectHandle, bool) {
	if s.instances == nil {
		return core.InvalidObjectHandle, false
	}
	return s.instances.LookupObjectInstanceByName(fed, name)
}

func (s *Service) ObjectInstanceName(fed core.FederationName, handle core.ObjectHandle) (string, bool) {
	if s.instances == nil {
		return "", false
	}
	return s.instances.LookupObjectInstanceName(fed, handle)
}

func (s *Service) HasObjectInstances() bool { return s.instances != nil }
