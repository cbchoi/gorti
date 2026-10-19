// Pure-Go QoS mapping tests. NO build tag — runs in the default
// build (no `-tags=dds`) because the QoS layer is pure Go and has no
// CGo / Cyclone DDS dependency. Phase 1b's CGo implementation will
// pass these same FromHLA values to the real Cyclone DDS API; if the
// mapping changes, we want the failure to surface in BOTH builds.

package dds

import (
	"testing"
)

// TestFromHLA_FourCoreCombos asserts the four core HLA→DDS QoS
// mappings docs/m19-dds-adapter.md §2.4 PINNED. Owns the wire
// contract — if the mapping moves, this test fails first and the
// design doc has to update with a clear before/after.
func TestFromHLA_FourCoreCombos(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		transport Transportation
		order     OrderType
		want      QoS
	}{
		{
			name:      "reliable+timestamp",
			transport: TransportationReliable,
			order:     OrderTimeStamp,
			want: QoS{
				Reliability:      ReliabilityReliable,
				History:          History{Kind: HistoryKeepAll},
				DestinationOrder: DestinationOrderBySourceTimestamp,
			},
		},
		{
			name:      "reliable+receive",
			transport: TransportationReliable,
			order:     OrderReceive,
			want: QoS{
				Reliability:      ReliabilityReliable,
				History:          History{Kind: HistoryKeepAll},
				DestinationOrder: DestinationOrderByReceptionTimestamp,
			},
		},
		{
			name:      "best-effort+timestamp",
			transport: TransportationBestEffort,
			order:     OrderTimeStamp,
			want: QoS{
				Reliability:      ReliabilityBestEffort,
				History:          History{Kind: HistoryKeepLast, Depth: 1},
				DestinationOrder: DestinationOrderBySourceTimestamp,
			},
		},
		{
			name:      "best-effort+receive",
			transport: TransportationBestEffort,
			order:     OrderReceive,
			want: QoS{
				Reliability:      ReliabilityBestEffort,
				History:          History{Kind: HistoryKeepLast, Depth: 1},
				DestinationOrder: DestinationOrderByReceptionTimestamp,
			},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := FromHLA(c.transport, c.order)
			if got != c.want {
				t.Errorf("FromHLA(%v, %v):\n  got=%+v\n want=%+v", c.transport, c.order, got, c.want)
			}
		})
	}
}

// TestFromHLA_UnknownDefaults asserts that unknown transport / order
// values fall back to the most conservative profile (RELIABLE +
// KEEP_ALL + BY_RECEPTION_TIMESTAMP). Matches the IEEE 1516-2010
// default for an attribute that never had its transportation set.
func TestFromHLA_UnknownDefaults(t *testing.T) {
	t.Parallel()
	got := FromHLA(0, 0)
	want := QoS{
		Reliability:      ReliabilityReliable,
		History:          History{Kind: HistoryKeepAll},
		DestinationOrder: DestinationOrderByReceptionTimestamp,
	}
	if got != want {
		t.Errorf("FromHLA(unknown, unknown):\n  got=%+v\n want=%+v", got, want)
	}
}

// TestQoSStringStable asserts QoS.String produces a stable diagnostic
// string. Tests downstream of QoS rely on the string format for
// log assertions; if the format changes, those break first.
func TestQoSStringStable(t *testing.T) {
	t.Parallel()
	q := FromHLA(TransportationBestEffort, OrderTimeStamp)
	got := q.String()
	want := "best-effort/keep-last(1)/by-source-timestamp"
	if got != want {
		t.Errorf("QoS.String:\n  got=%q\n want=%q", got, want)
	}
}

// TestReliabilityStringerStable + TestHistoryStringerStable +
// TestDestinationOrderStringerStable lock the individual stringers
// used in metrics / logs. Future code that reformats the strings
// will surface here.
func TestReliabilityStringerStable(t *testing.T) {
	t.Parallel()
	if got := ReliabilityReliable.String(); got != "reliable" {
		t.Errorf("ReliabilityReliable.String=%q; want %q", got, "reliable")
	}
	if got := ReliabilityBestEffort.String(); got != "best-effort" {
		t.Errorf("ReliabilityBestEffort.String=%q; want %q", got, "best-effort")
	}
}

func TestHistoryStringerStable(t *testing.T) {
	t.Parallel()
	if got := (History{Kind: HistoryKeepAll}).String(); got != "keep-all" {
		t.Errorf("History{KeepAll}.String=%q; want %q", got, "keep-all")
	}
	if got := (History{Kind: HistoryKeepLast, Depth: 5}).String(); got != "keep-last(5)" {
		t.Errorf("History{KeepLast,5}.String=%q; want %q", got, "keep-last(5)")
	}
}

func TestDestinationOrderStringerStable(t *testing.T) {
	t.Parallel()
	if got := DestinationOrderBySourceTimestamp.String(); got != "by-source-timestamp" {
		t.Errorf("BySourceTimestamp.String=%q; want %q", got, "by-source-timestamp")
	}
	if got := DestinationOrderByReceptionTimestamp.String(); got != "by-reception-timestamp" {
		t.Errorf("ByReceptionTimestamp.String=%q; want %q", got, "by-reception-timestamp")
	}
}
