package graph

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Standard channel errors matching LangGraph channel exceptions.
var (
	ErrEmptyChannel  = errors.New("channel is empty")
	ErrInvalidUpdate = errors.New("invalid channel update")
)

// Reducer defines how an update returned by a node is merged into the current state.
type Reducer[S any] func(current S, update S) S

// NodeFunc is the execution signature of a graph node.
// It receives the current state and returns a state update (or full updated state).
type NodeFunc[S any] func(ctx context.Context, state S) (S, error)

// OverwriteReducer simply replaces the current state with the update.
func OverwriteReducer[T any](_ T, update T) T {
	return update
}

// AppendSliceReducer appends items from update to current slice.
func AppendSliceReducer[T any](current []T, update []T) []T {
	return append(current, update...)
}

// ============================================================================
// LangGraph Channel Abstractions (1:1 Parity with langgraph/channels/)
// ============================================================================

// Channel defines the core channel interface matching langgraph.channels.base.BaseChannel.
type Channel[V any] interface {
	// Get returns the current value of the channel, or ErrEmptyChannel if empty.
	Get() (V, error)
	// IsAvailable returns true if the channel has a value available.
	IsAvailable() bool
	// Update applies a sequence of updates produced during a Pregel super-step.
	// Returns true if the channel value changed.
	Update(values []V) (bool, error)
	// Checkpoint returns a serializable snapshot of the channel.
	Checkpoint() any
	// FromCheckpoint restores or creates an identical channel from a checkpoint snapshot.
	FromCheckpoint(cp any) Channel[V]
	// Consume notifies the channel that a subscribed task ran.
	Consume() bool
	// Finish notifies the channel that the Pregel run is finishing.
	Finish() bool
}

// ----------------------------------------------------------------------------
// LastValueChannel: stores the last value received.
// Enforces at most one value per step (matching langgraph/channels/last_value.py).
// ----------------------------------------------------------------------------

type LastValueChannel[V any] struct {
	mu        sync.RWMutex
	key       string
	value     V
	available bool
}

// NewLastValueChannel creates a LastValueChannel.
func NewLastValueChannel[V any](key string) *LastValueChannel[V] {
	return &LastValueChannel[V]{key: key}
}

func (c *LastValueChannel[V]) Get() (V, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.available {
		var zero V
		return zero, ErrEmptyChannel
	}
	return c.value, nil
}

func (c *LastValueChannel[V]) IsAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.available
}

func (c *LastValueChannel[V]) Update(values []V) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(values) == 0 {
		return false, nil
	}
	if len(values) != 1 {
		return false, fmt.Errorf("%w: at key %q: can receive only one value per step, got %d", ErrInvalidUpdate, c.key, len(values))
	}
	c.value = values[0]
	c.available = true
	return true, nil
}

func (c *LastValueChannel[V]) Checkpoint() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.available {
		return nil
	}
	return c.value
}

func (c *LastValueChannel[V]) FromCheckpoint(cp any) Channel[V] {
	newCh := NewLastValueChannel[V](c.key)
	if cp != nil {
		if val, ok := cp.(V); ok {
			newCh.value = val
			newCh.available = true
		}
	}
	return newCh
}

func (c *LastValueChannel[V]) Consume() bool { return false }
func (c *LastValueChannel[V]) Finish() bool  { return false }

// ----------------------------------------------------------------------------
// BinOpChannel: applies a binary operator aggregate (current, update) -> value.
// Matching langgraph/channels/binop.py.
// ----------------------------------------------------------------------------

type BinOpChannel[V any] struct {
	mu        sync.RWMutex
	key       string
	operator  func(current, update V) V
	value     V
	available bool
}

// NewBinOpChannel creates a BinOpChannel with a specified reducer operator.
func NewBinOpChannel[V any](key string, operator func(current, update V) V) *BinOpChannel[V] {
	return &BinOpChannel[V]{
		key:      key,
		operator: operator,
	}
}

func (c *BinOpChannel[V]) Get() (V, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.available {
		var zero V
		return zero, ErrEmptyChannel
	}
	return c.value, nil
}

func (c *BinOpChannel[V]) IsAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.available
}

