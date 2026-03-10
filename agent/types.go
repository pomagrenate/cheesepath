// Package agent defines all core data structures for the crabpath agent path
// execution engine: the building blocks of every CrabPath run.
package agent

import "time"

// ─── Core structs ────────────────────────────────────────────────────────────

// CrabPath represents a single agent execution run from goal → final answer.
type CrabPath struct {
	ID        string      `json:"id"`
	Goal      string      `json:"goal"`
	Steps     []CrabStep  `json:"steps"`
	Status    PathStatus  `json:"status"`
	StartedAt time.Time   `json:"started_at"`
	EndedAt   *time.Time  `json:"ended_at,omitempty"`
	Answer    string      `json:"answer,omitempty"`
}

// PathStatus tracks the current lifecycle state of a CrabPath.
type PathStatus string

const (
	PathRunning   PathStatus = "running"
	PathCompleted PathStatus = "completed"
	PathFailed    PathStatus = "failed"
	PathAborted   PathStatus = "aborted"
)

// CrabStep is one iteration of the ReAct loop: Thought → Action → Observation.
type CrabStep struct {
	Index       int          `json:"index"`
	Thought     CrabThought  `json:"thought"`
	ToolCalls   []CrabToolCall `json:"tool_calls,omitempty"`
	Observation string       `json:"observation,omitempty"`
	IsFinal     bool         `json:"is_final,omitempty"`
}

// CrabThought is the raw model output for a single reasoning step.
type CrabThought struct {
	Reasoning   string `json:"reasoning"`
	Plan        string `json:"plan,omitempty"`
	IsFinal     bool   `json:"is_final"`
	FinalAnswer string `json:"final_answer,omitempty"`
}

// CrabToolCall captures a single tool invocation decided by the model.
type CrabToolCall struct {
	ToolName  string            `json:"tool"`
	Args      map[string]any    `json:"args"`
	Dangerous bool              `json:"dangerous,omitempty"` // triggers user approval
	Approved  *bool             `json:"approved,omitempty"`  // nil = pending
	Result    string            `json:"result,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// LLMRequest is kept as a type alias for llm.Request to avoid import cycles.
// The actual struct is defined in the llm package.


// ─── Stream events ────────────────────────────────────────────────────────────

// StreamEventType identifies what happened in the SSE stream.
type StreamEventType string

const (
	EventThought     StreamEventType = "thought"
	EventToolCall    StreamEventType = "tool_call"
	EventObservation StreamEventType = "observation"
	EventFinalAnswer StreamEventType = "final_answer"
	EventError       StreamEventType = "error"
	EventApprovalReq StreamEventType = "approval_required"
)

// StreamEvent is one SSE-style event emitted during a CrabPath run.
type StreamEvent struct {
	Type    StreamEventType `json:"type"`
	Step    int             `json:"step"`
	Payload any             `json:"payload"`
}
