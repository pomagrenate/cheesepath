package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ── diff_files ────────────────────────────────────────────────────────────────

// DiffFilesTool compares two files and returns a unified diff.
type DiffFilesTool struct{}

func NewDiffFilesTool() *DiffFilesTool { return &DiffFilesTool{} }

func (t *DiffFilesTool) Name() string      { return "diff_files" }
func (t *DiffFilesTool) Dangerous() bool   { return false }
func (t *DiffFilesTool) Description() string {
	return "Compare two files and return a unified diff showing the differences. Useful for reviewing changes between versions."
}
func (t *DiffFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_a":        map[string]any{"type": "string", "description": "Path to the first (original) file"},
			"file_b":        map[string]any{"type": "string", "description": "Path to the second (modified) file"},
			"context_lines": map[string]any{"type": "number", "description": "Number of context lines around each change (default: 3)"},
		},
		"required": []string{"file_a", "file_b"},
	}
}

func (t *DiffFilesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	fileA, _ := args["file_a"].(string)
	fileB, _ := args["file_b"].(string)
	if fileA == "" || fileB == "" {
		return "", fmt.Errorf("diff_files: file_a and file_b are required")
	}

	ctxLines := 3
	if v, ok := args["context_lines"].(float64); ok && v >= 0 {
		ctxLines = int(v)
	}

	cmd := exec.CommandContext(ctx, "diff", "-u",
		"-U", strconv.Itoa(ctxLines),
		"--label", fileA,
		"--label", fileB,
		fileA, fileB)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()

	// diff exits 1 when files differ — that is not an error condition.
	// Only exit code ≥ 2 is a real error.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			err = nil // files differ, not an execution error
		} else if err != nil {
			return "", fmt.Errorf("diff_files: %w", err)
		}
	}

	result := out.String()
	if result == "" {
		return "(files are identical)", nil
	}
	return truncate(result, 8000), nil
}

// ── grep_in_files ─────────────────────────────────────────────────────────────

// GrepInFilesTool searches file contents with a regex pattern.
type GrepInFilesTool struct{}

func NewGrepInFilesTool() *GrepInFilesTool { return &GrepInFilesTool{} }

func (t *GrepInFilesTool) Name() string      { return "grep_in_files" }
func (t *GrepInFilesTool) Dangerous() bool   { return false }
func (t *GrepInFilesTool) Description() string {
	return "Search for a regex pattern across files in a directory. Returns matching lines with line numbers and context. More powerful than search_files."
}
func (t *GrepInFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":       map[string]any{"type": "string", "description": "Regular expression pattern to search for"},
			"path":          map[string]any{"type": "string", "description": "Directory or file to search in (default: current directory)"},
			"context_lines": map[string]any{"type": "number", "description": "Lines of context before and after each match (default: 2)"},
			"include_glob":  map[string]any{"type": "string", "description": "Glob to filter files, e.g. '*.go' or '*.py' (optional)"},
		},
		"required": []string{"pattern"},
	}
}

func (t *GrepInFilesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return "", fmt.Errorf("grep_in_files: pattern is required")
	}

	searchPath := "."
	if v, ok := args["path"].(string); ok && v != "" {
		searchPath = v
	}

	ctxLines := 2
	if v, ok := args["context_lines"].(float64); ok && v >= 0 {
		ctxLines = int(v)
	}

	cmdArgs := []string{"-rn", "-C", strconv.Itoa(ctxLines), "--"}
	if glob, ok := args["include_glob"].(string); ok && glob != "" {
		cmdArgs = append([]string{"--include=" + glob}, cmdArgs...)
		cmdArgs = append([]string{"-rn", "-C", strconv.Itoa(ctxLines)}, cmdArgs[len(cmdArgs)-1:]...)
		// rebuild cleanly
		cmdArgs = []string{"-rn", "-C", strconv.Itoa(ctxLines), "--include=" + glob, "--", pattern, searchPath}
	} else {
		cmdArgs = append(cmdArgs, pattern, searchPath)
	}

	cmd := exec.CommandContext(ctx, "grep", cmdArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	// grep exits 1 when no matches — not an error.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "(no matches found)", nil
		}
		return "", fmt.Errorf("grep_in_files: %w", err)
	}

	result := out.String()
	lineCount := strings.Count(result, "\n")
	prefix := fmt.Sprintf("(grep found %d matching lines)\n", lineCount)
	return prefix + truncate(result, 8000), nil
}

// truncate limits a string to maxChars, appending a truncation notice.
func truncate(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + fmt.Sprintf("\n... (truncated, %d chars total)", len(s))
}
