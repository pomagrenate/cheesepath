package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AutoCookies/crabpath/callback"
	"github.com/AutoCookies/crabpath/llm"
	"github.com/AutoCookies/crabpath/memory"
	"github.com/AutoCookies/crabpath/tools"
)

// Executor orchestrates a full agent run: strategy + tools + memory + callbacks.
type Executor struct {
	client   *llm.Client
	strategy Strategy
	registry *tools.Registry
	mem      memory.Memory
	cbs      callback.Handler
	model    string
	maxSteps int
}

// ExecutorOption configures an Executor.
type ExecutorOption func(*Executor)

func WithStrategy(s Strategy) ExecutorOption      { return func(e *Executor) { e.strategy = s } }
func WithMemory(m memory.Memory) ExecutorOption   { return func(e *Executor) { e.mem = m } }
func WithCallbacks(h callback.Handler) ExecutorOption { return func(e *Executor) { e.cbs = h } }
func WithModel(model string) ExecutorOption        { return func(e *Executor) { e.model = model } }
func WithMaxSteps(n int) ExecutorOption            { return func(e *Executor) { e.maxSteps = n } }

// NewExecutor creates an Executor with defaults: ReAct strategy, BufferMemory, 20 steps.
func NewExecutor(client *llm.Client, registry *tools.Registry, opts ...ExecutorOption) *Executor {
	e := &Executor{
		client:   client,
		strategy: NewReActStrategy(),
		registry: registry,
		mem:      memory.NewBufferMemory(),
		cbs:      callback.NoopHandler{},
		model:    "default",
		maxSteps: 20,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Run executes the agent loop for goal, streaming events on the returned channel.
// The channel is closed when the run ends. Drain it fully to avoid goroutine leaks.
func (e *Executor) Run(ctx context.Context, goal string) (<-chan StreamEvent, *CrabPath) {
	events := make(chan StreamEvent, 64)

	path := &CrabPath{
		ID:        fmt.Sprintf("crab-%d", time.Now().UnixNano()),
		Goal:      goal,
		Status:    PathRunning,
		StartedAt: time.Now(),
	}

	go func() {
		defer close(events)
		defer func() {
			now := time.Now()
			path.EndedAt = &now
		}()

		e.cbs.OnStart(goal)

		// Build tool description block for the system prompt.
		var toolDescs strings.Builder
		for _, t := range e.registry.All() {
			toolDescs.WriteString(fmt.Sprintf("- %s: %s\n", t.Name(), t.Description()))
		}

		// Seed history from memory + new system prompt.
		history := []llm.Message{
			{Role: "system", Content: e.strategy.BuildSystemPrompt(toolDescs.String())},
		}
		for _, m := range e.mem.Messages() {
			history = append(history, m)
		}
		history = append(history, llm.Message{
			Role:    "user",
			Content: "Goal: " + goal + "\n\nBegin reasoning.",
		})

		for step := 0; step < e.maxSteps; step++ {
			events <- StreamEvent{Type: EventThinking, Step: step, Payload: ""}

			req := llm.Request{
				Model:         e.model,
				Messages:      history,
				Grammar:       e.strategy.Grammar(),
				Temperature:   0.1,
				RepeatPenalty: 1.1,
			}

			tokenCh, fullCh, errCh := e.client.CompleteStream(ctx, req)

			var raw string
		drain:
			for {
				select {
				case tok, ok := <-tokenCh:
					if !ok {
						tokenCh = nil
					} else if tok != "" {
						events <- StreamEvent{Type: EventStreamToken, Step: step, Payload: tok}
						e.cbs.OnToken(step, tok)
					}
				case full, ok := <-fullCh:
					if ok {
						raw = full
					}
					fullCh = nil
				case err, ok := <-errCh:
					if ok && err != nil {
						events <- StreamEvent{Type: EventError, Step: step, Payload: err.Error()}
						e.cbs.OnError(err)
						path.Status = PathFailed
						return
					}
					errCh = nil
				case <-ctx.Done():
					events <- StreamEvent{Type: EventError, Step: step, Payload: "context cancelled"}
					path.Status = PathAborted
					return
				}
				if tokenCh == nil && fullCh == nil && errCh == nil {
					break drain
				}
			}

			if raw == "" {
				history = append(history, llm.Message{
					Role:    "user",
					Content: "Your response was empty. Please provide a JSON block as requested.",
				})
				continue
			}

			thought, toolCalls, err := e.strategy.ParseResponse(raw)
			if err != nil {
				history = append(history, llm.Message{
					Role:    "user",
					Content: "Your response was invalid JSON. Please provide exactly one JSON block.",
				})
				continue
			}

			e.cbs.OnThought(step, callback.ThoughtEvent{
				Reasoning:   thought.Reasoning,
				Plan:        thought.Plan,
				IsFinal:     thought.IsFinal,
				FinalAnswer: thought.FinalAnswer,
			})
			events <- StreamEvent{Type: EventThought, Step: step, Payload: thought}

			if thought.IsFinal {
				path.Answer = thought.FinalAnswer
				path.Status = PathCompleted
				events <- StreamEvent{Type: EventFinalAnswer, Step: step, Payload: thought.FinalAnswer}
				e.cbs.OnFinalAnswer(thought.FinalAnswer)
				_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
				return
			}

			// Execute tool calls
			crabStep := CrabStep{Index: step, Thought: thought, ToolCalls: toolCalls}
			var observations []string

			for i, tc := range toolCalls {
				tool, ok := e.registry.Get(tc.ToolName)
				if !ok {
					crabStep.ToolCalls[i].Error = "unknown tool: " + tc.ToolName
					observations = append(observations, fmt.Sprintf("[%s]: ERROR — unknown tool", tc.ToolName))
					continue
				}

				crabStep.ToolCalls[i].Dangerous = tool.Dangerous()
				e.cbs.OnToolCall(step, callback.ToolCallEvent{
					ToolName:  tc.ToolName,
					Args:      tc.Args,
					Dangerous: tool.Dangerous(),
				})

				if tool.Dangerous() {
					events <- StreamEvent{Type: EventApprovalReq, Step: step, Payload: tc}
				}

				if tc.ToolName == "crabtable" {
					events <- StreamEvent{Type: EventCrabTableReq, Step: step, Payload: tc}
				}

				result, execErr := tool.Execute(ctx, tc.Args)
				if execErr != nil {
					crabStep.ToolCalls[i].Error = execErr.Error()
					observations = append(observations, fmt.Sprintf("[%s]: ERROR — %v", tc.ToolName, execErr))
				} else {
					crabStep.ToolCalls[i].Result = result
					observations = append(observations, fmt.Sprintf("[%s]: %s", tc.ToolName, result))
				}
			}

			obs := strings.Join(observations, "\n")
			crabStep.Observation = obs
			path.Steps = append(path.Steps, crabStep)

			events <- StreamEvent{Type: EventObservation, Step: step, Payload: obs}
			e.cbs.OnObservation(step, obs)

			history = append(history,
				llm.Message{Role: "assistant", Content: raw},
				llm.Message{Role: "user", Content: "Observation:\n" + obs + "\n\nContinue reasoning toward the goal."},
			)
			_ = e.mem.Add(llm.Message{Role: "assistant", Content: raw})
		}

		path.Status = PathFailed
		events <- StreamEvent{
			Type:    EventError,
			Step:    e.maxSteps,
			Payload: "max steps exceeded — path abandoned",
		}
		e.cbs.OnError(fmt.Errorf("max steps exceeded"))
	}()

	return events, path
}
