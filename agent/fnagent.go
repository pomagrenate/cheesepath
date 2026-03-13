package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FunctionCallingStrategy uses native OpenAI tool_calls JSON format.
// Compatible with models that support structured function calling (Mistral, Qwen2.5).
type FunctionCallingStrategy struct{}

func NewFunctionCallingStrategy() *FunctionCallingStrategy { return &FunctionCallingStrategy{} }

func (s *FunctionCallingStrategy) Name() string { return "function_calling" }

// No grammar needed: the model uses its native tool_calls format.
func (s *FunctionCallingStrategy) Grammar() string { return "" }

func (s *FunctionCallingStrategy) BuildSystemPrompt(toolDescs string) string {
	var sb strings.Builder
	sb.WriteString(`You are CrabAgent, a powerful local AI agent running inside Cheesecrab.
Execute the user's goal using the available tools. Call tools as needed.
When you have the final answer, respond with:

FINAL_ANSWER: <your complete answer here>

Available tools:
`)
	sb.WriteString(toolDescs)
	return sb.String()
}

// ParseResponse handles both native tool_calls JSON and FINAL_ANSWER text.
func (s *FunctionCallingStrategy) ParseResponse(raw string) (CrabThought, []CrabToolCall, error) {
	trimmed := strings.TrimSpace(raw)

	// Final answer marker
	if strings.HasPrefix(trimmed, "FINAL_ANSWER:") {
		answer := strings.TrimSpace(strings.TrimPrefix(trimmed, "FINAL_ANSWER:"))
		return CrabThought{
			Reasoning:   answer,
			IsFinal:     true,
			FinalAnswer: answer,
		}, nil, nil
	}

	// Try to parse as OpenAI tool_calls response format
	var fnResp struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal([]byte(raw), &fnResp); err == nil &&
		len(fnResp.Choices) > 0 &&
		len(fnResp.Choices[0].Message.ToolCalls) > 0 {

		var calls []CrabToolCall
		for _, tc := range fnResp.Choices[0].Message.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{"_raw": tc.Function.Arguments}
			}
			calls = append(calls, CrabToolCall{
				ToolName: tc.Function.Name,
				Args:     args,
			})
		}
		content := fnResp.Choices[0].Message.Content
		return CrabThought{
			Reasoning: content,
			Plan:      fmt.Sprintf("calling %d tool(s)", len(calls)),
		}, calls, nil
	}

	// Fall back to ReAct JSON parsing
	return (&ReActStrategy{}).ParseResponse(raw)
}
