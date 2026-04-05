// Package llm provides an HTTP client to call the cheese-server OpenAI-compatible
// local inference endpoint. Supports streaming (SSE), GBNF grammar-constrained
// decoding, automatic model discovery, and configurable retry with exponential backoff.
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

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithMaxRetries sets how many additional attempts to make after the first
// failure (default 3). Set to 0 to disable retries.
func WithMaxRetries(n int) ClientOption { return func(c *Client) { c.maxRetries = n } }

// WithRetryBaseDelay sets the initial backoff delay between retry attempts
// (default 1s). Each subsequent attempt doubles the delay.
func WithRetryBaseDelay(d time.Duration) ClientOption { return func(c *Client) { c.retryDelay = d } }

// WithStreamTimeout sets a per-request timeout on the HTTP client used for
// streaming requests (default 0 = no timeout). The timeout applies to the
// whole stream duration, not just the initial connection.
func WithStreamTimeout(d time.Duration) ClientOption {
	return func(c *Client) { c.streamTimeout = d }
}

// Client calls cheese-server's OpenAI-compatible /v1/chat/completions endpoint.
type Client struct {
	baseURL       string
	httpClient    *http.Client
	maxRetries    int
	retryDelay    time.Duration
	streamTimeout time.Duration
}

// NewClient creates a new LLM client pointing at the given base URL.
// Defaults: maxRetries=3, retryDelay=1s, no stream timeout.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 0},
		maxRetries: 3,
		retryDelay: time.Second,
	}
	for _, o := range opts {
		o(c)
	}
	return c
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

// ─── Retry helper ─────────────────────────────────────────────────────────────

// doWithRetry executes an HTTP POST to endpoint with body, retrying on transient
// errors (connection failures, 429, 5xx) up to c.maxRetries additional attempts
// with exponential backoff starting at c.retryDelay.
func (c *Client) doWithRetry(ctx context.Context, hc *http.Client, endpoint string, body []byte, accept string) (*http.Response, error) {
	var lastErr error
	delay := c.retryDelay

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			delay *= 2
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("crabchain/llm: build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if accept != "" {
			httpReq.Header.Set("Accept", accept)
		}

		resp, err := hc.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("crabchain/llm: http (attempt %d): %w", attempt+1, err)
			continue
		}
		// Retry on server overload or transient server errors.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("crabchain/llm: server %d (attempt %d): %s", resp.StatusCode, attempt+1, string(raw))
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("crabchain/llm: all %d attempt(s) failed: %w", c.maxRetries+1, lastErr)
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

		// Use a separate http.Client with the stream timeout if configured.
		hc := c.httpClient
		if c.streamTimeout > 0 {
			hc = &http.Client{Timeout: c.streamTimeout}
		}

		resp, err := c.doWithRetry(ctx, hc, "/v1/chat/completions", body, "text/event-stream")
		if err != nil {
			eCh <- err
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

// Complete sends a non-streaming chat completion with retry.
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

	resp, err := c.doWithRetry(ctx, c.httpClient, "/v1/chat/completions", body, "")
	if err != nil {
		return "", err
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
