package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ─── read_file ─────────────────────────────────────────────────────────────

type ReadFileTool struct{}

func NewReadFileTool() *ReadFileTool { return &ReadFileTool{} }

func (t *ReadFileTool) Name()        string { return "read_file" }
func (t *ReadFileTool) Dangerous()   bool   { return false }
func (t *ReadFileTool) Description() string {
	return "Reads the text content of a file at the given path."
}
func (t *ReadFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Absolute or relative path to the file"},
		},
		"required": []string{"path"},
	}
}
func (t *ReadFileTool) Execute(_ context.Context, args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("read_file: 'path' arg required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	content := string(data)
	const maxBytes = 8000
	if len(content) > maxBytes {
		content = content[:maxBytes] + "\n... [truncated]"
	}
	return content, nil
}

// ─── write_file ────────────────────────────────────────────────────────────

type WriteFileTool struct{}

func NewWriteFileTool() *WriteFileTool { return &WriteFileTool{} }

func (t *WriteFileTool) Name()        string { return "write_file" }
func (t *WriteFileTool) Dangerous()   bool   { return false }
func (t *WriteFileTool) Description() string {
	return "Writes text content to a file, creating it (and parent dirs) if needed."
}
func (t *WriteFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		"required": []string{"path", "content"},
	}
}
func (t *WriteFileTool) Execute(_ context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return "", fmt.Errorf("write_file: 'path' required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

// ─── list_dir ──────────────────────────────────────────────────────────────

type ListDirTool struct{}

func NewListDirTool() *ListDirTool { return &ListDirTool{} }

func (t *ListDirTool) Name()        string { return "list_dir" }
func (t *ListDirTool) Dangerous()   bool   { return false }
func (t *ListDirTool) Description() string {
	return "Lists files and directories in the given folder (non-recursive, one level deep)."
}
func (t *ListDirTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
		"required":   []string{"path"},
	}
}
func (t *ListDirTool) Execute(_ context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, e := range entries {
		kind := "file"
		if e.IsDir() {
			kind = "dir"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", kind, e.Name()))
	}
	return strings.Join(lines, "\n"), nil
}
