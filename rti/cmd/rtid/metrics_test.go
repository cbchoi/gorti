package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// stubFedSource lets the metrics handler exercise its emission path
// without instantiating a real federation manager.
type stubFedSource struct {
	summaries []core.FederationSummary
}

func (s *stubFedSource) List(_ context.Context) ([]core.FederationSummary, error) {
	return s.summaries, nil
}

// stubObjCounter reports per-federation object counts. Tests inject a
// fixed map; production wires a hook into the object registry.
type stubObjCounter struct {
	byFed map[core.FederationName]uint64
}

func (s *stubObjCounter) ObjectCount(fed core.FederationName) uint64 {
	return s.byFed[fed]
}

// stubSeqSource reports per-federation event log seq. Production wires a
// hook into the multiplex writer.
type stubSeqSource struct {
	byFed map[core.FederationName]uint64
}

func (s *stubSeqSource) EventLogSeq(fed core.FederationName) uint64 {
	return s.byFed[fed]
}

// TestMetricsHandler_EmitsExpectedGauges: GET /metrics renders the four
// named gauges with current values.
func TestMetricsHandler_EmitsExpectedGauges(t *testing.T) {
	feds := &stubFedSource{summaries: []core.FederationSummary{
		{Name: "alpha", Mode: core.ModeVerbose, FederatesJoined: 3},
		{Name: "beta", Mode: core.ModeBestEffort, FederatesJoined: 1},
	}}
	objs := &stubObjCounter{byFed: map[core.FederationName]uint64{
		"alpha": 7,
		"beta":  2,
	}}
	seqs := &stubSeqSource{byFed: map[core.FederationName]uint64{
		"alpha": 100,
		"beta":  50,
	}}

	h := newMetricsHandler(feds, objs, seqs)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics") //nolint:gosec,noctx // test server
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	wantContains := []string{
		"# HELP gorti_federations_total",
		"# TYPE gorti_federations_total gauge",
		"gorti_federations_total 2",
		`gorti_federates_total{federation="alpha"} 3`,
		`gorti_federates_total{federation="beta"} 1`,
		`gorti_event_log_seq{federation="alpha"} 100`,
		`gorti_object_handles_total{federation="alpha"} 7`,
	}
	for _, w := range wantContains {
		if !strings.Contains(text, w) {
			t.Errorf("metrics output missing %q.\nGot:\n%s", w, text)
		}
	}
}

// TestMetricsHandler_HealthCheck: GET / returns 200 with a small probe
// body so an external healthcheck can confirm the metrics process is up.
func TestMetricsHandler_HealthCheck(t *testing.T) {
	h := newMetricsHandler(&stubFedSource{}, &stubObjCounter{}, &stubSeqSource{})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/") //nolint:gosec,noctx
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "rtid") {
		t.Errorf("health-check body missing 'rtid' identifier; got %q", body)
	}
}

// TestMetricsHandler_EmptyState: with no federations, the handler still
// emits the federation count (0) without crashing.
func TestMetricsHandler_EmptyState(t *testing.T) {
	h := newMetricsHandler(&stubFedSource{}, &stubObjCounter{}, &stubSeqSource{})
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics") //nolint:gosec,noctx
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "gorti_federations_total 0") {
		t.Errorf("expected gorti_federations_total 0 in empty-state output; got\n%s", body)
	}
}
