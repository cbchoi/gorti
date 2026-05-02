package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// federationLister is the metrics handler's view of the federation
// manager — just enough to enumerate current federations.
type federationLister interface {
	List(ctx context.Context) ([]core.FederationSummary, error)
}

// objectCounter exposes the per-federation object instance count. Wired
// in main.go from the object registry.
type objectCounter interface {
	ObjectCount(fed core.FederationName) uint64
}

// eventLogSeqSource exposes the per-federation monotonic event-log seq.
// Wired in main.go from the multiplex writer.
type eventLogSeqSource interface {
	EventLogSeq(fed core.FederationName) uint64
}

// metricsHandler serves Prometheus text-format gauges on /metrics and a
// liveness probe on /. Implements http.Handler.
//
// Format reference: Prometheus exposition text format v0.0.4 — minimal
// subset (gauge metrics, # HELP / # TYPE comments, no HISTOGRAM /
// SUMMARY shapes which add HELP, COUNTER labels, etc.).
type metricsHandler struct {
	feds federationLister
	objs objectCounter
	seqs eventLogSeqSource
	mux  *http.ServeMux
}

// newMetricsHandler constructs the handler with the supplied collectors.
// Callers wire production implementations from federation.Manager (List),
// object.Registry (ObjectCount via an adapter), and the multiplex writer
// (EventLogSeq via an adapter). Tests pass small inline stubs.
func newMetricsHandler(feds federationLister, objs objectCounter, seqs eventLogSeqSource) *metricsHandler {
	h := &metricsHandler{
		feds: feds,
		objs: objs,
		seqs: seqs,
		mux:  http.NewServeMux(),
	}
	h.mux.HandleFunc("/metrics", h.serveMetrics)
	h.mux.HandleFunc("/", h.serveHealth)
	return h
}

// ServeHTTP routes to the registered handlers.
func (h *metricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// serveMetrics emits the four NFR-OPS-1 gauges in Prometheus exposition
// format. Per-federation labels are emitted in name-sorted order so
// scrape diffs are stable across runs (D-2 determinism even on the
// monitoring side).
func (h *metricsHandler) serveMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	summaries, err := h.feds.List(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("metrics: list federations: %v", err), http.StatusInternalServerError)
		return
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})

	emitGauge(w, "gorti_federations_total", "current federation count", func() {
		fmt.Fprintf(w, "gorti_federations_total %d\n", len(summaries))
	})
	emitGauge(w, "gorti_federates_total", "current federate count per federation", func() {
		for _, s := range summaries {
			fmt.Fprintf(w, "gorti_federates_total{federation=%q} %d\n", string(s.Name), s.FederatesJoined)
		}
	})
	emitGauge(w, "gorti_event_log_seq", "current monotonic event-log seq per federation", func() {
		for _, s := range summaries {
			seq := uint64(0)
			if h.seqs != nil {
				seq = h.seqs.EventLogSeq(s.Name)
			}
			fmt.Fprintf(w, "gorti_event_log_seq{federation=%q} %d\n", string(s.Name), seq)
		}
	})
	emitGauge(w, "gorti_object_handles_total", "current object count per federation", func() {
		for _, s := range summaries {
			n := uint64(0)
			if h.objs != nil {
				n = h.objs.ObjectCount(s.Name)
			}
			fmt.Fprintf(w, "gorti_object_handles_total{federation=%q} %d\n", string(s.Name), n)
		}
	})
}

// serveHealth returns a small text body identifying the process so an
// external healthcheck can tell rtid is alive at the metrics port.
func (h *metricsHandler) serveHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "rtid metrics endpoint alive\n")
}

// emitGauge writes the standard HELP/TYPE prelude plus the body emitted
// by emitFn. Centralized so tests can rely on the same shape.
func emitGauge(w io.Writer, name, help string, emitFn func()) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)
	emitFn()
}
