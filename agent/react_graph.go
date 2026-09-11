package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/AutoCookies/cheesepath/core"
	"github.com/AutoCookies/cheesepath/graph"
)

// ReactState represents the conversation state inside a ReAct agent graph.
type ReactState struct {
	Messages []core.Message `json:"messages"`
}

// ReactStateReducer merges updates by appending new messages to the existing history.
func ReactStateReducer(current ReactState, update ReactState) ReactState {
	return ReactState{
		Messages: append(current.Messages, update.Messages...),
	}
}

// ReactConfig options for creating a ReAct agent graph.
type ReactConfig struct {
	SystemPrompt    string
	Checkpointer    graph.Checkpointer[ReactState]
	MaxSteps        int
	InterruptBefore []string
	InterruptAfter  []string
}

// ReactOption allows configuring the ReAct agent graph.
type ReactOption func(*ReactConfig)

// WithSystemPrompt sets an optional system instruction for the agent.
func WithSystemPrompt(prompt string) ReactOption {
	return func(c *ReactConfig) { c.SystemPrompt = prompt }
}

// WithAgentCheckpointer configures a checkpointer for the agent graph.
func WithAgentCheckpointer(cp graph.Checkpointer[ReactState]) ReactOption {
	return func(c *ReactConfig) { c.Checkpointer = cp }
}

// WithAgentMaxSteps configures the maximum number of steps for the agent graph.
func WithAgentMaxSteps(steps int) ReactOption {
	return func(c *ReactConfig) { c.MaxSteps = steps }
}

// WithAgentInterruptBefore configures human-in-the-loop interrupts before specific nodes.
func WithAgentInterruptBefore(nodes ...string) ReactOption {
	return func(c *ReactConfig) { c.InterruptBefore = append(c.InterruptBefore, nodes...) }
}

// WithAgentInterruptAfter configures human-in-the-loop interrupts after specific nodes.
func WithAgentInterruptAfter(nodes ...string) ReactOption {
	return func(c *ReactConfig) { c.InterruptAfter = append(c.InterruptAfter, nodes...) }
}

// CreateReactAgent compiles a production-grade ReAct agent as a StateGraph:
//
//	START -> [agent] --(has tool calls?)--> [tools] -> [agent]
//	            |
//	            +--(is final?)--> END
func CreateReactAgent(
	model core.ChatModel,
	tools []core.Tool,
	opts ...ReactOption,
) (*graph.CompiledGraph[ReactState], error) {
	cfg := ReactConfig{
		MaxSteps: 25,
	}
	for _, o := range opts {
		o(&cfg)
	}

	modelWithTools := model.BindTools(tools...)
	toolRegistry := core.NewToolRegistry()
	for _, t := range tools {
		toolRegistry.Register(t)
	}

	g := graph.NewStateGraph[ReactState](ReactStateReducer)

	// Node 1: Agent Node (calls model)
	g.AddNode("agent", func(ctx context.Context, state ReactState) (ReactState, error) {
		msgs := state.Messages
		if cfg.SystemPrompt != "" && (len(msgs) == 0 || msgs[0].Role != core.RoleSystem) {
			msgs = append([]core.Message{core.NewSystemMessage(cfg.SystemPrompt)}, msgs...)
		}

		resp, err := modelWithTools.Generate(ctx, msgs)
		if err != nil {
			return ReactState{}, fmt.Errorf("react agent node: %w", err)
		}

		return ReactState{
			Messages: []core.Message{*resp},
		}, nil
	})

	// Node 2: Tools Node (executes tool calls in parallel)
	g.AddNode("tools", func(ctx context.Context, state ReactState) (ReactState, error) {
		if len(state.Messages) == 0 {
			return ReactState{}, nil
		}
		lastMsg := state.Messages[len(state.Messages)-1]
		if len(lastMsg.ToolCalls) == 0 {
			return ReactState{}, nil
		}

		results := make([]core.Message, len(lastMsg.ToolCalls))
		var wg sync.WaitGroup

		for i, tc := range lastMsg.ToolCalls {
			wg.Add(1)
			go func(idx int, call core.ToolCall) {
				defer wg.Done()
				out, err := toolRegistry.Execute(ctx, call.Name, call.Arguments)
				if err != nil {
					out = fmt.Sprintf("Error executing tool %q: %v", call.Name, err)
				}
				results[idx] = core.NewToolMessage(call.ID, out)
			}(i, tc)
		}

		wg.Wait()

		return ReactState{
			Messages: results,
		}, nil
	})

	// Conditional Edge from "agent"
	g.SetEntryPoint("agent")
	g.AddConditionalEdges("agent", func(_ context.Context, state ReactState) (string, error) {
		if len(state.Messages) == 0 {
			return graph.END, nil
		}
		last := state.Messages[len(state.Messages)-1]
		if len(last.ToolCalls) > 0 {
			return "tools", nil
		}
		return graph.END, nil
	}, nil)

	// Edge from "tools" back to "agent" (the cycle!)
	g.AddEdge("tools", "agent")

	compileOpts := []graph.CompileOption{
		graph.WithMaxRecursionLimit[ReactState](cfg.MaxSteps),
	}
	if cfg.Checkpointer != nil {
		compileOpts = append(compileOpts, graph.WithCheckpointer[ReactState](cfg.Checkpointer))
	}
	if len(cfg.InterruptBefore) > 0 {
		compileOpts = append(compileOpts, graph.WithInterruptBefore[ReactState](cfg.InterruptBefore...))
	}
	if len(cfg.InterruptAfter) > 0 {
		compileOpts = append(compileOpts, graph.WithInterruptAfter[ReactState](cfg.InterruptAfter...))
	}

	return g.Compile(compileOpts...)
}
