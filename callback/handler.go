// Package callback provides the observability layer for crabchain.
// Handlers receive events at every step of an agent run.
package callback

import (
	"fmt"
	"io"
	"time"
)

// ThoughtEvent carries the model's reasoning for one ReAct step.
type ThoughtEvent struct {
	Reasoning   string
	Plan        string
	IsFinal     bool
	FinalAnswer string
}

// ToolCallEvent describes one tool invocation.
type ToolCallEvent struct {
	ToolName  string
	Args      map[string]any
	Dangerous bool
}

// Handler is called synchronously at each stage of an agent run.
type Handler interface {
	OnStart(goal string)
	OnThought(step int, thought ThoughtEvent)
	OnToolCall(step int, call ToolCallEvent)
	OnObservation(step int, obs string)
	OnToken(step int, token string)
	OnFinalAnswer(answer string)
	OnError(err error)
}

// NoopHandler is a no-op; embed it to avoid implementing every method.
type NoopHandler struct{}

func (NoopHandler) OnStart(_ string)                  {}
func (NoopHandler) OnThought(_ int, _ ThoughtEvent)   {}
func (NoopHandler) OnToolCall(_ int, _ ToolCallEvent) {}
func (NoopHandler) OnObservation(_ int, _ string)     {}
func (NoopHandler) OnToken(_ int, _ string)           {}
func (NoopHandler) OnFinalAnswer(_ string)            {}
func (NoopHandler) OnError(_ error)                   {}

// MultiHandler fans out all events to a list of handlers.
type MultiHandler []Handler

func (m MultiHandler) OnStart(goal string) {
	for _, h := range m {
		h.OnStart(goal)
	}
}
func (m MultiHandler) OnThought(step int, t ThoughtEvent) {
	for _, h := range m {
		h.OnThought(step, t)
	}
}
func (m MultiHandler) OnToolCall(step int, c ToolCallEvent) {
	for _, h := range m {
		h.OnToolCall(step, c)
	}
}
func (m MultiHandler) OnObservation(step int, obs string) {
	for _, h := range m {
		h.OnObservation(step, obs)
	}
}
func (m MultiHandler) OnToken(step int, token string) {
	for _, h := range m {
		h.OnToken(step, token)
	}
}
func (m MultiHandler) OnFinalAnswer(answer string) {
	for _, h := range m {
		h.OnFinalAnswer(answer)
	}
}
func (m MultiHandler) OnError(err error) {
	for _, h := range m {
		h.OnError(err)
	}
}

// LogHandler writes structured text logs to a writer.
type LogHandler struct {
	NoopHandler
	w io.Writer
}

func NewLogHandler(w io.Writer) *LogHandler { return &LogHandler{w: w} }

func (h *LogHandler) OnStart(goal string) {
	fmt.Fprintf(h.w, "[crabchain] %s  start  goal=%q\n", ts(), goal)
}
func (h *LogHandler) OnThought(step int, t ThoughtEvent) {
	if t.IsFinal {
		fmt.Fprintf(h.w, "[crabchain] %s  thought step=%d  FINAL\n", ts(), step)
	} else {
		fmt.Fprintf(h.w, "[crabchain] %s  thought step=%d  plan=%q\n", ts(), step, t.Plan)
	}
}
func (h *LogHandler) OnToolCall(step int, c ToolCallEvent) {
	fmt.Fprintf(h.w, "[crabchain] %s  tool    step=%d  name=%s  dangerous=%v\n",
		ts(), step, c.ToolName, c.Dangerous)
}
func (h *LogHandler) OnObservation(step int, obs string) {
	short := obs
	if len(short) > 120 {
		short = short[:120] + "…"
	}
	fmt.Fprintf(h.w, "[crabchain] %s  obs     step=%d  %s\n", ts(), step, short)
}
func (h *LogHandler) OnFinalAnswer(answer string) {
	short := answer
	if len(short) > 200 {
		short = short[:200] + "…"
	}
	fmt.Fprintf(h.w, "[crabchain] %s  done    answer=%q\n", ts(), short)
}
func (h *LogHandler) OnError(err error) {
	fmt.Fprintf(h.w, "[crabchain] %s  ERROR   %v\n", ts(), err)
}

func ts() string { return time.Now().Format("15:04:05.000") }

// ─── AsyncMultiHandler ────────────────────────────────────────────────────────

// AsyncMultiHandler fans events out to a list of handlers via a buffered
// channel dispatched by a single background goroutine. A slow handler (e.g. a
// file logger) does not block the agent loop. Call Close() after the run ends
// to flush all queued events and stop the goroutine.
//
// If the internal buffer is full, new events are silently dropped — this only
// affects observability, not agent correctness.
type AsyncMultiHandler struct {
	handlers []Handler
	ch       chan func()
	done     chan struct{}
}

// NewAsyncMultiHandler creates an AsyncMultiHandler with a channel buffer of
// bufSize events. The background dispatch goroutine is started immediately.
// bufSize of 256 is suitable for most runs.
func NewAsyncMultiHandler(bufSize int, handlers ...Handler) *AsyncMultiHandler {
	a := &AsyncMultiHandler{
		handlers: handlers,
		ch:       make(chan func(), bufSize),
		done:     make(chan struct{}),
	}
	go func() {
		defer close(a.done)
		for fn := range a.ch {
			fn()
		}
	}()
	return a
}

// Close drains all pending events and stops the background goroutine.
// Must be called exactly once after the agent run completes.
func (a *AsyncMultiHandler) Close() {
	close(a.ch)
	<-a.done
}

func (a *AsyncMultiHandler) send(fn func()) {
	select {
	case a.ch <- fn:
	default:
		// Buffer full: drop event rather than blocking the agent.
	}
}

func (a *AsyncMultiHandler) OnStart(goal string) {
	a.send(func() {
		for _, h := range a.handlers {
			h.OnStart(goal)
		}
	})
}
func (a *AsyncMultiHandler) OnThought(step int, t ThoughtEvent) {
	a.send(func() {
		for _, h := range a.handlers {
			h.OnThought(step, t)
		}
	})
}
func (a *AsyncMultiHandler) OnToolCall(step int, c ToolCallEvent) {
	a.send(func() {
		for _, h := range a.handlers {
			h.OnToolCall(step, c)
		}
	})
}
func (a *AsyncMultiHandler) OnObservation(step int, obs string) {
	a.send(func() {
		for _, h := range a.handlers {
			h.OnObservation(step, obs)
		}
	})
}
func (a *AsyncMultiHandler) OnToken(step int, token string) {
	a.send(func() {
		for _, h := range a.handlers {
			h.OnToken(step, token)
		}
	})
}
func (a *AsyncMultiHandler) OnFinalAnswer(answer string) {
	a.send(func() {
		for _, h := range a.handlers {
			h.OnFinalAnswer(answer)
		}
	})
}
func (a *AsyncMultiHandler) OnError(err error) {
	a.send(func() {
		for _, h := range a.handlers {
			h.OnError(err)
		}
	})
}
