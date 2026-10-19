package core

// FederationJoinedJournalRecord is the transport-neutral lifecycle record
// shared by the federation service and journal adapters.
type FederationJoinedJournalRecord struct {
	Sequence             uint64
	WallNS               uint64
	FederateName         string
	FederateHandle       FederateHandle
	FederateType         string
	AdditionalFOMModules []FOMModule
	FOMRevision          uint64
	FOMDigest            string
}

func (e *FederationJoinedJournalRecord) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}

func (e *FederationJoinedJournalRecord) SetSeq(seq uint64) { e.Sequence = seq }

// FederationResignedJournalRecord records the entire resign action as one
// lifecycle mutation. ResignAction must be a concrete, supported action.
type FederationResignedJournalRecord struct {
	Sequence       uint64
	WallNS         uint64
	FederateHandle FederateHandle
	ResignAction   ResignAction
}

func (e *FederationResignedJournalRecord) Seq() uint64 {
	if e == nil {
		return 0
	}
	return e.Sequence
}

func (e *FederationResignedJournalRecord) SetSeq(seq uint64) { e.Sequence = seq }

var (
	_ EventRecord = (*FederationJoinedJournalRecord)(nil)
	_ EventRecord = (*FederationResignedJournalRecord)(nil)
)
