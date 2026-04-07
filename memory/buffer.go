package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/AutoCookies/crabpath/llm"
)

// BufferMemory is an in-memory conversation buffer.
// When maxSize > 0, Add evicts the oldest message (FIFO) before appending once
// the cap is reached, keeping memory bounded.
type BufferMemory struct {
	mu      sync.RWMutex
	msgs    []llm.Message
	maxSize int // 0 = unlimited
}

func NewBufferMemory() *BufferMemory { return &BufferMemory{} }

// NewBoundedBufferMemory creates a BufferMemory that retains at most maxSize
// messages, evicting the oldest when full.
func NewBoundedBufferMemory(maxSize int) *BufferMemory {
	return &BufferMemory{maxSize: maxSize}
}

func (b *BufferMemory) Add(msg llm.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.maxSize > 0 && len(b.msgs) >= b.maxSize {
		b.msgs = b.msgs[1:] // evict oldest
	}
	b.msgs = append(b.msgs, msg)
	return nil
}

func (b *BufferMemory) Messages() []llm.Message {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]llm.Message, len(b.msgs))
	copy(out, b.msgs)
	return out
}

func (b *BufferMemory) Compress(_ context.Context) error { return nil }

func (b *BufferMemory) Clear() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgs = nil
	return nil
}

// FileMemory is a buffer memory that persists to a JSON file.
type FileMemory struct {
	mu   sync.RWMutex
	path string
	msgs []llm.Message
}

func NewFileMemory(path string) (*FileMemory, error) {
	fm := &FileMemory{path: path}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &fm.msgs); err != nil {
			return nil, fmt.Errorf("memory: load %s: %w", path, err)
		}
	}
	return fm, nil
}

func (f *FileMemory) Add(msg llm.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg)
	return f.save()
}

func (f *FileMemory) Messages() []llm.Message {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]llm.Message, len(f.msgs))
	copy(out, f.msgs)
	return out
}

func (f *FileMemory) Compress(_ context.Context) error { return nil }

func (f *FileMemory) Clear() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = nil
	return f.save()
}

func (f *FileMemory) save() error {
	data, err := json.MarshalIndent(f.msgs, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: marshal: %w", err)
	}
	return os.WriteFile(f.path, data, 0o644)
}
