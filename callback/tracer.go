package callback

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// SpanType categorizes the execution layer of a trace span.
type SpanType string

const (
	SpanTypeWorkflow  SpanType = "workflow"
	SpanTypeSuperStep SpanType = "super_step"
	SpanTypeNode      SpanType = "node"
	SpanTypeTool      SpanType = "tool"
	SpanTypeModel     SpanType = "model"
)

// Span represents a single timed unit of work matching OpenInference / OpenTelemetry schemas.
type Span struct {
	mu           sync.RWMutex
	TraceID      string         `json:"trace_id"`
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id,omitempty"`
	Name         string         `json:"name"`
	Type         SpanType       `json:"type"`
	StartTime    time.Time      `json:"start_time"`
	EndTime      time.Time      `json:"end_time"`
	Duration     time.Duration  `json:"duration_ns"`
	DurationStr  string         `json:"duration"`
	Status       string         `json:"status"` // "OK" or "ERROR"
	Error        string         `json:"error,omitempty"`
	Attributes   map[string]any `json:"attributes,omitempty"`
}

// SetAttribute adds metadata to the span.
func (s *Span) SetAttribute(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Attributes == nil {
		s.Attributes = make(map[string]any)
	}
	s.Attributes[key] = value
}

// Tracer manages OpenInference-compatible spans across workflows, super-steps, nodes, and tools.
type Tracer struct {
	mu      sync.RWMutex
	traceID string
	counter int64
	spans   []*Span
}

// NewTracer creates a new Tracer.
func NewTracer(traceID ...string) *Tracer {
	tID := fmt.Sprintf("trace-%d", time.Now().UnixNano())
	if len(traceID) > 0 && traceID[0] != "" {
		tID = traceID[0]
	}
	return &Tracer{
		traceID: tID,
		spans:   make([]*Span, 0),
	}
}

type tracerCtxKey struct{}

// WithSpan injects the current active span into context.
func WithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, tracerCtxKey{}, span)
}

// CurrentSpan retrieves the active span from context, if present.
func CurrentSpan(ctx context.Context) *Span {
	if val, ok := ctx.Value(tracerCtxKey{}).(*Span); ok {
		return val
	}
	return nil
}

// StartSpan begins a new timed span.
func (t *Tracer) StartSpan(ctx context.Context, name string, spanType SpanType) (context.Context, *Span) {
	parent := CurrentSpan(ctx)
	parentID := ""
	if parent != nil {
		parentID = parent.SpanID
	}

	spanID := fmt.Sprintf("span-%d", atomic.AddInt64(&t.counter, 1))
	span := &Span{
		TraceID:      t.traceID,
		SpanID:       spanID,
		ParentSpanID: parentID,
		Name:         name,
		Type:         spanType,
		StartTime:    time.Now().UTC(),
		Status:       "OK",
		Attributes:   make(map[string]any),
	}

	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()

	return WithSpan(ctx, span), span
}

// EndSpan concludes a span and records its duration and error status.
func (t *Tracer) EndSpan(span *Span, err ...error) {
	if span == nil {
		return
	}
	span.mu.Lock()
	defer span.mu.Unlock()

	span.EndTime = time.Now().UTC()
	span.Duration = span.EndTime.Sub(span.StartTime)
	span.DurationStr = span.Duration.String()

	if len(err) > 0 && err[0] != nil {
		span.Status = "ERROR"
		span.Error = err[0].Error()
	}
}

// Spans returns a copy of all recorded spans.
func (t *Tracer) Spans() []*Span {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*Span, len(t.spans))
	copy(out, t.spans)
	return out
}

// ToJSON serializes all spans to formatted JSON.
func (t *Tracer) ToJSON() ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return json.MarshalIndent(t.spans, "", "  ")
}

// PrintTree prints a formatted visual tree of all spans to w (or stdout if nil).
func (t *Tracer) PrintTree(w ...io.Writer) {
	var out io.Writer = os.Stdout
	if len(w) > 0 && w[0] != nil {
		out = w[0]
	}

	spans := t.Spans()
	childMap := make(map[string][]*Span)
	var rootSpans []*Span

	for _, s := range spans {
		if s.ParentSpanID == "" {
			rootSpans = append(rootSpans, s)
		} else {
			childMap[s.ParentSpanID] = append(childMap[s.ParentSpanID], s)
		}
	}

	var printSpan func(s *Span, indent string, isLast bool)
	printSpan = func(s *Span, indent string, isLast bool) {
		marker := "├── "
		if isLast {
			marker = "└── "
		}
		statusMarker := ""
		if s.Status == "ERROR" {
			statusMarker = " [ERROR: " + s.Error + "]"
		}

		fmt.Fprintf(out, "%s%s[%s] %q (duration: %s)%s\n", indent, marker, s.Type, s.Name, s.DurationStr, statusMarker)

		children := childMap[s.SpanID]
		nextIndent := indent
		if isLast {
			nextIndent += "    "
		} else {
			nextIndent += "│   "
		}

		for i, child := range children {
			printSpan(child, nextIndent, i == len(children)-1)
		}
	}

	fmt.Fprintf(out, "Trace: %s\n", t.traceID)
	for i, root := range rootSpans {
		printSpan(root, "", i == len(rootSpans)-1)
	}
}
