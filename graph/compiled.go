package graph

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CompileOption customizes graph compilation.
type CompileOption func(any)

type compileConfig[S any] struct {
	checkpointer      Checkpointer[S]
	maxRecursionLimit int
	interruptBefore   map[string]bool
	interruptAfter    map[string]bool
}

// WithCheckpointer attaches a Checkpointer for state persistence and time-travel.
func WithCheckpointer[S any](cp Checkpointer[S]) CompileOption {
	return func(cfg any) {
		if c, ok := cfg.(*compileConfig[S]); ok {
			c.checkpointer = cp
		}
	}
}

// WithMaxRecursionLimit sets the maximum number of super-steps before failing.
func WithMaxRecursionLimit[S any](limit int) CompileOption {
	return func(cfg any) {
		if c, ok := cfg.(*compileConfig[S]); ok {
			c.maxRecursionLimit = limit
		}
	}
}

// WithInterruptBefore pauses execution immediately before the specified nodes execute.
func WithInterruptBefore[S any](nodeNames ...string) CompileOption {
	return func(cfg any) {
		if c, ok := cfg.(*compileConfig[S]); ok {
			for _, name := range nodeNames {
				c.interruptBefore[name] = true
			}
		}
	}
}

// WithInterruptAfter pauses execution immediately after the specified nodes execute.
func WithInterruptAfter[S any](nodeNames ...string) CompileOption {
	return func(cfg any) {
		if c, ok := cfg.(*compileConfig[S]); ok {
			for _, name := range nodeNames {
				c.interruptAfter[name] = true
			}
		}
	}
}

// RunConfig configures an individual graph invocation.
type RunConfig struct {
	ThreadID            string
	StreamMode          StreamMode
	ActiveNodes         []string
	SkipInterruptBefore string
	StepOffset          int
}

// RunOption configures graph execution.
type RunOption func(*RunConfig)

// WithThreadID sets the thread identifier for checkpoint persistence and resumption.
func WithThreadID(threadID string) RunOption {
	return func(c *RunConfig) { c.ThreadID = threadID }
}

// WithStreamMode specifies whether streaming emits values or updates.
func WithStreamMode(mode StreamMode) RunOption {
	return func(c *RunConfig) { c.StreamMode = mode }
}

// WithActiveNodes overrides the initial active nodes (used for resuming).
func WithActiveNodes(nodes ...string) RunOption {
	return func(c *RunConfig) { c.ActiveNodes = nodes }
}

// WithSkipInterruptBefore bypasses interruptBefore for a node on the first step (used for resuming).
func WithSkipInterruptBefore(nodeName string) RunOption {
	return func(c *RunConfig) { c.SkipInterruptBefore = nodeName }
}

// CompiledGraph is the executable, compiled state machine.
type CompiledGraph[S any] struct {
	nodes             map[string]NodeFunc[S]
	nodePolicies      map[string]*RetryPolicy
	nodeFallbacks     map[string]string
	nodeGuardrails    map[string][]Guardrail[S]
	edges             map[string][]string
	conditionalEdges  map[string][]ConditionalEdge[S]
	entryPoint        string
	finishPoints      map[string]bool
	reducer           Reducer[S]
	checkpointer      Checkpointer[S]
	maxRecursionLimit int
	interruptBefore   map[string]bool
	interruptAfter    map[string]bool
}

type nodeResult[S any] struct {
	name   string
	update S
	err    error
}

