package memory

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/AutoCookies/crabpath/llm"
)

// VectorEntry stores a message alongside its embedding.
type VectorEntry struct {
	Msg       llm.Message
	Embedding []float32 // nil if embed failed (graceful degradation)
	idx       int       // original insertion order
}

// VectorMemory stores messages with semantic embeddings and retrieves
// the most contextually relevant past turns using cosine similarity.
// It implements both Memory and SemanticMemory.
type VectorMemory struct {
	mu        sync.RWMutex
	embed     EmbedFunc
	entries   []VectorEntry
	maxTokens int // approximate token budget for Retrieve results
}

// NewVectorMemory creates a VectorMemory backed by the provided embedding function.
// maxTokens is the approximate number of tokens (chars/4) to budget for Retrieve output.
func NewVectorMemory(embedFn EmbedFunc, maxTokens int) *VectorMemory {
	return &VectorMemory{embed: embedFn, maxTokens: maxTokens}
}

// Add appends msg to the store and attempts to compute its embedding.
// If embedding fails the message is stored without one (graceful degradation).
func (v *VectorMemory) Add(msg llm.Message) error {
	emb, _ := v.embed(msg.Content) // ignore error; nil embedding is handled in Retrieve
	v.mu.Lock()
	defer v.mu.Unlock()
	v.entries = append(v.entries, VectorEntry{
		Msg:       msg,
		Embedding: emb,
		idx:       len(v.entries),
	})
	return nil
}

// Messages returns all stored messages in chronological order.
func (v *VectorMemory) Messages() []llm.Message {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]llm.Message, len(v.entries))
	for i, e := range v.entries {
		out[i] = e.Msg
	}
	return out
}

// Retrieve returns up to topK messages most semantically similar to query,
// re-sorted in chronological order, respecting the maxTokens budget.
func (v *VectorMemory) Retrieve(query string, topK int) ([]llm.Message, error) {
	queryEmb, err := v.embed(query)
	if err != nil {
		// Fallback: return recent messages up to budget
		return v.recentMessages(), nil
	}

	v.mu.RLock()
	entries := make([]VectorEntry, len(v.entries))
	copy(entries, v.entries)
	v.mu.RUnlock()

	type scored struct {
		entry VectorEntry
		score float32
	}
	var candidates []scored
	for _, e := range entries {
		if e.Embedding == nil {
			continue
		}
		s := cosineSimilarity(queryEmb, e.Embedding)
		candidates = append(candidates, scored{e, s})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	budget := v.maxTokens * 4 // approx chars per token
	var selected []VectorEntry
	for _, c := range candidates {
		if len(selected) >= topK {
			break
		}
		budget -= len(c.entry.Msg.Content)
		if budget < 0 {
			break
		}
		selected = append(selected, c.entry)
	}

	// Re-sort by original insertion order (chronological)
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].idx < selected[j].idx
	})

	out := make([]llm.Message, len(selected))
	for i, e := range selected {
		out[i] = e.Msg
	}
	return out, nil
}

// Compress is a no-op; embeddings provide natural compression.
func (v *VectorMemory) Compress(_ context.Context) error { return nil }

// Clear resets all stored messages and embeddings.
func (v *VectorMemory) Clear() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.entries = nil
	return nil
}

// recentMessages returns recent messages up to the token budget (fallback for Retrieve).
func (v *VectorMemory) recentMessages() []llm.Message {
	v.mu.RLock()
	defer v.mu.RUnlock()
	budget := v.maxTokens * 4
	var out []llm.Message
	for i := len(v.entries) - 1; i >= 0; i-- {
		budget -= len(v.entries[i].Msg.Content)
		if budget < 0 {
			break
		}
		out = append([]llm.Message{v.entries[i].Msg}, out...)
	}
	return out
}

// cosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0 if either vector has zero norm.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}

// Ensure VectorMemory satisfies both interfaces at compile time.
var _ Memory = (*VectorMemory)(nil)
var _ SemanticMemory = (*VectorMemory)(nil)
