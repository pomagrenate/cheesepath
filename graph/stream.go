package graph

// StreamMode determines the granularity of events emitted during graph streaming.
type StreamMode string

const (
	// StreamModeValues emits the complete state snapshot after each super-step.
	StreamModeValues StreamMode = "values"
	// StreamModeUpdates emits the node name and state update delta after each node executes.
	StreamModeUpdates StreamMode = "updates"
)

// StreamEvent represents a single event emitted on the graph stream channel.
type StreamEvent[S any] struct {
	Step        int        `json:"step"`
	Node        string     `json:"node"`
	Values      S          `json:"values,omitempty"`
	Update      S          `json:"update,omitempty"`
	Interrupted bool       `json:"interrupted,omitempty"`
	Error       error      `json:"error,omitempty"`
}
