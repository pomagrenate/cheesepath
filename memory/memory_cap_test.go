package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/AutoCookies/crabpath/llm"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func msg(role, content string) llm.Message { return llm.Message{Role: role, Content: content} }

func stubEmbed(text string) ([]float32, error) {
	// Returns a tiny deterministic embedding based on text length.
	return []float32{float32(len(text)), 0, 1}, nil
}

// ─── BufferMemory ─────────────────────────────────────────────────────────────

func TestNewBufferMemory_Unbounded(t *testing.T) {
	m := NewBufferMemory()
	for i := range 300 {
		_ = m.Add(msg("user", fmt.Sprintf("msg %d", i)))
	}
	if got := len(m.Messages()); got != 300 {
		t.Fatalf("unbounded BufferMemory: want 300 msgs, got %d", got)
	}
}

func TestBoundedBufferMemory_CapEnforced(t *testing.T) {
	const cap = 5
	m := NewBoundedBufferMemory(cap)
	for i := range 10 {
		_ = m.Add(msg("user", fmt.Sprintf("msg %d", i)))
	}
	msgs := m.Messages()
	if len(msgs) != cap {
		t.Fatalf("want %d msgs after 10 adds, got %d", cap, len(msgs))
	}
}

func TestBoundedBufferMemory_FIFOEviction(t *testing.T) {
	const cap = 3
	m := NewBoundedBufferMemory(cap)
	for i := range 5 {
		_ = m.Add(msg("user", fmt.Sprintf("msg%d", i)))
	}
	msgs := m.Messages()
	// After adding msg0..msg4 into cap=3, we keep msg2, msg3, msg4.
	if msgs[0].Content != "msg2" {
		t.Fatalf("FIFO eviction: first msg should be msg2, got %q", msgs[0].Content)
	}
	if msgs[cap-1].Content != "msg4" {
		t.Fatalf("FIFO eviction: last msg should be msg4, got %q", msgs[cap-1].Content)
	}
}

func TestBoundedBufferMemory_Clear(t *testing.T) {
	m := NewBoundedBufferMemory(10)
	_ = m.Add(msg("user", "hello"))
	_ = m.Clear()
	if len(m.Messages()) != 0 {
		t.Fatal("expected empty after Clear")
	}
}

func TestBoundedBufferMemory_ExactlyAtCap(t *testing.T) {
	const cap = 4
	m := NewBoundedBufferMemory(cap)
	for i := range cap {
		_ = m.Add(msg("user", fmt.Sprintf("x%d", i)))
	}
	if got := len(m.Messages()); got != cap {
		t.Fatalf("at-cap: want %d, got %d", cap, got)
	}
	// One more add should evict oldest.
	_ = m.Add(msg("user", "new"))
	msgs := m.Messages()
	if len(msgs) != cap {
		t.Fatalf("over-cap: want %d, got %d", cap, len(msgs))
	}
	if msgs[0].Content != "x1" {
		t.Fatalf("expected x1 as oldest remaining, got %q", msgs[0].Content)
	}
}

// ─── VectorMemory ─────────────────────────────────────────────────────────────

func TestNewVectorMemory_Unbounded(t *testing.T) {
	m := NewVectorMemory(stubEmbed, 4096)
	for i := range 200 {
		_ = m.Add(msg("user", fmt.Sprintf("msg %d", i)))
	}
	if got := len(m.Messages()); got != 200 {
		t.Fatalf("unbounded VectorMemory: want 200 entries, got %d", got)
	}
}

func TestBoundedVectorMemory_CapEnforced(t *testing.T) {
	const cap = 10
	m := NewBoundedVectorMemory(stubEmbed, 4096, cap)
	for i := range 25 {
		_ = m.Add(msg("user", fmt.Sprintf("msg %d", i)))
	}
	if got := len(m.entries); got != cap {
		t.Fatalf("want %d entries after 25 adds, got %d", cap, got)
	}
}

func TestBoundedVectorMemory_FIFOEviction(t *testing.T) {
	const cap = 3
	m := NewBoundedVectorMemory(stubEmbed, 4096, cap)
	for i := range 5 {
		_ = m.Add(msg("user", fmt.Sprintf("vec%d", i)))
	}
	msgs := m.Messages()
	if len(msgs) != cap {
		t.Fatalf("want %d msgs, got %d", cap, len(msgs))
	}
	if msgs[0].Content != "vec2" {
		t.Fatalf("FIFO eviction: want vec2, got %q", msgs[0].Content)
	}
}

