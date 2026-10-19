package time

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// RetractionIDRange is an inclusive range of accepted originating IDs.
type RetractionIDRange struct {
	First uint64 `json:"first"`
	Last  uint64 `json:"last"`
}

type RetractionProducerHistoryOf[T core.LogicalTimeScalar] struct {
	Producer core.FederateHandle           `json:"producer"`
	Accepted []RetractionIDRange           `json:"accepted"`
	Messages []RetractionMessageStateOf[T] `json:"messages,omitempty"`
}

// RetractionMessageState describes one originating TSO message, including
// recipients that have already received a speculative or receive-order copy.
type RetractionMessageStateOf[T core.LogicalTimeScalar] struct {
	ID        uint64                `json:"id"`
	Timestamp T                     `json:"timestamp"`
	Delivered []core.FederateHandle `json:"delivered,omitempty"`
	Retracted bool                  `json:"retracted,omitempty"`
}

// Complete distinguishes proven empty history from an older missing ledger.
type RetractionHistoryOf[T core.LogicalTimeScalar] struct {
	Complete  bool                             `json:"complete"`
	Producers []RetractionProducerHistoryOf[T] `json:"producers"`
}

type retractionIDStoreOf[T core.LogicalTimeScalar] struct {
	// No service callbacks or other manager locks are acquired under mu.
	mu          sync.Mutex
	federations map[core.FederationName]*retractionFederationOf[T]
}

type retractionFederationOf[T core.LogicalTimeScalar] struct {
	producers map[core.FederateHandle]*retractionProducerOf[T]
}

type retractionProducerOf[T core.LogicalTimeScalar] struct {
	accepted []RetractionIDRange
	pending  map[uint64]*retractionIDReservationOf[T]
	messages map[uint64]*RetractionMessageStateOf[T]
}

func (s *retractionIDStoreOf[T]) federationLocked(fed core.FederationName) *retractionFederationOf[T] {
	if s.federations == nil {
		s.federations = make(map[core.FederationName]*retractionFederationOf[T])
	}
	f := s.federations[fed]
	if f == nil {
		f = &retractionFederationOf[T]{producers: make(map[core.FederateHandle]*retractionProducerOf[T])}
		s.federations[fed] = f
	}
	return f
}

func (f *retractionFederationOf[T]) producerLocked(h core.FederateHandle) *retractionProducerOf[T] {
	p := f.producers[h]
	if p == nil {
		p = &retractionProducerOf[T]{pending: make(map[uint64]*retractionIDReservationOf[T]), messages: make(map[uint64]*RetractionMessageStateOf[T])}
		f.producers[h] = p
	}
	return p
}

// ReserveRetractionID is called once before any originating WAL or delivery
// effects. It never depends on recipient count or TSO buffering. The caller
// pins membership/checkpoint admission through Commit or Release.
func (m *ManagerOf[T]) ReserveRetractionID(ctx context.Context, fed core.FederationName, producer core.FederateHandle, id uint64) (core.OutboxReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if producer == 0 || id == 0 {
		return nil, fmt.Errorf("retraction admission requires nonzero producer and ID")
	}
	s := &m.retractions
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.federationLocked(fed)
	p := f.producerLocked(producer)
	if containsRetractionID(p.accepted, id) || p.pending[id] != nil {
		return nil, core.ErrRetractionIDAlreadyUsed
	}
	r := &retractionIDReservationOf[T]{store: s, fed: fed, federation: f, producer: p, id: id}
	p.pending[id] = r
	return r, nil
}

type retractionIDReservationOf[T core.LogicalTimeScalar] struct {
	store      *retractionIDStoreOf[T]
	fed        core.FederationName
	federation *retractionFederationOf[T]
	producer   *retractionProducerOf[T]
	id         uint64
	done       bool
	err        error
	message    *RetractionMessageStateOf[T]
}

func (r *retractionIDReservationOf[T]) Commit() error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if r.done {
		return r.err
	}
	r.done = true
	if r.store.federations[r.fed] != r.federation || r.producer.pending[r.id] != r {
		r.err = core.ErrRetractionHistoryUnavailable
		return r.err
	}
	delete(r.producer.pending, r.id)
	r.producer.accepted = unionRetractionRanges(r.producer.accepted, []RetractionIDRange{{First: r.id, Last: r.id}})
	if r.message != nil {
		r.producer.messages[r.id] = r.message
	}
	return nil
}

