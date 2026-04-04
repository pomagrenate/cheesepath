package callback

import (
	"sync"
	"time"

	"github.com/AutoCookies/crabpath/llm"
)

// StepMetric captures per-tool-call performance data for one agent step.
type StepMetric struct {
	Step      int
	ToolName  string        // empty for thought-only steps
	Dangerous bool
	Tokens    int           // estimated tokens in the observation
	Duration  time.Duration // time between tool call and observation
}

// Metrics is a snapshot of aggregate performance data for one agent run.
type Metrics struct {
	Goal          string
	TotalSteps    int
	ToolCallCount int
	TotalTokens   int
	TotalDuration time.Duration
	Steps         []StepMetric
}

// MetricsHandler collects performance statistics across an agent run.
// Retrieve results via Snapshot() after the run completes.
type MetricsHandler struct {
	NoopHandler
	mu      sync.Mutex
	start   time.Time
	toolAt  time.Time
	current StepMetric
	m       Metrics
}

// NewMetricsHandler creates a ready-to-use MetricsHandler.
func NewMetricsHandler() *MetricsHandler { return &MetricsHandler{} }

// Snapshot returns a deep copy of the current metrics. Thread-safe.
func (h *MetricsHandler) Snapshot() Metrics {
	h.mu.Lock()
	defer h.mu.Unlock()
	steps := make([]StepMetric, len(h.m.Steps))
	copy(steps, h.m.Steps)
	m := h.m
	m.Steps = steps
	return m
}

// Reset clears all collected metrics.
func (h *MetricsHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.m = Metrics{}
	h.start = time.Time{}
	h.toolAt = time.Time{}
	h.current = StepMetric{}
}

func (h *MetricsHandler) OnStart(goal string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.start = time.Now()
	h.m.Goal = goal
}

func (h *MetricsHandler) OnThought(_ int, t ThoughtEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.m.TotalTokens += llm.EstimateTokens(t.Reasoning + t.Plan + t.FinalAnswer)
	h.m.TotalSteps++
}

func (h *MetricsHandler) OnToolCall(step int, c ToolCallEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.toolAt = time.Now()
	h.current = StepMetric{
		Step:      step,
		ToolName:  c.ToolName,
		Dangerous: c.Dangerous,
	}
}

func (h *MetricsHandler) OnObservation(_ int, obs string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.current.Tokens = llm.EstimateTokens(obs)
	h.current.Duration = time.Since(h.toolAt)
	h.m.TotalTokens += h.current.Tokens
	h.m.ToolCallCount++
	h.m.Steps = append(h.m.Steps, h.current)
	h.current = StepMetric{}
}

func (h *MetricsHandler) OnFinalAnswer(answer string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.m.TotalTokens += llm.EstimateTokens(answer)
	h.m.TotalDuration = time.Since(h.start)
}
