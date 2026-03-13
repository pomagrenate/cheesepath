package chain

import (
	"context"
	"fmt"
)

// Step is a single labelled transformation within a SequentialChain.
type Step struct {
	Name string
	Fn   func(ctx context.Context, vars map[string]any) (map[string]any, error)
}

// SequentialChain runs a list of Steps in order, merging their outputs into a
// shared variable map that each subsequent step can read.
type SequentialChain struct {
	steps []Step
}

// NewSequentialChain builds a chain from a list of Steps.
func NewSequentialChain(steps ...Step) *SequentialChain {
	return &SequentialChain{steps: steps}
}

// Run executes all steps in order.
func (s *SequentialChain) Run(ctx context.Context, vars map[string]any) (map[string]any, error) {
	for i, step := range s.steps {
		out, err := step.Fn(ctx, vars)
		if err != nil {
			return vars, fmt.Errorf("sequential: step[%d] %q: %w", i, step.Name, err)
		}
		for k, v := range out {
			vars[k] = v
		}
	}
	return vars, nil
}
