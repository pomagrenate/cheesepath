package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ─── apply_code_diff ──────────────────────────────────────────────────────────

type ApplyDiffTool struct{}

func NewApplyDiffTool() *ApplyDiffTool { return &ApplyDiffTool{} }

func (t *ApplyDiffTool) Name()        string { return "apply_code_diff" }
func (t *ApplyDiffTool) Dangerous()   bool   { return true }
func (t *ApplyDiffTool) Description() string {
	return "Applies a unified diff patch to a file. DANGEROUS: modifies source code. User approval required."
}
func (t *ApplyDiffTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_file": map[string]any{"type": "string", "description": "Path to the file to patch"},
			"diff":        map[string]any{"type": "string", "description": "Unified diff content (--- / +++ format)"},
		},
		"required": []string{"target_file", "diff"},
	}
}
func (t *ApplyDiffTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	targetFile, _ := args["target_file"].(string)
	diff, _ := args["diff"].(string)
	if targetFile == "" || diff == "" {
		return "", fmt.Errorf("apply_code_diff: 'target_file' and 'diff' required")
	}
	tmp, err := os.CreateTemp("", "crabpatch-*.patch")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(diff); err != nil {
		return "", err
	}
	tmp.Close()

	out, err := exec.CommandContext(ctx, "patch", "--forward", targetFile, tmp.Name()).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("apply_code_diff: patch failed: %w", err)
	}
	return fmt.Sprintf("patch applied to %s: %s", targetFile, strings.TrimSpace(string(out))), nil
}

// ─── git_commit ───────────────────────────────────────────────────────────────

type GitTool struct{}

func NewGitTool() *GitTool { return &GitTool{} }

func (t *GitTool) Name()        string { return "git_commit" }
func (t *GitTool) Dangerous()   bool   { return true }
func (t *GitTool) Description() string {
	return "Stages all changes and creates a git commit. DANGEROUS: requires user approval."
}
func (t *GitTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"repo_path": map[string]any{"type": "string", "description": "Path to git repo"},
			"message":   map[string]any{"type": "string", "description": "Commit message"},
		},
		"required": []string{"repo_path", "message"},
	}
}
func (t *GitTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	repoPath, _ := args["repo_path"].(string)
	message, _ := args["message"].(string)
	if repoPath == "" || message == "" {
		return "", fmt.Errorf("git_commit: 'repo_path' and 'message' required")
	}
	addOut, err := exec.CommandContext(ctx, "git", "-C", repoPath, "add", "-A").CombinedOutput()
	if err != nil {
		return string(addOut), fmt.Errorf("git add failed: %w", err)
	}
	commitOut, err := exec.CommandContext(ctx, "git", "-C", repoPath, "commit", "-m", message).CombinedOutput()
	if err != nil {
		return string(commitOut), fmt.Errorf("git commit failed: %w", err)
	}
	return strings.TrimSpace(string(commitOut)), nil
}
