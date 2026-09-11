package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AutoCookies/cheesepath/core"
)

func TestOpenAIClient_Generate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}

		if len(req.Messages) != 1 || req.Messages[0].Content != "Hello" {
			t.Errorf("unexpected messages: %+v", req.Messages)
		}

		resp := openAIResponse{
			Choices: []struct {
				Message struct {
					Role      string           `json:"role"`
					Content   string           `json:"content"`
					ToolCalls []openAIToolCall `json:"tool_calls"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Role      string           `json:"role"`
						Content   string           `json:"content"`
						ToolCalls []openAIToolCall `json:"tool_calls"`
					}{
						Role:    "assistant",
						Content: "Hi there!",
					},
					FinishReason: "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	msg, err := client.Generate(context.Background(), []core.Message{
		core.NewHumanMessage("Hello"),
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	if msg.Content != "Hi there!" {
		t.Fatalf("expected 'Hi there!', got: %s", msg.Content)
	}
}

func TestOpenAIClient_ToolCalling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openAIRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "get_weather" {
			t.Errorf("expected get_weather tool in request, got: %+v", req.Tools)
		}

		resp := openAIResponse{
			Choices: []struct {
				Message struct {
					Role      string           `json:"role"`
					Content   string           `json:"content"`
					ToolCalls []openAIToolCall `json:"tool_calls"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Role      string           `json:"role"`
						Content   string           `json:"content"`
						ToolCalls []openAIToolCall `json:"tool_calls"`
					}{
						Role: "assistant",
						ToolCalls: []openAIToolCall{
							{
								ID:   "call_abc123",
								Type: "function",
								Function: struct {
									Name      string `json:"name"`
									Arguments string `json:"arguments"`
								}{
									Name:      "get_weather",
									Arguments: `{"location": "Tokyo"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	weatherTool := core.NewFuncTool("get_weather", "Get weather for a city", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{"type": "string"},
		},
		"required": []string{"location"},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		return "Sunny 22C", nil
	})

	client := NewClient(WithBaseURL(server.URL))
	modelWithTools := client.BindTools(weatherTool)

	msg, err := modelWithTools.Generate(context.Background(), []core.Message{
		core.NewHumanMessage("Weather in Tokyo?"),
	})
	if err != nil {
		t.Fatalf("generate with tools failed: %v", err)
	}

	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.Name != "get_weather" {
		t.Fatalf("expected tool 'get_weather', got: %s", tc.Name)
	}
	if tc.Arguments["location"] != "Tokyo" {
		t.Fatalf("expected location Tokyo, got: %v", tc.Arguments["location"])
	}
}

func TestOpenAIClient_Stream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		chunks := []string{
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{"content":" world"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`[DONE]`,
		}

		for _, chunk := range chunks {
			if chunk == "[DONE]" {
				fmt.Fprintf(w, "data: [DONE]\n\n")
			} else {
				fmt.Fprintf(w, "data: %s\n\n", chunk)
			}
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	stream, err := client.Stream(context.Background(), []core.Message{
		core.NewHumanMessage("Hi"),
	})
	if err != nil {
		t.Fatalf("stream call failed: %v", err)
	}

	var assembled string
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("chunk error: %v", chunk.Error)
		}
		assembled += chunk.ContentDelta
	}

	if assembled != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", assembled)
	}
}
