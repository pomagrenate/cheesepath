package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Checkpoint captures the state of a graph execution at a specific super-step.
type Checkpoint[S any] struct {
	ThreadID     string    `json:"thread_id"`
	CheckpointID string    `json:"checkpoint_id"`
	ParentID     string    `json:"parent_id,omitempty"`
	Node         string    `json:"node,omitempty"`
	Step         int       `json:"step"`
	State        S         `json:"state"`
	CreatedAt    time.Time `json:"created_at"`
}

// Checkpointer persists and retrieves graph state snapshots.
type Checkpointer[S any] interface {
	// Get retrieves the latest checkpoint for a thread.
	Get(ctx context.Context, threadID string) (*Checkpoint[S], error)
	// GetTuple retrieves a specific checkpoint version by checkpoint ID.
	GetTuple(ctx context.Context, threadID string, checkpointID string) (*Checkpoint[S], error)
	// List returns all checkpoints for a thread ordered from earliest to latest.
	List(ctx context.Context, threadID string) ([]*Checkpoint[S], error)
	// Put saves a new checkpoint snapshot.
	Put(ctx context.Context, cp *Checkpoint[S]) error
}

// MemorySaver is an in-memory, thread-safe implementation of Checkpointer.
type MemorySaver[S any] struct {
	mu          sync.RWMutex
	checkpoints map[string][]*Checkpoint[S]
}

// NewMemorySaver creates an in-memory checkpointer.
func NewMemorySaver[S any]() *MemorySaver[S] {
	return &MemorySaver[S]{
		checkpoints: make(map[string][]*Checkpoint[S]),
	}
}

func (m *MemorySaver[S]) Get(_ context.Context, threadID string) (*Checkpoint[S], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list, ok := m.checkpoints[threadID]
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("no checkpoints found for thread %q", threadID)
	}
	return list[len(list)-1], nil
}

func (m *MemorySaver[S]) GetTuple(_ context.Context, threadID string, checkpointID string) (*Checkpoint[S], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list, ok := m.checkpoints[threadID]
	if !ok {
		return nil, fmt.Errorf("no checkpoints found for thread %q", threadID)
	}
	for _, cp := range list {
		if cp.CheckpointID == checkpointID {
			return cp, nil
		}
	}
	return nil, fmt.Errorf("checkpoint %q not found for thread %q", checkpointID, threadID)
}

func (m *MemorySaver[S]) List(_ context.Context, threadID string) ([]*Checkpoint[S], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list, ok := m.checkpoints[threadID]
	if !ok {
		return nil, nil
	}
	out := make([]*Checkpoint[S], len(list))
	copy(out, list)
	return out, nil
}

func (m *MemorySaver[S]) Put(_ context.Context, cp *Checkpoint[S]) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkpoints[cp.ThreadID] = append(m.checkpoints[cp.ThreadID], cp)
	return nil
}

// FileSaver persists checkpoints to disk as JSON files under a directory.
type FileSaver[S any] struct {
	mu      sync.RWMutex
	baseDir string
}

// NewFileSaver creates a file-backed checkpointer.
func NewFileSaver[S any](baseDir string) (*FileSaver[S], error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("filesaver: failed to create base dir: %w", err)
	}
	return &FileSaver[S]{baseDir: baseDir}, nil
}

func (f *FileSaver[S]) threadDir(threadID string) string {
	return filepath.Join(f.baseDir, threadID)
}

func (f *FileSaver[S]) Put(_ context.Context, cp *Checkpoint[S]) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	tdir := f.threadDir(cp.ThreadID)
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		return fmt.Errorf("filesaver: create thread dir: %w", err)
	}
	filename := fmt.Sprintf("%06d_%s.json", cp.Step, cp.CheckpointID)
	filePath := filepath.Join(tdir, filename)

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("filesaver: marshal checkpoint: %w", err)
	}
	return os.WriteFile(filePath, data, 0o644)
}

func (f *FileSaver[S]) List(_ context.Context, threadID string) ([]*Checkpoint[S], error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	tdir := f.threadDir(threadID)
	entries, err := os.ReadDir(tdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("filesaver: read thread dir: %w", err)
	}

	var list []*Checkpoint[S]
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tdir, entry.Name()))
		if err != nil {
			continue
		}
		var cp Checkpoint[S]
		if err := json.Unmarshal(data, &cp); err == nil {
			list = append(list, &cp)
		}
	}
	return list, nil
}

func (f *FileSaver[S]) Get(ctx context.Context, threadID string) (*Checkpoint[S], error) {
	list, err := f.List(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no checkpoints found for thread %q", threadID)
	}
	return list[len(list)-1], nil
}

func (f *FileSaver[S]) GetTuple(ctx context.Context, threadID string, checkpointID string) (*Checkpoint[S], error) {
	list, err := f.List(ctx, threadID)
	if err != nil {
		return nil, err
	}
	for _, cp := range list {
		if cp.CheckpointID == checkpointID {
			return cp, nil
		}
	}
	return nil, fmt.Errorf("checkpoint %q not found for thread %q", checkpointID, threadID)
}
