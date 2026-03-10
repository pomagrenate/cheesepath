// Package llm provides a thin HTTP client to call cheese-server
// (an OpenAI-compatible local LLM server) from the crabpath agent.
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

// Client calls cheese-server's OpenAI-compatible endpoint.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new LLM client pointed at the given base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			// No hard timeout here — we set per-request deadlines via context
			Timeout: 0,
		},
	}
}

// ─── Types ───────────────────────────────────────────────────────────────────

type Request struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Grammar  string    `json:"grammar,omitempty"`
	Stream   bool      `json:"stream"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Response struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// SSE delta chunk from cheese-server streaming response
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
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

// GetActiveModel queries cheese-server for loaded models and returns the first one.
func (c *Client) GetActiveModel(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
	if err != nil {
		return ""
	}
	// Use a short timeout just for model discovery
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

// resolveModel ensures we never send "default" to cheese-server.
func (c *Client) resolveModel(ctx context.Context, model string) (string, error) {
	if model != "" && model != "default" {
		return model, nil
	}
	if active := c.GetActiveModel(ctx); active != "" {
		return active, nil
	}
	return "", fmt.Errorf("crabpath/llm: no model loaded — please select a model in AI Models space first")
}

// ─── Streaming completion ─────────────────────────────────────────────────────

// CompleteStream calls cheese-server with stream:true and sends each token
// to the returned channel as it arrives. The channel is closed when generation
// is done. The full accumulated text is also returned via the done channel.
// tokenCh receives individual string tokens; fullCh receives the complete text.
func (c *Client) CompleteStream(ctx context.Context, req Request) (tokenCh <-chan string, fullCh <-chan string, errCh <-chan error) {
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
			eCh <- fmt.Errorf("crabpath/llm: marshal: %w", err)
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
		if err != nil {
			eCh <- fmt.Errorf("crabpath/llm: request: %w", err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			eCh <- fmt.Errorf("crabpath/llm: http: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			eCh <- fmt.Errorf("crabpath/llm: cheese-server %d: %s", resp.StatusCode, string(raw))
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
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) == 0 {
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
		fCh <- full.String()
	}()

	return tCh, fCh, eCh
}

// ─── Non-streaming (fallback) ─────────────────────────────────────────────────

// Complete sends a non-streaming chat completion. If req.Model is "default",
// it auto-detects the active model. Use CompleteStream for real-time output.
func (c *Client) Complete(ctx context.Context, req Request) (string, error) {
	model, err := c.resolveModel(ctx, req.Model)
	if err != nil {
		return "", err
	}
	req.Model = model
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("crabpath/llm: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("crabpath/llm: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("crabpath/llm: http: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("crabpath/llm: read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("crabpath/llm: cheese-server %d: %s", resp.StatusCode, string(raw))
	}

	var llmResp Response
	if err := json.Unmarshal(raw, &llmResp); err != nil {
		return "", fmt.Errorf("crabpath/llm: unmarshal: %w", err)
	}
	if len(llmResp.Choices) == 0 {
		return "", fmt.Errorf("crabpath/llm: empty choices")
	}
	return llmResp.Choices[0].Message.Content, nil
}
