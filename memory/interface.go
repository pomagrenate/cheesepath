// Package memory provides conversation history storage for crabchain agents.
package memory

import (
	"context"

	"github.com/AutoCookies/crabpath/llm"
)

// Memory stores and retrieves conversation history.
type Memory interface {
	// Add appends a message to the history.
	Add(msg llm.Message) error
	// Messages returns the current history.
	Messages() []llm.Message
	// Compress optionally compresses history using ctx (e.g., via LLM summarisation).
	Compress(ctx context.Context) error
	// Clear resets the history.
	Clear() error
}
