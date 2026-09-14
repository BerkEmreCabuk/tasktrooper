package http

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	ToolCallsTotal  *prometheus.CounterVec
	AgentIterations prometheus.Histogram
	LLMLatency      prometheus.Histogram
	MCPErrorsTotal  prometheus.Counter
	AgentScore      *prometheus.GaugeVec
	AgentRevisions  *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	m := &Metrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_requests_total",
			Help: "Total HTTP requests",
		}, []string{"method", "path", "status"}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bridge_request_duration_seconds",
			Help:    "HTTP request duration",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path"}),
		ToolCallsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_tool_calls_total",
			Help: "Total tool calls",
		}, []string{"tool", "is_error"}),
		AgentIterations: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "bridge_agent_iterations",
			Help:    "Agent loop iterations per request",
			Buckets: []float64{1, 2, 3, 5, 10, 20},
		}),
		LLMLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "bridge_llm_latency_seconds",
			Help:    "LLM chat latency",
			Buckets: prometheus.DefBuckets,
		}),
		MCPErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "bridge_mcp_errors_total",
			Help: "MCP connection errors",
		}),
		AgentScore: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bridge_agent_score",
			Help: "Current performance score per agent",
		}, []string{"agent_id"}),
		AgentRevisions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bridge_agent_revisions_total",
			Help: "Total revision events per agent",
		}, []string{"agent_id"}),
	}
	prometheus.MustRegister(m.RequestsTotal, m.RequestDuration, m.ToolCallsTotal, m.AgentIterations, m.LLMLatency, m.MCPErrorsTotal, m.AgentScore, m.AgentRevisions)
	return m
}

// RecordAgentScore feeds the board ScoreTracker hook into Prometheus.
func (h *Handler) RecordAgentScore(agentID string, score, delta float64) {
	if h.metrics == nil {
		return
	}
	h.metrics.AgentScore.WithLabelValues(agentID).Set(score)
	if delta < 0 {
		h.metrics.AgentRevisions.WithLabelValues(agentID).Inc()
	}
}
