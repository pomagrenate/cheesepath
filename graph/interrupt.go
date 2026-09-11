package graph

import (
	"errors"
	"fmt"
)

// ErrInterrupt is the base sentinel error when graph execution halts at an interrupt.
var ErrInterrupt = errors.New("graph execution interrupted")

// InterruptError captures execution context when an interrupt occurs.
type InterruptError[S any] struct {
	Node         string
	When         string // "before" | "after"
	State        S
	ThreadID     string
	CheckpointID string
}

func (e *InterruptError[S]) Error() string {
	return fmt.Sprintf("graph execution interrupted %s node %q (thread: %s, checkpoint: %s)",
		e.When, e.Node, e.ThreadID, e.CheckpointID)
}

func (e *InterruptError[S]) Unwrap() error {
	return ErrInterrupt
}

// IsInterrupt checks whether an error was caused by a human-in-the-loop interrupt.
func IsInterrupt(err error) bool {
	return errors.Is(err, ErrInterrupt)
}