func (r *retractionIDReservationOf[T]) Release() {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	r.err = context.Canceled
	if r.store.federations[r.fed] == r.federation && r.producer.pending[r.id] == r {
		delete(r.producer.pending, r.id)
	}
}

func containsRetractionID(ranges []RetractionIDRange, id uint64) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].Last >= id })
	return i < len(ranges) && ranges[i].First <= id
}

func unionRetractionRanges(a, b []RetractionIDRange) []RetractionIDRange {
	all := append(slices.Clone(a), b...)
	slices.SortFunc(all, func(a, b RetractionIDRange) int {
		if a.First < b.First {
			return -1
		}
		if a.First > b.First {
			return 1
		}
		return 0
	})
	result := all[:0]
	for _, next := range all {
		if len(result) == 0 {
			result = append(result, next)
			continue
		}
		last := &result[len(result)-1]
		if next.First <= last.Last || (last.Last != math.MaxUint64 && next.First == last.Last+1) {
			if next.Last > last.Last {
				last.Last = next.Last
			}
		} else {
			result = append(result, next)
		}
	}
	return result
}

// ExportRetractionHistory requires global checkpoint admission. In-flight
// originating operations are rejected instead of producing an incomplete image.
func (m *ManagerOf[T]) ExportRetractionHistory(fed core.FederationName) (*RetractionHistoryOf[T], error) {
	s := &m.retractions
	s.mu.Lock()
	defer s.mu.Unlock()
	history := &RetractionHistoryOf[T]{Complete: true}
	f := s.federations[fed]
	if f == nil {
		return history, nil
	}
	for h, p := range f.producers {
		if len(p.pending) != 0 {
			return nil, fmt.Errorf("%w: originating operations pending", core.ErrRetractionHistoryUnavailable)
		}
		if len(p.accepted) > 0 {
			entry := RetractionProducerHistoryOf[T]{Producer: h, Accepted: slices.Clone(p.accepted)}
			for _, message := range p.messages {
				entry.Messages = append(entry.Messages, cloneRetractionMessageOf[T](*message))
			}
			slices.SortFunc(entry.Messages, func(a, b RetractionMessageStateOf[T]) int { return compareRetractionIDs(a.ID, b.ID) })
			history.Producers = append(history.Producers, entry)
		}
	}
	slices.SortFunc(history.Producers, func(a, b RetractionProducerHistoryOf[T]) int {
		if a.Producer < b.Producer {
			return -1
		}
		if a.Producer > b.Producer {
			return 1
		}
		return 0
	})
	return history, nil
}

func ValidateRetractionHistoryOf[T core.LogicalTimeScalar](history *RetractionHistoryOf[T]) error {
	if history == nil || !history.Complete {
		return core.ErrRetractionHistoryUnavailable
	}
	seen := make(map[core.FederateHandle]bool, len(history.Producers))
	for _, p := range history.Producers {
		if p.Producer == 0 || seen[p.Producer] {
			return fmt.Errorf("retraction history has zero or duplicate producer")
		}
		seen[p.Producer] = true
		if err := validateRetractionProducer(p); err != nil {
			return err
		}
	}
	return nil
}

// PrepareRetractionHistory detaches a complete image. Commit unions it with the
// live ledger, including IDs accepted after the checkpoint or before resign.
// Global checkpoint admission prevents sends and destruction across this call
// and its commit. The ordinary time-state restore never rewinds this ledger.
func (m *ManagerOf[T]) PrepareRetractionHistory(fed core.FederationName, history *RetractionHistoryOf[T]) (core.PreparedRestore, error) {
	if err := ValidateRetractionHistoryOf[T](history); err != nil {
		return nil, err
	}
	copyHistory := make([]RetractionProducerHistoryOf[T], len(history.Producers))
	for i, p := range history.Producers {
		copyHistory[i] = RetractionProducerHistoryOf[T]{Producer: p.Producer, Accepted: slices.Clone(p.Accepted)}
		for _, message := range p.Messages {
			copyHistory[i].Messages = append(copyHistory[i].Messages, cloneRetractionMessageOf[T](message))
		}
	}
	m.retractions.mu.Lock()
	defer m.retractions.mu.Unlock()
	f := m.retractions.federationLocked(fed)
	for _, p := range f.producers {
		if len(p.pending) != 0 {
			return nil, core.ErrRetractionHistoryUnavailable
		}
	}
	return &preparedRetractionHistoryOf[T]{store: &m.retractions, fed: fed, federation: f, saved: copyHistory}, nil
}

