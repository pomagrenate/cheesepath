package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

type threadKeyType struct{}

var threadKey = threadKeyType{}

// WithThreadID injects a thread identifier into the context for idempotent tool execution.
func WithThreadID(ctx context.Context, threadID string) context.Context {
	return context.WithValue(ctx, threadKey, threadID)
}

// GetThreadID retrieves the thread identifier from the context, if present.
func GetThreadID(ctx context.Context) string {
	if val, ok := ctx.Value(threadKey).(string); ok {
		return val
	}
	return ""
}

// IdempotencyStore persists execution outputs of side-effect operations to prevent double-execution.
type IdempotencyStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, output string) error
}

// MemoryIdempotencyStore is an in-memory thread-safe IdempotencyStore.
type MemoryIdempotencyStore struct {
	mu    sync.RWMutex
	cache map[string]string
}

// NewMemoryIdempotencyStore creates a new MemoryIdempotencyStore.
func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{
		cache: make(map[string]string),
	}
}

func (s *MemoryIdempotencyStore) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.cache[key]
	return val, ok, nil
}

func (s *MemoryIdempotencyStore) Set(_ context.Context, key string, output string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[key] = output
	return nil
}

// ComputeIdempotencyKey creates a deterministic SHA-256 hash key for a tool invocation.
func ComputeIdempotencyKey(threadID, toolName string, args map[string]any) string {
	data, _ := json.Marshal(args)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%s:%s:%s", threadID, toolName, hex.EncodeToString(hash[:8]))
}

// IdempotentToolRegistry wraps a ToolRegistry with automatic side-effect deduplication.
type IdempotentToolRegistry struct {
	base  *ToolRegistry
	store IdempotencyStore
}

// NewIdempotentToolRegistry creates a new IdempotentToolRegistry.
func NewIdempotentToolRegistry(base *ToolRegistry, store ...IdempotencyStore) *IdempotentToolRegistry {
	var s IdempotencyStore = NewMemoryIdempotencyStore()
	if len(store) > 0 && store[0] != nil {
		s = store[0]
	}
	return &IdempotentToolRegistry{
		base:  base,
		store: s,
	}
}

// Register delegates to the underlying ToolRegistry.
func (r *IdempotentToolRegistry) Register(tool Tool) {
	r.base.Register(tool)
}

// Tools delegates to the underlying ToolRegistry.
func (r *IdempotentToolRegistry) Tools() []Tool {
	return r.base.All()
}

// All delegates to the underlying ToolRegistry.
func (r *IdempotentToolRegistry) All() []Tool {
	return r.base.All()
}

// ExecuteIdempotent executes a tool with idempotency guarantees.
// Returns (output, wasCached, error).
func (r *IdempotentToolRegistry) ExecuteIdempotent(ctx context.Context, threadID string, name string, args map[string]any) (string, bool, error) {
	if threadID == "" {
		threadID = GetThreadID(ctx)
	}

	// If no thread ID is provided, execute without idempotency caching
	if threadID == "" {
		out, err := r.base.Execute(ctx, name, args)
		return out, false, err
	}

	key := ComputeIdempotencyKey(threadID, name, args)

	// Check if already executed
	if cached, found, err := r.store.Get(ctx, key); err == nil && found {
		return cached, true, nil
	}

	// Execute tool
	output, err := r.base.Execute(ctx, name, args)
	if err != nil {
		return "", false, err
	}

	// Cache successful result
	_ = r.store.Set(ctx, key, output)
	return output, false, nil
}
