// Package telemetry provides structured logging and an in-memory metrics
// registry rendered in Prometheus text format. Every event is logged with its
// change_id/trace_id so the watcher→push→AI→status path can be correlated.
//
// The registry is intentionally dependency-light; an OTLP exporter can be added
// against the same Snapshot() surface without touching call sites.
package telemetry

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
)

// Metric names emitted by the agent.
const (
	QueueDepth          = "krennic_queue_depth"
	EventLatencyMS      = "krennic_event_latency_ms"
	ModelLatencyMS      = "krennic_model_latency_ms"
	TriageEscalations   = "krennic_triage_escalations_total"
	TriageTotal         = "krennic_triage_total"
	CacheHits           = "krennic_dedup_cache_hits_total"
	ShadowPushFailures  = "krennic_shadow_push_failures_total"
	ShadowPushTotal     = "krennic_shadow_push_total"
	AICostUSD           = "krennic_ai_cost_usd_total"
	ProviderErrors      = "krennic_provider_errors_total"
	ChangesProcessed    = "krennic_changes_processed_total"
)

// Metrics is a small thread-safe registry.
type Metrics struct {
	mu       sync.Mutex
	counters map[string]float64
	gauges   map[string]float64
	obsSum   map[string]float64
	obsCount map[string]float64
}

// NewMetrics returns an empty registry.
func NewMetrics() *Metrics {
	return &Metrics{
		counters: map[string]float64{},
		gauges:   map[string]float64{},
		obsSum:   map[string]float64{},
		obsCount: map[string]float64{},
	}
}

// Inc adds 1 to a counter.
func (m *Metrics) Inc(name string) { m.Add(name, 1) }

// Add adds v to a counter.
func (m *Metrics) Add(name string, v float64) {
	m.mu.Lock()
	m.counters[name] += v
	m.mu.Unlock()
}

// SetGauge sets a gauge value.
func (m *Metrics) SetGauge(name string, v float64) {
	m.mu.Lock()
	m.gauges[name] = v
	m.mu.Unlock()
}

// Observe records a value into a sum/count pair (latency histograms-lite).
func (m *Metrics) Observe(name string, v float64) {
	m.mu.Lock()
	m.obsSum[name] += v
	m.obsCount[name]++
	m.mu.Unlock()
}

// Snapshot returns a flat name→value view for the dashboard.
func (m *Metrics) Snapshot() map[string]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]float64{}
	for k, v := range m.counters {
		out[k] = v
	}
	for k, v := range m.gauges {
		out[k] = v
	}
	for k, v := range m.obsSum {
		if c := m.obsCount[k]; c > 0 {
			out[k+"_avg"] = v / c
		}
	}
	// Derived rate: escalation ratio.
	if t := m.counters[TriageTotal]; t > 0 {
		out["krennic_escalation_ratio"] = m.counters[TriageEscalations] / t
	}
	return out
}

// Prometheus renders the registry in text exposition format.
func (m *Metrics) Prometheus() string {
	snap := m.Snapshot()
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s %g\n", k, snap[k])
	}
	return b.String()
}

// NewLogger builds the process logger.
func NewLogger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
