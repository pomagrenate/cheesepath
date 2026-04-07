package memory

import (
	"context"
	"sync"

	"github.com/AutoCookies/crabpath/llm"
)

// SlidingWindowMemory retains only the most recent windowSize messages.
// When Add is called and the window is full, the oldest message is evicted.
// Compress is a no-op because the window is already bounded.
type SlidingWindowMemory struct {
	mu         sync.RWMutex
	msgs       []llm.Message
	windowSize int
}

// NewSlidingWindowMemory creates a SlidingWindowMemory with the given window size.
// windowSize must be > 0; if ≤ 0 it defaults to 30.
func NewSlidingWindowMemory(windowSize int) *SlidingWindowMemory {
	if windowSize <= 0 {
		windowSize = 30
	}
	return &SlidingWindowMemory{windowSize: windowSize}
}

// Add appends msg. If the window is full, the oldest message is dropped first.
func (s *SlidingWindowMemory) Add(msg llm.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.msgs) >= s.windowSize {
		s.msgs = s.msgs[1:]
	}
	s.msgs = append(s.msgs, msg)
	return nil
}

// Messages returns a copy of the current window in chronological order.
func (s *SlidingWindowMemory) Messages() []llm.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]llm.Message, len(s.msgs))
	copy(out, s.msgs)
	return out
}

// Compress is a no-op: the window is already bounded.
func (s *SlidingWindowMemory) Compress(_ context.Context) error { return nil }

// Clear empties the window.
func (s *SlidingWindowMemory) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = nil
	return nil
}

// Ensure SlidingWindowMemory satisfies Memory at compile time.
var _ Memory = (*SlidingWindowMemory)(nil)
