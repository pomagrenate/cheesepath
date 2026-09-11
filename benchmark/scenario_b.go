package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"github.com/AutoCookies/cheesepath/agent"
	"github.com/AutoCookies/cheesepath/core"
	"github.com/AutoCookies/cheesepath/providers"
)

// RunScenarioB executes Scenario B (Cyclic ReAct Loop Execution - 20 turns).
func RunScenarioB(iterations int, maxTurns int) (Result, error) {
	if maxTurns <= 0 {
		maxTurns = 20
	}

	ctx := context.Background()

	// Tool with JSON schema validation and execution
	mockTool := core.NewFuncTool(
		"data_fetcher",
		"Fetches structured data records by ID",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"item_id": map[string]any{"type": "number"},
				"query":   map[string]any{"type": "string"},
			},
			"required": []string{"item_id", "query"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			id, _ := args["item_id"].(float64)
			q, _ := args["query"].(string)
			resp := map[string]any{
				"status": "ok",
				"record": id,
				"query":  q,
				"data":   "sample_payload_data_block",
			}
			bytes, _ := json.Marshal(resp)
			return string(bytes), nil
		},
	)

	// Build a dynamic mock responder that iterates maxTurns times
	mockModel := providers.NewMockChatModel()
	mockModel.SetResponder(func(messages []core.Message) (*core.Message, error) {
		// Count tool messages in conversation
		toolCount := 0
		for _, m := range messages {
			if m.Role == core.RoleTool {
				toolCount++
			}
		}

		if toolCount < maxTurns {
			aiMsg := core.NewAIMessageWithToolCalls(
				fmt.Sprintf("Step %d: querying data", toolCount+1),
				[]core.ToolCall{
					{
						ID:   fmt.Sprintf("call-%d", toolCount+1),
						Name: "data_fetcher",
						Arguments: map[string]any{
							"item_id": float64(toolCount + 1),
							"query":   "fetch_record",
						},
					},
				},
			)
			return &aiMsg, nil
		}

		final := core.NewAIMessage("All 20 iterations completed successfully.")
		return &final, nil
	})

	app, err := agent.CreateReactAgent(mockModel, []core.Tool{mockTool}, agent.WithAgentMaxSteps(maxTurns*2+5))
	if err != nil {
		return Result{}, fmt.Errorf("create react agent: %w", err)
	}

	// Warmup phase
	for i := 0; i < 10; i++ {
		_, _ = app.Invoke(ctx, agent.ReactState{
			Messages: []core.Message{core.NewHumanMessage("Start loop")},
		})
	}

	latencies := make([]time.Duration, iterations)
	runtime.GC()

	var mStart, mEnd runtime.MemStats
	runtime.ReadMemStats(&mStart)

	for i := 0; i < iterations; i++ {
		start := time.Now()
		res, err := app.Invoke(ctx, agent.ReactState{
			Messages: []core.Message{core.NewHumanMessage("Start loop")},
		})
		dur := time.Since(start)
		if err != nil {
			return Result{}, fmt.Errorf("react loop failed at %d: %w", i, err)
		}
		// Expected messages: Human(1) + 20*(AI tool_call + Tool response) + final AI = 42
		if len(res.Messages) < maxTurns*2 {
			return Result{}, fmt.Errorf("expected at least %d messages, got %d", maxTurns*2, len(res.Messages))
		}
		latencies[i] = dur
	}

	runtime.ReadMemStats(&mEnd)
	return CalculateStats("Scenario B: Cyclic ReAct Loop (20 Tool Calls)", latencies, mStart, mEnd), nil
}
