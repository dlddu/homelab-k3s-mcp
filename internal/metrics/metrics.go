// Package metrics is the prd-metrics surface: the four families that PRD's
// "표면 개요" table closes, derived from tool-call records.
package metrics

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"

	"github.com/dlddu/homelab-k3s-mcp/internal/eventlog"
	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
)

// Unregistered is the tool label of a call that named no registered tool.
// It is one value, not dropped: a rise in calls to tools that do not exist
// is a misconfigured client or someone probing.
const Unregistered = "unregistered"

// gateWaitBuckets are seconds a human takes to answer, not a handler: the
// gate's own timeout is minutes, and a distribution bucketed for
// milliseconds would put every real wait in +Inf.
var gateWaitBuckets = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800}

// Metrics holds the four families on their own registry. Its own rather than
// the default one so that what /metrics exposes is exactly the table AC4
// closes — the process and Go runtime collectors the default registry
// carries would add label names the AC does not list.
type Metrics struct {
	registry *prometheus.Registry
	tools    map[string]bool

	calls    *prometheus.CounterVec
	refusals *prometheus.CounterVec
	duration *prometheus.HistogramVec
	gateWait *prometheus.HistogramVec
}

// New builds the surface for the given registered tools and pre-registers
// every series the table bounds.
func New(tools []string) (*Metrics, error) {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		tools:    make(map[string]bool, len(tools)),
		calls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_tool_calls_total",
			Help: "Tool calls by registered tool and result (prd-metrics AC1).",
		}, []string{"tool", "result"}),
		refusals: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_tool_refusals_total",
			Help: "Refused tool calls by registered tool and reason (prd-metrics AC2).",
		}, []string{"tool", "reason"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mcp_tool_duration_seconds",
			Help:    "Handler latency of tool calls that ran, gate wait excluded (prd-metrics AC3).",
			Buckets: prometheus.DefBuckets,
		}, []string{"tool"}),
		gateWait: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mcp_gate_wait_seconds",
			Help:    "Time the approval gate held a call, by its verdict (prd-metrics AC3).",
			Buckets: gateWaitBuckets,
		}, []string{"decision"}),
	}
	for _, c := range []prometheus.Collector{m.calls, m.refusals, m.duration, m.gateWait} {
		if err := m.registry.Register(c); err != nil {
			return nil, err
		}
	}
	for _, tool := range tools {
		if tool == Unregistered {
			return nil, fmt.Errorf("metrics: a tool is named %q, which is the fold value for unregistered names", Unregistered)
		}
		m.tools[tool] = true
	}
	for _, tool := range append(tools, Unregistered) {
		for _, result := range eventlog.Results {
			m.calls.WithLabelValues(tool, string(result))
		}
		for _, reason := range eventlog.Reasons {
			m.refusals.WithLabelValues(tool, string(reason))
		}
		m.duration.WithLabelValues(tool)
	}
	for _, verdict := range gatekeeper.Verdicts {
		m.gateWait.WithLabelValues(string(verdict))
	}
	return m, nil
}

// Emit implements eventlog.Sink: one record moves the counters its labels
// name and observes the two durations it carries.
func (m *Metrics) Emit(_ context.Context, r eventlog.Record) {
	tool := r.Tool
	if !m.tools[tool] {
		tool = Unregistered
	}
	m.calls.WithLabelValues(tool, string(r.Result)).Inc()
	switch r.Result {
	case eventlog.ResultRefused:
		m.refusals.WithLabelValues(tool, string(r.Reason)).Inc()
	default:
		// A refused call ran no handler, so it has no handler latency to
		// observe: AC3's "처리 지연" is the distribution of calls that did
		// something, and a burst of refusals must not pull it toward zero.
		m.duration.WithLabelValues(tool).Observe(r.Duration.Seconds())
	}
	if r.Gate.Decision != "" {
		// A gated call refused before any verdict — its approval context
		// could not be built — has no decision and is not a wait a human
		// took.
		m.gateWait.WithLabelValues(r.Gate.Decision).Observe(r.GateWait.Seconds())
	}
}

// Handler serves the registry in the Prometheus text exposition.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Gather is the registry's own, for tests that read the families as values
// rather than scraping the text.
func (m *Metrics) Gather() ([]*dto.MetricFamily, error) {
	return m.registry.Gather()
}
