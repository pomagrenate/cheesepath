package tools

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ─── get_file_info ────────────────────────────────────────────────────────────

type GetFileInfoTool struct{}

func NewGetFileInfoTool() *GetFileInfoTool { return &GetFileInfoTool{} }

func (t *GetFileInfoTool) Name()      string { return "get_file_info" }
func (t *GetFileInfoTool) Dangerous() bool   { return false }
func (t *GetFileInfoTool) Description() string {
	return "Returns metadata for a file or directory: size, permissions, modified time, type."
}
func (t *GetFileInfoTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
		"required":   []string{"path"},
	}
}
func (t *GetFileInfoTool) Execute(_ context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("get_file_info: 'path' required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	return fmt.Sprintf("path: %s\ntype: %s\nsize: %d bytes\npermissions: %s\nmodified: %s",
		path, kind, info.Size(), info.Mode().String(), info.ModTime().Format(time.RFC3339)), nil
}

// ─── list_dir_recursive ───────────────────────────────────────────────────────

type ListDirRecursiveTool struct{}

func NewListDirRecursiveTool() *ListDirRecursiveTool { return &ListDirRecursiveTool{} }

func (t *ListDirRecursiveTool) Name()      string { return "list_dir_recursive" }
func (t *ListDirRecursiveTool) Dangerous() bool   { return false }
func (t *ListDirRecursiveTool) Description() string {
	return "Lists all files and directories under a path recursively. Skips hidden dirs (.git, node_modules). Use max_depth to limit."
}
func (t *ListDirRecursiveTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":      map[string]any{"type": "string"},
			"max_depth": map[string]any{"type": "integer", "description": "Max folder depth (default 4)"},
		},
		"required": []string{"path"},
	}
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".next": true, "vendor": true,
	"__pycache__": true, ".venv": true, "dist": true, "build": true,
}

func (t *ListDirRecursiveTool) Execute(_ context.Context, args map[string]any) (string, error) {
	root, _ := args["path"].(string)
	if root == "" {
		root = "."
	}
	maxDepth := 4
	if d, ok := args["max_depth"].(float64); ok && int(d) > 0 {
		maxDepth = int(d)
	}

	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator))
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if depth >= maxDepth && d.IsDir() {
			return filepath.SkipDir
		}
		prefix := strings.Repeat("  ", depth)
		if d.IsDir() {
			lines = append(lines, prefix+"[dir] "+rel+"/")
		} else {
			lines = append(lines, prefix+"[file] "+rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "(empty directory)", nil
	}
	if len(lines) > 300 {
		lines = append(lines[:300], fmt.Sprintf("... (%d more items)", len(lines)-300))
	}
	return strings.Join(lines, "\n"), nil
}

// ─── search_files ─────────────────────────────────────────────────────────────

type SearchFilesTool struct{}

func NewSearchFilesTool() *SearchFilesTool { return &SearchFilesTool{} }

func (t *SearchFilesTool) Name()      string { return "search_files" }
func (t *SearchFilesTool) Dangerous() bool   { return false }
func (t *SearchFilesTool) Description() string {
	return "Searches for a text pattern inside files in a directory (like grep -r). Returns file path + matching line."
}
func (t *SearchFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"pattern": map[string]any{"type": "string"},
			"ext":     map[string]any{"type": "string", "description": "Optional file extension filter, e.g. '.go'"},
		},
		"required": []string{"path", "pattern"},
	}
}
func (t *SearchFilesTool) Execute(_ context.Context, args map[string]any) (string, error) {
	root, _ := args["path"].(string)
	pattern, _ := args["pattern"].(string)
	extFilter, _ := args["ext"].(string)
	if root == "" || pattern == "" {
		return "", fmt.Errorf("search_files: 'path' and 'pattern' required")
	}
	patternLower := strings.ToLower(pattern)
	var results []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if extFilter != "" && !strings.HasSuffix(path, extFilter) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(strings.ToLower(line), patternLower) {
				rel, _ := filepath.Rel(root, path)
				results = append(results, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
				if len(results) >= 50 {
					return fmt.Errorf("limit reached")
				}
			}
		}
		return nil
	})
	if len(results) == 0 {
		return fmt.Sprintf("No matches found for '%s' in %s", pattern, root), nil
	}
	return strings.Join(results, "\n"), nil
}

// ─── find_files ───────────────────────────────────────────────────────────────

type FindFilesTool struct{}

func NewFindFilesTool() *FindFilesTool { return &FindFilesTool{} }

func (t *FindFilesTool) Name()      string { return "find_files" }
func (t *FindFilesTool) Dangerous() bool   { return false }
func (t *FindFilesTool) Description() string {
	return "Finds files matching a name pattern (e.g. '*.go', 'README*') within a directory tree."
}
func (t *FindFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"pattern": map[string]any{"type": "string", "description": "Glob pattern for filename"},
		},
		"required": []string{"path", "pattern"},
	}
}
func (t *FindFilesTool) Execute(_ context.Context, args map[string]any) (string, error) {
	root, _ := args["path"].(string)
	pattern, _ := args["pattern"].(string)
	if root == "" || pattern == "" {
		return "", fmt.Errorf("find_files: 'path' and 'pattern' required")
	}
	var matches []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			if matched, _ := filepath.Match(pattern, d.Name()); matched {
				rel, _ := filepath.Rel(root, path)
				matches = append(matches, rel)
			}
		}
		return nil
	})
	if len(matches) == 0 {
		return fmt.Sprintf("No files found matching '%s' in %s", pattern, root), nil
	}
	if len(matches) > 100 {
		matches = append(matches[:100], fmt.Sprintf("... (%d more)", len(matches)-100))
	}
	return strings.Join(matches, "\n"), nil
}

// ─── create_dir ───────────────────────────────────────────────────────────────

type CreateDirTool struct{}

func NewCreateDirTool() *CreateDirTool { return &CreateDirTool{} }

func (t *CreateDirTool) Name()      string { return "create_dir" }
func (t *CreateDirTool) Dangerous() bool   { return false }
func (t *CreateDirTool) Description() string {
	return "Creates a directory (and all parent dirs) at the given path."
}
func (t *CreateDirTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
		"required":   []string{"path"},
	}
}
func (t *CreateDirTool) Execute(_ context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("create_dir: 'path' required")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return fmt.Sprintf("created directory: %s", path), nil
}

// ─── delete_file ──────────────────────────────────────────────────────────────

type DeleteFileTool struct{}

func NewDeleteFileTool() *DeleteFileTool { return &DeleteFileTool{} }

func (t *DeleteFileTool) Name()      string { return "delete_file" }
func (t *DeleteFileTool) Dangerous() bool   { return true }
func (t *DeleteFileTool) Description() string {
	return "Deletes a file (not a directory). DANGEROUS: requires user approval."
}
func (t *DeleteFileTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
		"required":   []string{"path"},
	}
}
func (t *DeleteFileTool) Execute(_ context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("delete_file: 'path' required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("delete_file: path is a directory")
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return fmt.Sprintf("deleted: %s", path), nil
}
