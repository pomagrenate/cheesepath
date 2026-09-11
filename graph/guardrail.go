package graph

import (
	"context"
	"fmt"
)

// GuardrailAction defines what to do when a guardrail validation fails.
type GuardrailAction int

const (
	// GuardrailActionHalt halts execution immediately and emits a GuardrailViolationError.
	GuardrailActionHalt GuardrailAction = iota
	// GuardrailActionFallback diverts execution to a designated fallback node.
	GuardrailActionFallback
)

// GuardrailViolationError is emitted when a node's output violates a guardrail.
type GuardrailViolationError[S any] struct {
	GuardrailName string
	NodeName      string
	Reason        error
	CurrentState  S
	UpdateState   S
}

func (e *GuardrailViolationError[S]) Error() string {
	return fmt.Sprintf("guardrail violation %q at node %q: %v", e.GuardrailName, e.NodeName, e.Reason)
}

func (e *GuardrailViolationError[S]) Unwrap() error {
	return e.Reason
}

// Guardrail validates node state updates before they are merged into graph state.
type Guardrail[S any] struct {
	Name         string
	Validate     func(ctx context.Context, state S, update S) error
	OnViolation  GuardrailAction
	FallbackNode string
}

// NewGuardrail creates a new Guardrail.
func NewGuardrail[S any](
	name string,
	validate func(ctx context.Context, state S, update S) error,
	action GuardrailAction,
	fallbackNode ...string,
) Guardrail[S] {
	fb := ""
	if len(fallbackNode) > 0 {
		fb = fallbackNode[0]
	}
	return Guardrail[S]{
		Name:         name,
		Validate:     validate,
		OnViolation:  action,
		FallbackNode: fb,
	}
}
