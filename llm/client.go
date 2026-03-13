// Package llm provides an HTTP client to call the cheese-server OpenAI-compatible
// local inference endpoint. Supports streaming (SSE), GBNF grammar-constrained
// decoding, and automatic model discovery.
package llm

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
)

// Client calls cheese-server's OpenAI-compatible /v1/chat/completions endpoint.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new LLM client pointing at the given base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 0},
	}
}

// BaseURL returns the base URL this client targets.
func (c *Client) BaseURL() string { return c.baseURL }

// ─── Types ───────────────────────────────────────────────────────────────────

// Request is a chat completion request to cheese-server.
type Request struct {
	Model         string    `json:"model"`
	Messages      []Message `json:"messages"`
	Grammar       string    `json:"grammar,omitempty"`
	Stream        bool      `json:"stream"`
	Temperature   float32   `json:"temperature,omitempty"`
	TopP          float32   `json:"top_p,omitempty"`
	RepeatPenalty float32   `json:"repetition_penalty,omitempty"`
	Tools         []Tool    `json:"tools,omitempty"`
}

// Message is a single role/content chat turn.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// Tool describes a callable function in the OpenAI tool-calling format.
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function is the callable function descriptor inside Tool.
type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall is a model-requested function call.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Response is the non-streaming response from cheese-server.
type Response struct {
	Choices []struct {
		Message   Message    `json:"message"`
		ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	} `json:"choices"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// ─── Model discovery ──────────────────────────────────────────────────────────

type modelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// GetActiveModel queries cheese-server for loaded models and returns the first ID.
func (c *Client) GetActiveModel(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
	if err != nil {
		return ""
	}
	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var list modelListResponse
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Data) == 0 {
		return ""
	}
	return list.Data[0].ID
}

func (c *Client) resolveModel(ctx context.Context, model string) (string, error) {
	if model != "" && model != "default" {
		return model, nil
	}
	if active := c.GetActiveModel(ctx); active != "" {
		return active, nil
	}
	return "", fmt.Errorf("crabchain/llm: no model loaded — load a model in AI Space first")
}

// ─── Streaming completion ─────────────────────────────────────────────────────

// CompleteStream returns three channels: token strings, full accumulated text, and errors.
// Callers must drain all channels to avoid goroutine leaks.
func (c *Client) CompleteStream(ctx context.Context, req Request) (<-chan string, <-chan string, <-chan error) {
	tCh := make(chan string, 256)
	fCh := make(chan string, 1)
	eCh := make(chan error, 1)

	go func() {
		defer close(tCh)
		defer close(fCh)
		defer close(eCh)

		model, err := c.resolveModel(ctx, req.Model)
		if err != nil {
			eCh <- err
			return
		}
		req.Model = model
		req.Stream = true

		body, err := json.Marshal(req)
		if err != nil {
			eCh <- fmt.Errorf("crabchain/llm: marshal: %w", err)
			return
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
		if err != nil {
			eCh <- fmt.Errorf("crabchain/llm: build request: %w", err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			eCh <- fmt.Errorf("crabchain/llm: http: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			eCh <- fmt.Errorf("crabchain/llm: server %d: %s", resp.StatusCode, string(raw))
			return
		}

		var full strings.Builder
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}
			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil || len(chunk.Choices) == 0 {
				continue
			}
			token := chunk.Choices[0].Delta.Content
			if token != "" {
				full.WriteString(token)
				select {
				case tCh <- token:
				case <-ctx.Done():
					eCh <- ctx.Err()
					return
				}
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			eCh <- fmt.Errorf("crabchain/llm: scan: %w", err)
			return
		}
		fCh <- full.String()
	}()

	return tCh, fCh, eCh
}

// Complete sends a non-streaming chat completion.
func (c *Client) Complete(ctx context.Context, req Request) (string, error) {
	model, err := c.resolveModel(ctx, req.Model)
	if err != nil {
		return "", err
	}
	req.Model = model
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("crabchain/llm: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("crabchain/llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("crabchain/llm: http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("crabchain/llm: read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("crabchain/llm: server %d: %s", resp.StatusCode, string(raw))
	}

	var llmResp Response
	if err := json.Unmarshal(raw, &llmResp); err != nil {
		return "", fmt.Errorf("crabchain/llm: unmarshal: %w", err)
	}
	if len(llmResp.Choices) == 0 {
		return "", fmt.Errorf("crabchain/llm: empty choices")
	}
	return llmResp.Choices[0].Message.Content, nil
}
