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

// EmbedFunc embeds a text string into a float32 vector.
// Keeping this in the memory package lets VectorMemory stay dependency-free;
// callers inject the actual embedding implementation.
type EmbedFunc func(text string) ([]float32, error)

// SemanticMemory extends Memory with query-aware retrieval.
// Executors can type-assert on this to seed history with relevant context
// instead of returning the full chronological history.
type SemanticMemory interface {
	Memory
	Retrieve(query string, topK int) ([]llm.Message, error)
}
