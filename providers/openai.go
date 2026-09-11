// Package providers provides ChatModel implementations for inference endpoints and mocking.
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AutoCookies/cheesepath/core"
)

// OpenAIClient implements core.ChatModel for any OpenAI-compatible inference endpoint.
type OpenAIClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	tools      []core.Tool
}

// Client is an alias for OpenAIClient.
type Client = OpenAIClient

// OpenAIOption configures an OpenAI Client.
type OpenAIOption func(*OpenAIClient)

// Option is an alias for OpenAIOption.
type Option = OpenAIOption

func WithBaseURL(url string) OpenAIOption {
	return func(c *OpenAIClient) { c.baseURL = strings.TrimRight(url, "/") }
}

func WithAPIKey(key string) OpenAIOption {
	return func(c *OpenAIClient) { c.apiKey = key }
}

func WithModel(model string) OpenAIOption {
	return func(c *OpenAIClient) { c.model = model }
}

func WithHTTPClient(hc *http.Client) OpenAIOption {
	return func(c *OpenAIClient) { c.httpClient = hc }
}

// NewOpenAIClient creates an OpenAI-compatible ChatModel.
func NewOpenAIClient(opts ...OpenAIOption) *OpenAIClient {
	c := &OpenAIClient{
		baseURL:    "http://127.0.0.1:8081",
		model:      "default",
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// NewClient creates an OpenAI-compatible ChatModel (alias).
func NewClient(opts ...OpenAIOption) *OpenAIClient {
	return NewOpenAIClient(opts...)
}

// BindTools returns a clone of the client with the specified tools attached.
func (c *OpenAIClient) BindTools(tools ...core.Tool) core.ChatModel {
	clone := &OpenAIClient{
		baseURL:    c.baseURL,
		apiKey:     c.apiKey,
		model:      c.model,
		httpClient: c.httpClient,
		tools:      append(c.tools, tools...),
	}
	return clone
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature float32         `json:"temperature,omitempty"`
	Tools       []openAITool    `json:"tools,omitempty"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Role      string           `json:"role"`
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func (c *OpenAIClient) formatMessages(messages []core.Message) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages))
	for _, m := range messages {
		oMsg := openAIMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
		if m.ToolCallID != "" {
			oMsg.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				argsStr := tc.RawArguments
				if argsStr == "" && tc.Arguments != nil {
					data, _ := json.Marshal(tc.Arguments)
					argsStr = string(data)
				}
				oTc := openAIToolCall{
					ID:   tc.ID,
					Type: "function",
				}
				oTc.Function.Name = tc.Name
				oTc.Function.Arguments = argsStr
				oMsg.ToolCalls = append(oMsg.ToolCalls, oTc)
			}
		}
		out = append(out, oMsg)
	}
	return out
}

func (c *OpenAIClient) formatTools(tools []core.Tool) []openAITool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openAITool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return out
}

// Generate produces a non-streaming completion.
func (c *OpenAIClient) Generate(ctx context.Context, messages []core.Message, opts ...core.ModelOption) (*core.Message, error) {
	cfg := core.ModelConfig{
		Model: c.model,
		Tools: c.tools,
	}
	for _, o := range opts {
		o(&cfg)
	}

	reqBody := openAIRequest{
		Model:       cfg.Model,
		Messages:    c.formatMessages(messages),
		Stream:      false,
		Temperature: cfg.Temperature,
		Tools:       c.formatTools(cfg.Tools),
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: do request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: server returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var parsed openAIResponse
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return nil, fmt.Errorf("openai: unmarshal response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices returned")
	}

	choice := parsed.Choices[0].Message
	var toolCalls []core.ToolCall
	for _, tc := range choice.ToolCalls {
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		toolCalls = append(toolCalls, core.ToolCall{
			ID:           tc.ID,
			Name:         tc.Function.Name,
			Arguments:    args,
			RawArguments: tc.Function.Arguments,
		})
	}

	result := core.NewAIMessage(choice.Content)
	if len(toolCalls) > 0 {
		result.ToolCalls = toolCalls
	}
	return &result, nil
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Role      string           `json:"role"`
			Content   string           `json:"content"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// Stream produces a streaming response of tokens and tool calls.
func (c *OpenAIClient) Stream(ctx context.Context, messages []core.Message, opts ...core.ModelOption) (<-chan core.StreamChunk, error) {
	cfg := core.ModelConfig{
		Model: c.model,
		Tools: c.tools,
	}
	for _, o := range opts {
		o(&cfg)
	}

	reqBody := openAIRequest{
		Model:       cfg.Model,
		Messages:    c.formatMessages(messages),
		Stream:      true,
		Temperature: cfg.Temperature,
		Tools:       c.formatTools(cfg.Tools),
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: do stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai: stream returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	outCh := make(chan core.StreamChunk, 64)

	go func() {
		defer resp.Body.Close()
		defer close(outCh)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				outCh <- core.StreamChunk{Done: true}
				return
			}

			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil || len(chunk.Choices) == 0 {
				continue
			}

			delta := chunk.Choices[0].Delta
			var tcDeltas []core.ToolCallDelta
			for _, tc := range delta.ToolCalls {
				tcDeltas = append(tcDeltas, core.ToolCallDelta{
					Index:          tc.Index,
					ID:             tc.ID,
					Name:           tc.Function.Name,
					ArgumentsDelta: tc.Function.Arguments,
				})
			}

			outCh <- core.StreamChunk{
				ContentDelta:   delta.Content,
				ToolCallDeltas: tcDeltas,
				Done:           chunk.Choices[0].FinishReason != nil,
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			outCh <- core.StreamChunk{Error: fmt.Errorf("openai: stream scanner: %w", err)}
		}
	}()

	return outCh, nil
}