func (c *BinOpChannel[V]) Update(values []V) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(values) == 0 {
		return false, nil
	}

	startIdx := 0
	if !c.available {
		c.value = values[0]
		c.available = true
		startIdx = 1
	}

	for i := startIdx; i < len(values); i++ {
		c.value = c.operator(c.value, values[i])
	}
	return true, nil
}

func (c *BinOpChannel[V]) Checkpoint() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.available {
		return nil
	}
	return c.value
}

func (c *BinOpChannel[V]) FromCheckpoint(cp any) Channel[V] {
	newCh := NewBinOpChannel[V](c.key, c.operator)
	if cp != nil {
		if val, ok := cp.(V); ok {
			newCh.value = val
			newCh.available = true
		}
	}
	return newCh
}

func (c *BinOpChannel[V]) Consume() bool { return false }
func (c *BinOpChannel[V]) Finish() bool  { return false }

// ----------------------------------------------------------------------------
// TopicChannel: configurable PubSub topic channel.
// Supports per-step clearing or accumulation (matching langgraph/channels/topic.py).
// ----------------------------------------------------------------------------

type TopicChannel[V any] struct {
	mu         sync.RWMutex
	key        string
	accumulate bool
	values     []V
}

// NewTopicChannel creates a TopicChannel.
func NewTopicChannel[V any](key string, accumulate bool) *TopicChannel[V] {
	return &TopicChannel[V]{
		key:        key,
		accumulate: accumulate,
		values:     make([]V, 0),
	}
}

func (c *TopicChannel[V]) Get() ([]V, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.values) == 0 {
		return nil, ErrEmptyChannel
	}
	out := make([]V, len(c.values))
	copy(out, c.values)
	return out, nil
}

func (c *TopicChannel[V]) IsAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.values) > 0
}

func (c *TopicChannel[V]) Update(values []V) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	updated := false
	if !c.accumulate {
		if len(c.values) > 0 {
			updated = true
		}
		c.values = make([]V, 0, len(values))
	}
	if len(values) > 0 {
		c.values = append(c.values, values...)
		updated = true
	}
	return updated, nil
}

func (c *TopicChannel[V]) Checkpoint() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]V, len(c.values))
	copy(out, c.values)
	return out
}

func (c *TopicChannel[V]) FromCheckpoint(cp any) Channel[[]V] {
	// Not directly used; implements Channel[V] conceptually
	return nil
}

func (c *TopicChannel[V]) Consume() bool { return false }
func (c *TopicChannel[V]) Finish() bool  { return false }

// ----------------------------------------------------------------------------
// EphemeralChannel: stores value received in immediate previous step, clears after.
// Matching langgraph/channels/ephemeral_value.py.
// ----------------------------------------------------------------------------

type EphemeralChannel[V any] struct {
	mu        sync.RWMutex
	key       string
	guard     bool
	value     V
	available bool
}

// NewEphemeralChannel creates an EphemeralChannel.
func NewEphemeralChannel[V any](key string, guard bool) *EphemeralChannel[V] {
	return &EphemeralChannel[V]{
		key:   key,
		guard: guard,
	}
}

func (c *EphemeralChannel[V]) Get() (V, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.available {
		var zero V
		return zero, ErrEmptyChannel
	}
	return c.value, nil
}

func (c *EphemeralChannel[V]) IsAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.available
}

func (c *EphemeralChannel[V]) Update(values []V) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(values) == 0 {
		if c.available {
			c.available = false
			var zero V
			c.value = zero
			return true, nil
		}
		return false, nil
	}

	if len(values) != 1 && c.guard {
		return false, fmt.Errorf("%w: at key %q: EphemeralChannel(guard=true) can receive only one value per step", ErrInvalidUpdate, c.key)
	}

	c.value = values[len(values)-1]
	c.available = true
	return true, nil
}

func (c *EphemeralChannel[V]) Checkpoint() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.available {
		return nil
	}
	return c.value
}

func (c *EphemeralChannel[V]) FromCheckpoint(cp any) Channel[V] {
	newCh := NewEphemeralChannel[V](c.key, c.guard)
	if cp != nil {
		if val, ok := cp.(V); ok {
			newCh.value = val
			newCh.available = true
		}
	}
	return newCh
}

func (c *EphemeralChannel[V]) Consume() bool { return false }
func (c *EphemeralChannel[V]) Finish() bool  { return false }
