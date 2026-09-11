package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/AutoCookies/cheesepath/core"
	"github.com/AutoCookies/cheesepath/graph"
	"github.com/AutoCookies/cheesepath/providers"
)

func TestReactAgent_DirectAnswer(t *testing.T) {
	ctx := context.Background()
	mockModel := providers.NewMockChatModel(
		core.NewAIMessage("Hello! How can I help you today?"),
	)

	agent, err := CreateReactAgent(mockModel, nil)
	if err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	initialState := ReactState{
		Messages: []core.Message{
			core.NewHumanMessage("Hi"),
		},
	}

	finalState, err := agent.Invoke(ctx, initialState)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}

	if len(finalState.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(finalState.Messages))
	}
	if finalState.Messages[1].Content != "Hello! How can I help you today?" {
		t.Fatalf("unexpected answer: %s", finalState.Messages[1].Content)
	}
}

func TestReactAgent_ToolCallCycle(t *testing.T) {
	ctx := context.Background()

	// Step 1: Model calls tool 'calculator'
	step1AI := core.NewAIMessageWithToolCalls("Let me calculate that for you.", []core.ToolCall{
		{
			ID:   "calc-call-1",
			Name: "calculator",
			Arguments: map[string]any{
				"expression": "5 + 7",
			},
		},
	})
	// Step 2: Model receives tool answer and gives final response
	step2AI := core.NewAIMessage("The result of 5 + 7 is 12.")

	mockModel := providers.NewMockChatModel(step1AI, step2AI)

	calcTool := core.NewFuncTool(
		"calculator",
		"Evaluates simple arithmetic expressions",
		map[string]any{"type": "object"},
		func(ctx context.Context, args map[string]any) (string, error) {
			expr, _ := args["expression"].(string)
			if expr == "5 + 7" {
				return "12", nil
			}
			return "", fmt.Errorf("unknown expr: %s", expr)
		},
	)

	agent, err := CreateReactAgent(mockModel, []core.Tool{calcTool})
	if err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	initialState := ReactState{
		Messages: []core.Message{
			core.NewHumanMessage("What is 5 + 7?"),
		},
	}

	finalState, err := agent.Invoke(ctx, initialState)
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}

	// Sequence of messages:
	// 0: Human: What is 5 + 7?
	// 1: AI: Let me calculate (tool_calls: calc-call-1)
	// 2: Tool: 12 (tool_call_id: calc-call-1)
	// 3: AI: The result of 5 + 7 is 12.
	if len(finalState.Messages) != 4 {
		t.Fatalf("expected 4 messages in conversation history, got %d", len(finalState.Messages))
	}

	if finalState.Messages[1].ToolCalls[0].Name != "calculator" {
		t.Fatalf("expected tool call to calculator, got %s", finalState.Messages[1].ToolCalls[0].Name)
	}
	if finalState.Messages[2].Role != core.RoleTool || finalState.Messages[2].Content != "12" {
		t.Fatalf("expected tool message with content 12, got: %+v", finalState.Messages[2])
	}
	if finalState.Messages[3].Content != "The result of 5 + 7 is 12." {
		t.Fatalf("expected final answer, got: %s", finalState.Messages[3].Content)
	}
}

func TestReactAgent_HumanInTheLoopInterrupt(t *testing.T) {
	ctx := context.Background()

	step1AI := core.NewAIMessageWithToolCalls("Calling bash command", []core.ToolCall{
		{
			ID:   "call-1",
			Name: "dangerous_shell",
			Arguments: map[string]any{
				"cmd": "rm -rf /tmp/data",
			},
		},
	})
	step2AI := core.NewAIMessage("Command executed successfully.")

	mockModel := providers.NewMockChatModel(step1AI, step2AI)
	shellTool := core.NewFuncTool("dangerous_shell", "Executes shell commands", nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "files deleted", nil
		},
	)

	saver := graph.NewMemorySaver[ReactState]()
	agent, err := CreateReactAgent(
		mockModel,
		[]core.Tool{shellTool},
		WithAgentCheckpointer(saver),
		WithAgentInterruptBefore("tools"), // Interrupt before dangerous tools execute!
	)
	if err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	threadID := "agent-approval-1"
	initialState := ReactState{
		Messages: []core.Message{
			core.NewHumanMessage("Clean up temp data"),
		},
	}

	// Run 1: Should halt before "tools" node
	_, err = agent.Invoke(ctx, initialState, graph.WithThreadID(threadID))
	if err == nil {
		t.Fatal("expected interrupt error, got nil")
	}
	if !graph.IsInterrupt(err) {
		t.Fatalf("expected IsInterrupt=true, got: %v", err)
	}

	// Inspect interrupted state
	cp, err := saver.Get(ctx, threadID)
	if err != nil {
		t.Fatalf("get checkpoint: %v", err)
	}
	if cp.Node != "tools" {
		t.Fatalf("expected interrupted at 'tools' node, got %s", cp.Node)
	}

	// Run 2: Human approves! Resume execution
	resumedState, err := agent.Resume(ctx, threadID, ReactState{})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	if len(resumedState.Messages) != 4 {
		t.Fatalf("expected 4 messages after resume, got %d", len(resumedState.Messages))
	}
	if resumedState.Messages[3].Content != "Command executed successfully." {
		t.Fatalf("unexpected final response: %s", resumedState.Messages[3].Content)
	}
}
