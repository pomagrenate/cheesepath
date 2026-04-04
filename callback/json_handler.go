package callback

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// JSONLogHandler writes one JSON object per line for every agent event.
// Useful for log aggregation, piping, and offline analysis.
// OnToken events are omitted (too high-frequency for structured logs).
type JSONLogHandler struct {
	NoopHandler
	mu  sync.Mutex
	enc *json.Encoder
}

// NewJSONLogHandler returns a handler that writes NDJSON to w.
func NewJSONLogHandler(w io.Writer) *JSONLogHandler {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &JSONLogHandler{enc: enc}
}

type jsonEvent struct {
	TS    string `json:"ts"`
	Event string `json:"event"`
	Step  int    `json:"step,omitempty"`
	Data  any    `json:"data,omitempty"`
}

func (h *JSONLogHandler) emit(event string, step int, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	_ = h.enc.Encode(jsonEvent{
		TS:    time.Now().UTC().Format(time.RFC3339Nano),
		Event: event,
		Step:  step,
		Data:  data,
	})
}

func (h *JSONLogHandler) OnStart(goal string) {
	h.emit("start", 0, map[string]any{"goal": goal})
}

func (h *JSONLogHandler) OnThought(step int, t ThoughtEvent) {
	h.emit("thought", step, map[string]any{
		"reasoning": t.Reasoning,
		"plan":      t.Plan,
		"is_final":  t.IsFinal,
	})
}

func (h *JSONLogHandler) OnToolCall(step int, c ToolCallEvent) {
	h.emit("tool_call", step, map[string]any{
		"tool":      c.ToolName,
		"dangerous": c.Dangerous,
		"args":      c.Args,
	})
}

func (h *JSONLogHandler) OnObservation(step int, obs string) {
	short := obs
	if len(short) > 500 {
		short = short[:500] + "…"
	}
	h.emit("observation", step, map[string]any{"text": short})
}

func (h *JSONLogHandler) OnFinalAnswer(answer string) {
	h.emit("final_answer", 0, map[string]any{"answer": answer})
}

func (h *JSONLogHandler) OnError(err error) {
	h.emit("error", 0, map[string]any{"error": err.Error()})
}
