//go:build dds

// Package dds — Topic interface + Phase 1a stub.
//
// docs/m19-dds-adapter.md §2.3 PINNED: each interaction-class topic
// is named gorti/<federation>/interaction/<class_handle>; each
// object-class attribute topic is gorti/<federation>/object/
// <class_handle>/<attribute_handle>. The Topic interface here owns
// just the lifecycle — name + QoS resolution lives in the federation
// runtime that constructs the Topic.

package dds

import (
	"errors"
)

// Topic is the gorti-side handle for a Cyclone DDS Topic. One Topic
// per (federation, interaction class) or per (federation, object
// class, attribute) per docs/m19-dds-adapter.md §2.3.
//
// Phase 1b adds CGo-backed lifecycle. Phase 1a is the foundation
// only — every method returns errors.ErrUnsupported.
type Topic interface {
	// Name returns the DDS topic name the publisher / subscriber
	// will see on the wire. Computed at construction time; safe
	// to call concurrently.
	Name() string

	// QoS returns the QoS the topic was created with. Used by
	// CreateWriter / CreateReader to propagate the matching QoS
	// onto the data endpoints.
	QoS() QoS

	// CreateWriter returns a Writer attached to this topic. Phase
	// 1a stub returns ErrUnsupported.
	CreateWriter() (Writer, error)

	// CreateReader returns a Reader attached to this topic. Phase
	// 1a stub returns ErrUnsupported.
	CreateReader() (Reader, error)

	// Close releases the topic + every writer/reader the topic
	// owns. Idempotent. Phase 1a stub returns ErrUnsupported on
	// first call.
	Close() error
}

// defaultTopic is the Phase 1a stub. Holds the (name, QoS) pair so
// stub-contract tests can verify Phase 1a doesn't lose the
// configuration even though the lifecycle is unimplemented.
type defaultTopic struct {
	name string
	qos  QoS
}

func (t *defaultTopic) Name() string                  { return t.name }
func (t *defaultTopic) QoS() QoS                      { return t.qos }
func (*defaultTopic) CreateWriter() (Writer, error)   { return nil, errors.ErrUnsupported }
func (*defaultTopic) CreateReader() (Reader, error)   { return nil, errors.ErrUnsupported }
func (*defaultTopic) Close() error                    { return errors.ErrUnsupported }