func TestBoundedVectorMemory_NewVectorMemoryUnbounded(t *testing.T) {
	// NewVectorMemory (no max) should remain unbounded.
	m := NewVectorMemory(stubEmbed, 1000)
	for i := range 150 {
		_ = m.Add(msg("user", fmt.Sprintf("e%d", i)))
	}
	if got := len(m.entries); got != 150 {
		t.Fatalf("NewVectorMemory should be unbounded, got %d entries", got)
	}
}

func TestBoundedVectorMemory_Clear(t *testing.T) {
	m := NewBoundedVectorMemory(stubEmbed, 4096, 5)
	_ = m.Add(msg("user", "data"))
	_ = m.Clear()
	if len(m.Messages()) != 0 {
		t.Fatal("expected empty after Clear")
	}
}

func TestVectorMemory_Compress_Noop(t *testing.T) {
	m := NewBoundedVectorMemory(stubEmbed, 4096, 10)
	_ = m.Add(msg("user", "test"))
	if err := m.Compress(context.Background()); err != nil {
		t.Fatalf("Compress should be noop, got error: %v", err)
	}
}

// ─── SlidingWindowMemory ─────────────────────────────────────────────────────

func TestSlidingWindowMemory_BelowWindow(t *testing.T) {
	m := NewSlidingWindowMemory(10)
	for i := range 5 {
		_ = m.Add(msg("user", fmt.Sprintf("sw%d", i)))
	}
	if got := len(m.Messages()); got != 5 {
		t.Fatalf("below window: want 5, got %d", got)
	}
}

func TestSlidingWindowMemory_ExactWindow(t *testing.T) {
	const win = 6
	m := NewSlidingWindowMemory(win)
	for i := range win {
		_ = m.Add(msg("user", fmt.Sprintf("w%d", i)))
	}
	if got := len(m.Messages()); got != win {
		t.Fatalf("exact window: want %d, got %d", win, got)
	}
}

func TestSlidingWindowMemory_KeepsLatest(t *testing.T) {
	const win = 4
	m := NewSlidingWindowMemory(win)
	for i := range 10 {
		_ = m.Add(msg("user", fmt.Sprintf("sw%d", i)))
	}
	msgs := m.Messages()
	if len(msgs) != win {
		t.Fatalf("want %d msgs, got %d", win, len(msgs))
	}
	// Should have sw6, sw7, sw8, sw9.
	if msgs[0].Content != "sw6" {
		t.Fatalf("want sw6 as oldest, got %q", msgs[0].Content)
	}
	if msgs[win-1].Content != "sw9" {
		t.Fatalf("want sw9 as newest, got %q", msgs[win-1].Content)
	}
}

func TestSlidingWindowMemory_Clear(t *testing.T) {
	m := NewSlidingWindowMemory(5)
	_ = m.Add(msg("user", "hello"))
	_ = m.Clear()
	if len(m.Messages()) != 0 {
		t.Fatal("expected empty after Clear")
	}
}

func TestSlidingWindowMemory_DefaultWindowSize(t *testing.T) {
	// windowSize ≤ 0 should default to 30.
	m := NewSlidingWindowMemory(0)
	for i := range 50 {
		_ = m.Add(msg("user", fmt.Sprintf("d%d", i)))
	}
	if got := len(m.Messages()); got != 30 {
		t.Fatalf("default window: want 30, got %d", got)
	}
}

func TestSlidingWindowMemory_MessagesCopy(t *testing.T) {
	m := NewSlidingWindowMemory(5)
	_ = m.Add(msg("user", "original"))
	msgs := m.Messages()
	// Mutate the returned slice — should not affect the internal state.
	msgs[0].Content = "mutated"
	if m.Messages()[0].Content != "original" {
		t.Fatal("Messages() should return a copy, not a reference")
	}
}

func TestSlidingWindowMemory_Compress_Noop(t *testing.T) {
	m := NewSlidingWindowMemory(5)
	_ = m.Add(msg("user", "data"))
	if err := m.Compress(context.Background()); err != nil {
		t.Fatalf("Compress should be noop, got error: %v", err)
	}
}

func TestSlidingWindowMemory_SatisfiesMemoryInterface(t *testing.T) {
	var _ Memory = NewSlidingWindowMemory(10)
}