type preparedRetractionHistoryOf[T core.LogicalTimeScalar] struct {
	store      *retractionIDStoreOf[T]
	fed        core.FederationName
	federation *retractionFederationOf[T]
	saved      []RetractionProducerHistoryOf[T]
	done       bool
	err        error
}

func (r *preparedRetractionHistoryOf[T]) Commit() error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if r.done {
		return r.err
	}
	r.done = true
	if r.store.federations[r.fed] != r.federation {
		r.err = core.ErrRetractionHistoryUnavailable
		return r.err
	}
	for _, p := range r.federation.producers {
		if len(p.pending) != 0 {
			r.err = core.ErrRetractionHistoryUnavailable
			return r.err
		}
	}
	for _, saved := range r.saved {
		p := r.federation.producerLocked(saved.Producer)
		p.accepted = unionRetractionRanges(p.accepted, saved.Accepted)
	}
	// IDs never rewind, but outstanding messages and prior callback recipients
	// are part of the saved execution state and must rewind together with queues.
	for _, p := range r.federation.producers {
		p.messages = make(map[uint64]*RetractionMessageStateOf[T])
	}
	for _, saved := range r.saved {
		p := r.federation.producerLocked(saved.Producer)
		for _, message := range saved.Messages {
			messageCopy := cloneRetractionMessageOf[T](message)
			p.messages[messageCopy.ID] = &messageCopy
		}
	}
	return nil
}

func (r *preparedRetractionHistoryOf[T]) Release() {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if !r.done {
		r.done = true
		r.err = context.Canceled
	}
}

// PreviewNextRetractionHandle returns an allocation hint, not a minimum legal
// caller ID. Any unused nonzero ID remains admissible, including smaller gaps.
func (m *ManagerOf[T]) PreviewNextRetractionHandle(fed core.FederationName, history *RetractionHistoryOf[T], producer core.FederateHandle) (uint64, error) {
	if err := ValidateRetractionHistoryOf[T](history); err != nil {
		return 0, err
	}
	if producer == 0 {
		return 0, fmt.Errorf("retraction history requires nonzero producer")
	}
	var maximum uint64
	for _, p := range history.Producers {
		if p.Producer == producer && len(p.Accepted) > 0 {
			maximum = p.Accepted[len(p.Accepted)-1].Last
		}
	}
	m.retractions.mu.Lock()
	defer m.retractions.mu.Unlock()
	if f := m.retractions.federations[fed]; f != nil {
		if p := f.producers[producer]; p != nil {
			if len(p.pending) != 0 {
				return 0, core.ErrRetractionHistoryUnavailable
			}
			if len(p.accepted) > 0 && p.accepted[len(p.accepted)-1].Last > maximum {
				maximum = p.accepted[len(p.accepted)-1].Last
			}
		}
	}
	if maximum == math.MaxUint64 {
		return 0, core.ErrRetractionIDExhausted
	}
	return maximum + 1, nil
}

var _ core.RetractionIDAdmission = (*ManagerOf[core.LogicalTime])(nil)

func validateRetractionProducer[T core.LogicalTimeScalar](p RetractionProducerHistoryOf[T]) error {
	var previous uint64
	for i, r := range p.Accepted {
		if r.First == 0 || r.Last < r.First || (i > 0 && (previous == math.MaxUint64 || r.First <= previous+1)) {
			return fmt.Errorf("retraction history producer %d has invalid or noncanonical ranges", p.Producer)
		}
		previous = r.Last
	}
	seenMessages := make(map[uint64]bool, len(p.Messages))
	for _, message := range p.Messages {
		if message.ID == 0 || seenMessages[message.ID] || !containsRetractionID(p.Accepted, message.ID) || !validPersistentTimeOf[T](message.Timestamp) {
			return fmt.Errorf("retraction history producer %d has invalid message", p.Producer)
		}
		seenMessages[message.ID] = true
		var prior core.FederateHandle
		for _, recipient := range message.Delivered {
			if recipient == 0 || recipient <= prior {
				return fmt.Errorf("retraction history has invalid or duplicate recipient")
			}
			prior = recipient
		}
	}
	return nil
}