func (g *CompiledGraph[S]) executeNode(ctx context.Context, name string, currentState S) nodeResult[S] {
	fn, ok := g.nodes[name]
	if !ok {
		return nodeResult[S]{name: name, err: fmt.Errorf("node %q not found", name)}
	}

	policy := g.nodePolicies[name]
	up, err := ExecuteWithRetry(ctx, policy, fn, currentState)
	if err != nil {
		if fb, hasFb := g.nodeFallbacks[name]; hasFb && g.nodes[fb] != nil {
			fbFn := g.nodes[fb]
			fbUp, fbErr := fbFn(ctx, currentState)
			if fbErr == nil {
				return nodeResult[S]{name: fb, update: fbUp, err: nil}
			}
		}
		return nodeResult[S]{name: name, update: up, err: err}
	}

	// Validate Guardrails
	if rails, hasRails := g.nodeGuardrails[name]; hasRails {
		for _, rail := range rails {
			if vErr := rail.Validate(ctx, currentState, up); vErr != nil {
				if rail.OnViolation == GuardrailActionFallback && rail.FallbackNode != "" && g.nodes[rail.FallbackNode] != nil {
					fbFn := g.nodes[rail.FallbackNode]
					fbUp, fbErr := fbFn(ctx, currentState)
					if fbErr == nil {
						return nodeResult[S]{name: rail.FallbackNode, update: fbUp, err: nil}
					}
				}
				return nodeResult[S]{
					name: name,
					err: &GuardrailViolationError[S]{
						GuardrailName: rail.Name,
						NodeName:      name,
						Reason:        vErr,
						CurrentState:  currentState,
						UpdateState:   up,
					},
				}
			}
		}
	}

	return nodeResult[S]{name: name, update: up, err: nil}
}

// Invoke executes the graph from start to completion.
func (g *CompiledGraph[S]) Invoke(ctx context.Context, initialState S, opts ...RunOption) (S, error) {
	events, err := g.Stream(ctx, initialState, opts...)
	if err != nil {
		return initialState, err
	}

	var lastState S = initialState
	var lastErr error

	for ev := range events {
		if ev.Error != nil {
			lastErr = ev.Error
		}
		lastState = ev.Values
	}

	return lastState, lastErr
}

