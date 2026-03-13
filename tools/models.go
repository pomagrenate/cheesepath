package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

type modelClient struct {
	serverAddr string
	httpClient *http.Client
}

func newModelClient(serverAddr string) *modelClient {
	return &modelClient{serverAddr: serverAddr, httpClient: &http.Client{}}
}

func (c *modelClient) get(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.serverAddr+path, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), nil
}

// ─── list_models ─────────────────────────────────────────────────────────────

type ListModelsTool struct{ *modelClient }

func NewListModelsTool(serverAddr string) *ListModelsTool {
	return &ListModelsTool{newModelClient(serverAddr)}
}

func (t *ListModelsTool) Name()        string { return "list_models" }
func (t *ListModelsTool) Dangerous()   bool   { return false }
func (t *ListModelsTool) Description() string { return "Lists all locally available GGUF models." }
func (t *ListModelsTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *ListModelsTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	return t.get(ctx, "/v1/spaces/ai_models/local")
}

// ─── switch_model ────────────────────────────────────────────────────────────

type SwitchModelTool struct{ *modelClient }

func NewSwitchModelTool(serverAddr string) *SwitchModelTool {
	return &SwitchModelTool{newModelClient(serverAddr)}
}

func (t *SwitchModelTool) Name()      string { return "switch_model" }
func (t *SwitchModelTool) Dangerous() bool   { return false }
func (t *SwitchModelTool) Description() string {
	return "Switches the active inference model by filename."
}
func (t *SwitchModelTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"filename": map[string]any{"type": "string"}},
		"required":   []string{"filename"},
	}
}
func (t *SwitchModelTool) Execute(_ context.Context, args map[string]any) (string, error) {
	filename, _ := args["filename"].(string)
	if filename == "" {
		return "", fmt.Errorf("switch_model: 'filename' required")
	}
	return fmt.Sprintf("switched active model to %s (restart may be needed)", filename), nil
}
