package federate

import (
	"fmt"
	"strings"
)

func (f *Federate) rememberPublishedObjectAttributes(classHandle uint64, attributes []uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.publishedObjectAttributes == nil {
		f.publishedObjectAttributes = make(map[uint64]map[uint64]struct{})
	}
	published := f.publishedObjectAttributes[classHandle]
	if published == nil {
		published = make(map[uint64]struct{}, len(attributes))
		f.publishedObjectAttributes[classHandle] = published
	}
	for _, attribute := range attributes {
		published[attribute] = struct{}{}
	}
}

func (f *Federate) rememberPublishedInteraction(classHandle uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.publishedInteractions == nil {
		f.publishedInteractions = make(map[uint64]struct{})
	}
	f.publishedInteractions[classHandle] = struct{}{}
}

func (f *Federate) rememberRegisteredObject(objectHandle, classHandle uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.objectClasses == nil {
		f.objectClasses = make(map[uint64]uint64)
	}
	if f.ownedObjectAttributes == nil {
		f.ownedObjectAttributes = make(map[uint64]map[uint64]struct{})
	}
	f.objectClasses[objectHandle] = classHandle
	published := f.publishedObjectAttributes[classHandle]
	owned := make(map[uint64]struct{}, len(published))
	for attribute := range published {
		owned[attribute] = struct{}{}
	}
	f.ownedObjectAttributes[objectHandle] = owned
}

func (f *Federate) validateQueuedAttributeUpdate(objectHandle uint64, attributes map[uint64][]byte) error {
	if f == nil || f.conn == nil || f.localLRCClosing.Load() {
		return ErrNotJoined
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	classHandle, ok := f.objectClasses[objectHandle]
	if !ok {
		return ErrObjectInstanceNotKnown
	}
	owned := f.ownedObjectAttributes[objectHandle]
	for attribute := range attributes {
		if _, ok := f.handles.attributeName(classHandle, attribute); !ok {
			return fmt.Errorf("%w: %d", ErrAttributeNotDefined, attribute)
		}
		if _, ok := owned[attribute]; !ok {
			return fmt.Errorf("%w: %d", ErrAttributeNotOwned, attribute)
		}
	}
	return nil
}

func (f *Federate) validateQueuedInteraction(classHandle uint64, parameters map[uint64][]byte) error {
	if f == nil || f.conn == nil || f.localLRCClosing.Load() {
		return ErrNotJoined
	}
	className, ok := f.handles.interactionName(classHandle)
	if !ok {
		return fmt.Errorf("%w: %d", ErrInteractionClassNotDefined, classHandle)
	}
	for parameter := range parameters {
		if _, ok := f.handles.parameterName(classHandle, parameter); !ok {
			return fmt.Errorf("%w: %d", ErrInteractionParameterNotDefined, parameter)
		}
	}
	// IEEE MOM requests in the HLAmanager subtree are RTI services and do
	// not require an application publication declaration.
	if strings.HasPrefix(className, "HLAmanager.") {
		return nil
	}
	f.mu.Lock()
	_, published := f.publishedInteractions[classHandle]
	f.mu.Unlock()
	if !published {
		return ErrInteractionClassNotPublished
	}
	return nil
}
