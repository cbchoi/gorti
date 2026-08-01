package federate

import (
	"errors"
	"testing"
)

func testLocalStateTables() *fomTables {
	return &fomTables{
		objectByHandle:      map[uint64]string{10: "Vehicle"},
		attrByHandle:        map[uint64]map[uint64]string{10: {2: "Position", 3: "Speed"}},
		interactionByHandle: map[uint64]string{12: "Move", 13: "HLAmanager.HLAfederate.HLArequest.HLArequestPublications"},
		paramByHandle: map[uint64]map[uint64]string{
			12: {3: "Sequence", 4: "Payload", 5: "X"},
			13: {},
		},
	}
}

func TestLocalStateMirrorsPublicationAndRegistrationOwnership(t *testing.T) {
	fed := &Federate{handles: testLocalStateTables()}
	fed.rememberPublishedObjectAttributes(10, []uint64{2})
	fed.rememberPublishedObjectAttributes(10, []uint64{3})
	fed.rememberRegisteredObject(11, 10)

	if err := fed.validateQueuedAttributeUpdate(11, map[uint64][]byte{2: nil, 3: nil}); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("validation without connection = %v, want ErrNotJoined", err)
	}

	// Admission tests use a non-nil connection as the local joined marker.
	fed.conn = &Connection{}
	if err := fed.validateQueuedAttributeUpdate(11, map[uint64][]byte{2: nil, 3: nil}); err != nil {
		t.Fatalf("validation of owned attributes: %v", err)
	}
	if err := fed.validateQueuedAttributeUpdate(11, map[uint64][]byte{4: nil}); !errors.Is(err, ErrAttributeNotDefined) {
		t.Fatalf("undefined attribute error = %v", err)
	}
}

func TestLocalStateInteractionPublicationValidation(t *testing.T) {
	fed := &Federate{conn: &Connection{}, handles: testLocalStateTables()}
	if err := fed.validateQueuedInteraction(12, map[uint64][]byte{5: nil}); !errors.Is(err, ErrInteractionClassNotPublished) {
		t.Fatalf("unpublished interaction error = %v", err)
	}
	fed.rememberPublishedInteraction(12)
	if err := fed.validateQueuedInteraction(12, map[uint64][]byte{5: nil}); err != nil {
		t.Fatalf("published interaction validation: %v", err)
	}
	if err := fed.validateQueuedInteraction(12, map[uint64][]byte{6: nil}); !errors.Is(err, ErrInteractionParameterNotDefined) {
		t.Fatalf("undefined parameter error = %v", err)
	}
}

func TestLocalStateAllowsManagementInteractionWithoutPublication(t *testing.T) {
	fed := &Federate{conn: &Connection{}, handles: testLocalStateTables()}
	if err := fed.validateQueuedInteraction(13, nil); err != nil {
		t.Fatalf("management interaction validation: %v", err)
	}
}

func TestLocalStateDiscoveredObjectDoesNotGainOwnership(t *testing.T) {
	fed := &Federate{conn: &Connection{}, handles: testLocalStateTables()}
	fed.rememberPublishedObjectAttributes(10, []uint64{2})
	fed.rememberObjectClass(11, 10)
	if err := fed.validateQueuedAttributeUpdate(11, map[uint64][]byte{2: nil}); !errors.Is(err, ErrAttributeNotOwned) {
		t.Fatalf("discovered object update error = %v, want ErrAttributeNotOwned", err)
	}
}

func BenchmarkValidateQueuedInteraction(b *testing.B) {
	fed := &Federate{conn: &Connection{}, handles: testLocalStateTables()}
	fed.rememberPublishedInteraction(12)
	parameters := map[uint64][]byte{5: nil}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := fed.validateQueuedInteraction(12, parameters); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateQueuedInteractionParallel(b *testing.B) {
	fed := &Federate{conn: &Connection{}, handles: testLocalStateTables()}
	fed.rememberPublishedInteraction(12)
	parameters := map[uint64][]byte{5: nil}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := fed.validateQueuedInteraction(12, parameters); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func BenchmarkValidateQueuedAttributeUpdate(b *testing.B) {
	fed := &Federate{conn: &Connection{}, handles: testLocalStateTables()}
	fed.rememberPublishedObjectAttributes(10, []uint64{2, 3})
	fed.rememberRegisteredObject(11, 10)
	attributes := map[uint64][]byte{2: nil, 3: nil}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := fed.validateQueuedAttributeUpdate(11, attributes); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateQueuedAttributeUpdateParallel(b *testing.B) {
	fed := &Federate{conn: &Connection{}, handles: testLocalStateTables()}
	fed.rememberPublishedObjectAttributes(10, []uint64{2, 3})
	fed.rememberRegisteredObject(11, 10)
	attributes := map[uint64][]byte{2: nil, 3: nil}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := fed.validateQueuedAttributeUpdate(11, attributes); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
