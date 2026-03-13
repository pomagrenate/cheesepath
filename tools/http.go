package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPRequestTool allows the agent to make HTTP GET or POST requests.
type HTTPRequestTool struct {
	client *http.Client
}

func NewHTTPRequestTool() *HTTPRequestTool {
	return &HTTPRequestTool{client: &http.Client{Timeout: 15 * time.Second}}
}

func (t *HTTPRequestTool) Name()      string { return "http_request" }
func (t *HTTPRequestTool) Dangerous() bool   { return false }
func (t *HTTPRequestTool) Description() string {
	return "Makes an HTTP GET or POST request to a URL. Returns the response body (truncated at 8 KB). Use for web research, API calls, or fetching remote data."
}
func (t *HTTPRequestTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"method": map[string]any{
				"type":        "string",
				"enum":        []string{"GET", "POST"},
				"description": "HTTP method (GET or POST)",
			},
			"url": map[string]any{
				"type":        "string",
				"description": "Full URL to request",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Optional request body for POST requests",
			},
			"content_type": map[string]any{
				"type":        "string",
				"description": "Content-Type header (default: application/json for POST)",
			},
		},
		"required": []string{"url"},
	}
}

func (t *HTTPRequestTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return "", fmt.Errorf("http_request: 'url' required")
	}
	method := "GET"
	if m, ok := args["method"].(string); ok && m != "" {
		method = strings.ToUpper(m)
	}

	var bodyReader io.Reader
	if rawBody, ok := args["body"].(string); ok && rawBody != "" {
		bodyReader = bytes.NewBufferString(rawBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return "", fmt.Errorf("http_request: build request: %w", err)
	}

	if method == "POST" {
		ct := "application/json"
		if v, ok := args["content_type"].(string); ok && v != "" {
			ct = v
		}
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("User-Agent", "CrabAgent/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http_request: %w", err)
	}
	defer resp.Body.Close()

	const maxBytes = 8192
	limited := io.LimitReader(resp.Body, maxBytes+1)
	rawBytes, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("http_request: read body: %w", err)
	}

	body := string(rawBytes)
	truncated := false
	if len(rawBytes) > maxBytes {
		body = body[:maxBytes]
		truncated = true
	}

	// Pretty-print if JSON
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		var pretty any
		if json.Unmarshal([]byte(body), &pretty) == nil {
			if b, err := json.MarshalIndent(pretty, "", "  "); err == nil {
				body = string(b)
			}
		}
	}

	result := fmt.Sprintf("HTTP %d %s\n\n%s", resp.StatusCode, resp.Status, body)
	if truncated {
		result += "\n... [response truncated at 8 KB]"
	}
	return result, nil
}
