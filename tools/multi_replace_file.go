package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type MultiReplaceFileContentTool struct{}

func NewMultiReplaceFileContentTool() *MultiReplaceFileContentTool {
	return &MultiReplaceFileContentTool{}
}

func (t *MultiReplaceFileContentTool) Name() string { return "multi_replace_file_content" }

func (t *MultiReplaceFileContentTool) Description() string {
	return "Use this tool to safely edit an existing file by applying block diffs instead of overwriting the whole file."
}

func (t *MultiReplaceFileContentTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"TargetFile":         map[string]any{"type": "string", "description": "Absolute path to the target file."},
			"TargetContent":      map[string]any{"type": "string", "description": "The exact string block to be replaced."},
			"ReplacementContent": map[string]any{"type": "string", "description": "The exact new string block to insert."},
		},
		"required": []string{"TargetFile", "TargetContent", "ReplacementContent"},
	}
}

func (t *MultiReplaceFileContentTool) Dangerous() bool { return false }

func (t *MultiReplaceFileContentTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	target, ok := args["TargetFile"].(string)
	if !ok {
		return "", fmt.Errorf("missing TargetFile")
	}
	oldContent, _ := args["TargetContent"].(string)
	newContent, _ := args["ReplacementContent"].(string)

	b, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	data := string(b)

	if !strings.Contains(data, oldContent) {
		return "", fmt.Errorf("target content not found in file")
	}

	res := strings.Replace(data, oldContent, newContent, 1)
	err = os.WriteFile(target, []byte(res), 0644)
	if err != nil {
		return "", err
	}

	// Post-edit formatting hook
	if strings.HasSuffix(target, ".go") {
		_ = exec.Command("gofmt", "-w", target).Run()
	} else if strings.HasSuffix(target, ".py") {
		// Try running black
		_ = exec.Command("python3", "-m", "black", target).Run()
	}

	return "Replacement successful.", nil
}