// Stream executes the graph and yields events over a channel.
func (g *CompiledGraph[S]) Stream(ctx context.Context, initialState S, opts ...RunOption) (<-chan StreamEvent[S], error) {
	cfg := RunConfig{
		StreamMode: StreamModeValues,
	}
	for _, o := range opts {
		o(&cfg)
	}

	outCh := make(chan StreamEvent[S], 64)

	go func() {
		defer close(outCh)

		currentState := initialState
		activeNodes := []string{g.entryPoint}
		if len(cfg.ActiveNodes) > 0 {
			activeNodes = cfg.ActiveNodes
		}
		step := cfg.StepOffset
		threadID := cfg.ThreadID
		if threadID == "" {
			threadID = fmt.Sprintf("thread-%d", time.Now().UnixNano())
		}
		var parentID string
		skipInterruptBefore := cfg.SkipInterruptBefore

		for step < g.maxRecursionLimit {
			// Filter out END nodes
			currentActive := make([]string, 0, len(activeNodes))
			for _, n := range activeNodes {
				if n != END && n != "" {
					currentActive = append(currentActive, n)
				}
			}

			if len(currentActive) == 0 {
				// Graph reached completion
				return
			}

			// Check interrupt before
			for _, n := range currentActive {
				if g.interruptBefore[n] {
					if step == cfg.StepOffset && n == skipInterruptBefore {
						// Skip interrupt on resume step
						continue
					}
					cpID := fmt.Sprintf("cp-%d-%d", step, time.Now().UnixNano())
					if g.checkpointer != nil {
						_ = g.checkpointer.Put(ctx, &Checkpoint[S]{
							ThreadID:     threadID,
							CheckpointID: cpID,
							ParentID:     parentID,
							Node:         n,
							Step:         step,
							State:        currentState,
							CreatedAt:    time.Now().UTC(),
						})
					}
					intErr := &InterruptError[S]{
						Node:         n,
						When:         "before",
						State:        currentState,
						ThreadID:     threadID,
						CheckpointID: cpID,
					}
					outCh <- StreamEvent[S]{
						Step:        step,
						Node:        n,
						Values:      currentState,
						Interrupted: true,
						Error:       intErr,
					}
					return
				}
			}

			// Execute active nodes (in parallel if multiple)
			results := make([]nodeResult[S], len(currentActive))
			if len(currentActive) == 1 {
				results[0] = g.executeNode(ctx, currentActive[0], currentState)
			} else {
				var wg sync.WaitGroup
				for i, n := range currentActive {
					wg.Add(1)
					go func(idx int, nodeName string) {
						defer wg.Done()
						results[idx] = g.executeNode(ctx, nodeName, currentState)
					}(i, n)
				}
				wg.Wait()
			}

			// Handle errors and apply reducers
			for _, res := range results {
				if res.err != nil {
					outCh <- StreamEvent[S]{
						Step:   step,
						Node:   res.name,
						Values: currentState,
						Error:  res.err,
					}
					return
				}
				currentState = g.reducer(currentState, res.update)

				if cfg.StreamMode == StreamModeUpdates {
					outCh <- StreamEvent[S]{
						Step:   step,
						Node:   res.name,
						Values: currentState,
						Update: res.update,
					}
				}
			}

			cpID := fmt.Sprintf("cp-%d-%d", step, time.Now().UnixNano())
			if g.checkpointer != nil {
				_ = g.checkpointer.Put(ctx, &Checkpoint[S]{
					ThreadID:     threadID,
					CheckpointID: cpID,
					ParentID:     parentID,
					Node:         results[len(results)-1].name,
					Step:         step,
					State:        currentState,
					CreatedAt:    time.Now().UTC(),
				})
			}
			parentID = cpID

			if cfg.StreamMode == StreamModeValues {
				outCh <- StreamEvent[S]{
					Step:   step,
					Node:   results[len(results)-1].name,
					Values: currentState,
				}
			}

			// Check interrupt after
			for _, res := range results {
				if g.interruptAfter[res.name] {
					intErr := &InterruptError[S]{
						Node:         res.name,
						When:         "after",
						State:        currentState,
						ThreadID:     threadID,
						CheckpointID: cpID,
					}
					outCh <- StreamEvent[S]{
						Step:        step,
						Node:        res.name,
						Values:      currentState,
						Interrupted: true,
						Error:       intErr,
					}
					return
				}
			}

			// Transition to next active nodes
			nextMap := make(map[string]bool)
			for _, res := range results {
				// Finish point check
				if g.finishPoints[res.name] {
					continue
				}

				// Direct edges
				for _, to := range g.edges[res.name] {
					if to != END {
						nextMap[to] = true
					}
				}

				// Conditional edges
				for _, ce := range g.conditionalEdges[res.name] {
					target, err := ce.Route(ctx, currentState)
					if err != nil {
						outCh <- StreamEvent[S]{
							Step:   step,
							Node:   res.name,
							Values: currentState,
							Error:  fmt.Errorf("conditional edge from %q: %w", res.name, err),
						}
						return
					}
					if target != END && target != "" {
						nextMap[target] = true
					}
				}
			}

			activeNodes = make([]string, 0, len(nextMap))
			for nodeName := range nextMap {
				activeNodes = append(activeNodes, nodeName)
			}

			step++
		}

		outCh <- StreamEvent[S]{
			Step:   step,
			Values: currentState,
			Error:  fmt.Errorf("graph recursion limit %d exceeded", g.maxRecursionLimit),
		}
	}()

	return outCh, nil
}

// Resume reloads an interrupted thread checkpoint, applies resumeUpdate, and resumes execution.
func (g *CompiledGraph[S]) Resume(ctx context.Context, threadID string, resumeUpdate S, opts ...RunOption) (S, error) {
	if g.checkpointer == nil {
		var zero S
		return zero, fmt.Errorf("cannot resume: graph has no checkpointer configured")
	}

	latest, err := g.checkpointer.Get(ctx, threadID)
	if err != nil {
		var zero S
		return zero, fmt.Errorf("failed to retrieve checkpoint for thread %q: %w", threadID, err)
	}

	resumedState := g.reducer(latest.State, resumeUpdate)

	runOpts := []RunOption{
		WithThreadID(threadID),
		WithActiveNodes(latest.Node),
		WithSkipInterruptBefore(latest.Node),
	}
	runOpts = append(runOpts, opts...)
	return g.Invoke(ctx, resumedState, runOpts...)
}
