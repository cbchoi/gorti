// Package dds — QoS mapping. Pure Go, no CGo. Phase 1b's CGo interop
// will translate this QoS value object into the Cyclone DDS API calls
// (dds_qos_create / dds_qset_reliability / dds_qset_history /
// dds_qset_destination_order) without revisiting the design.

package dds

// Reliability mirrors the DDS Reliability QoS axis. We expose only
// the two values the four core HLA combos need; the wider DDS axis
// (RELIABLE with custom max-blocking-time, etc.) is a Phase 3+
// addition gated behind explicit FOM tags.
type Reliability int

const (
	// ReliabilityBestEffort maps HLAbestEffort transportation. DDS
	// BEST_EFFORT is the lighter wire path — no acks, no retries.
	ReliabilityBestEffort Reliability = 1
	// ReliabilityReliable maps HLAreliable transportation. DDS
	// RELIABLE adds acks + retransmits within a writer-side
	// max-blocking-time budget.
	ReliabilityReliable Reliability = 2
)

// String returns a stable diagnostic string for logging + tests.
func (r Reliability) String() string {
	switch r {
	case ReliabilityBestEffort:
		return "best-effort"
	case ReliabilityReliable:
		return "reliable"
	default:
		return "reliability(unknown)"
	}
}

// HistoryKind mirrors the DDS History QoS axis (KEEP_ALL vs KEEP_LAST).
type HistoryKind int

const (
	// HistoryKeepAll keeps every sample until the resource limits
	// kick in. Pairs with Reliable for the lossless wire path.
	HistoryKeepAll HistoryKind = 1
	// HistoryKeepLast keeps only the last N samples (depth carried
	// in History.Depth). Pairs with BestEffort for the
	// "freshest-wins" semantic federates already get over the
	// gRPC stream's latest-only fanout.
	HistoryKeepLast HistoryKind = 2
)

// History is the DDS history QoS sub-bundle.
type History struct {
	Kind  HistoryKind
	Depth int // only meaningful when Kind == HistoryKeepLast
}

// String returns a stable diagnostic string.
func (h History) String() string {
	switch h.Kind {
	case HistoryKeepAll:
		return "keep-all"
	case HistoryKeepLast:
		// Phase 1a: depth is always 1 for the four core combos.
		// A future depth-tunable surface will widen this string.
		return "keep-last(" + itoa(h.Depth) + ")"
	default:
		return "history(unknown)"
	}
}

// DestinationOrder mirrors the DDS DESTINATION_ORDER QoS axis (the
// receiver-side ordering policy).
type DestinationOrder int

const (
	// DestinationOrderByReceptionTimestamp maps HLA Receive order.
	// DDS sorts samples by the receiver-local arrival timestamp.
	DestinationOrderByReceptionTimestamp DestinationOrder = 1
	// DestinationOrderBySourceTimestamp maps HLA TimeStamp order.
	// DDS sorts by the writer-stamped source timestamp; gorti's
	// federate stamps each payload with the HLA logical time so
	// downstream readers see TSO-ordered delivery.
	DestinationOrderBySourceTimestamp DestinationOrder = 2
)

// String returns a stable diagnostic string.
func (d DestinationOrder) String() string {
	switch d {
	case DestinationOrderByReceptionTimestamp:
		return "by-reception-timestamp"
	case DestinationOrderBySourceTimestamp:
		return "by-source-timestamp"
	default:
		return "destination-order(unknown)"
	}
}

// QoS is the value object produced by HLA→DDS QoS mapping. It captures
// the four core HLA QoS combinations that Phase 1 ships
// (docs/m19-dds-adapter.md §2.4):
//
//   - HLAreliable + TimeStamp → RELIABLE + KEEP_ALL + BY_SOURCE_TIMESTAMP
//   - HLAreliable + Receive   → RELIABLE + KEEP_ALL + BY_RECEPTION_TIMESTAMP
//   - HLAbestEffort + TimeStamp → BEST_EFFORT + KEEP_LAST(1) + BY_SOURCE_TIMESTAMP
//   - HLAbestEffort + Receive   → BEST_EFFORT + KEEP_LAST(1) + BY_RECEPTION_TIMESTAMP
//
// Phase 3+ additions (per-class deadlines, ownership strength, custom
// history depth) are out of scope for the four core combos.
type QoS struct {
	Reliability      Reliability
	History          History
	DestinationOrder DestinationOrder
}

// String returns a stable single-line diagnostic for logging + tests.
func (q QoS) String() string {
	return q.Reliability.String() + "/" + q.History.String() + "/" + q.DestinationOrder.String()
}

// Transportation is the HLA-side transportation classifier this
// package consumes. Mirrors the IEEE 1516-2010 type names. We avoid
// importing the FOM model package directly to keep this layer free
// of FOM-specific dependencies — the QoS mapping is a pure value
// translation.
type Transportation int

const (
	// TransportationReliable corresponds to IEEE 1516-2010 HLAreliable.
	TransportationReliable Transportation = 1
	// TransportationBestEffort corresponds to IEEE 1516-2010
	// HLAbestEffort.
	TransportationBestEffort Transportation = 2
)

// OrderType is the HLA-side ordering classifier.
type OrderType int

const (
	// OrderTimeStamp corresponds to IEEE 1516-2010 TimeStamp order.
	OrderTimeStamp OrderType = 1
	// OrderReceive corresponds to IEEE 1516-2010 Receive order.
	OrderReceive OrderType = 2
)

// FromHLA maps the (transportation, order) HLA tuple to a DDS QoS.
// Per docs/m19-dds-adapter.md §2.4 PINNED. Unknown values fall back
// to the most conservative DDS profile (RELIABLE + KEEP_ALL +
// BY_RECEPTION_TIMESTAMP) so a caller that hasn't yet declared
// transportation/order still gets reliable delivery; this matches
// the IEEE 1516-2010 default for an attribute that never had its
// transportation set explicitly.
func FromHLA(t Transportation, o OrderType) QoS {
	q := QoS{
		Reliability:      ReliabilityReliable,
		History:          History{Kind: HistoryKeepAll},
		DestinationOrder: DestinationOrderByReceptionTimestamp,
	}
	switch t {
	case TransportationBestEffort:
		q.Reliability = ReliabilityBestEffort
		q.History = History{Kind: HistoryKeepLast, Depth: 1}
	case TransportationReliable:
		q.Reliability = ReliabilityReliable
		q.History = History{Kind: HistoryKeepAll}
	}
	switch o {
	case OrderTimeStamp:
		q.DestinationOrder = DestinationOrderBySourceTimestamp
	case OrderReceive:
		q.DestinationOrder = DestinationOrderByReceptionTimestamp
	}
	return q
}

// itoa is a tiny strconv.Itoa shim that keeps the QoS package free of
// strconv imports. The History stringer only needs non-negative ints
// in practice (depth values).
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
