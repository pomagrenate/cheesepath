package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type RunPythonTestTool struct{}

func NewRunPythonTestTool() *RunPythonTestTool { return &RunPythonTestTool{} }

func (t *RunPythonTestTool) Name() string { return "run_python_test" }
func (t *RunPythonTestTool) Dangerous() bool { return false }
func (t *RunPythonTestTool) Description() string {
	return "Execute a Python test script (e.g. pytest tests/test_api.py) and return the stdout/stderr. " +
		"Use this to verify your Python code works. This tool is explicitly safe to run autonomously."
}
func (t *RunPythonTestTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The exact pytest or python command to run (e.g. pytest tests/test_api.py)",
			},
		},
		"required": []string{"command"},
	}
}

func (t *RunPythonTestTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	cmdStr, _ := args["command"].(string)
	if cmdStr == "" {
		return "", fmt.Errorf("command is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	result := out.String()
	if err != nil {
		return fmt.Sprintf("Tests failed with error: %v\n\nOutput:\n%s", err, result), nil
	}
	return fmt.Sprintf("Tests passed successfully!\n\nOutput:\n%s", result), nil
}

type RunGoTestTool struct{}

func NewRunGoTestTool() *RunGoTestTool { return &RunGoTestTool{} }

func (t *RunGoTestTool) Name() string { return "run_go_test" }
func (t *RunGoTestTool) Dangerous() bool { return false }
func (t *RunGoTestTool) Description() string {
	return "Execute a Go test command (e.g. go test ./...) and return the output. " +
		"Use this to systematically verify your Go code edits."
}
func (t *RunGoTestTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The exact go test command to run (e.g. go test ./...)",
			},
		},
		"required": []string{"command"},
	}
}

func (t *RunGoTestTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	cmdStr, _ := args["command"].(string)
	if cmdStr == "" {
		return "", fmt.Errorf("command is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	result := out.String()
	if err != nil {
		return fmt.Sprintf("Go tests failed with error: %v\n\nOutput:\n%s", err, result), nil
	}
	return fmt.Sprintf("Go tests passed successfully!\n\nOutput:\n%s", result), nil
}

type RunLinterTool struct{}

func NewRunLinterTool() *RunLinterTool { return &RunLinterTool{} }

func (t *RunLinterTool) Name() string { return "run_linter" }
func (t *RunLinterTool) Dangerous() bool { return false }
func (t *RunLinterTool) Description() string {
	return "Execute a linter (ruff for Python, golangci-lint for Go) and return findings. " +
		"Use this to ensure architectural integrity and catch subtle bugs."
}
func (t *RunLinterTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"linter": map[string]any{
				"type":        "string",
				"enum":        []string{"ruff", "golangci-lint"},
				"description": "The linter to run.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "The file or directory to lint (e.g. . or cmd/agent/main.go).",
			},
		},
		"required": []string{"linter", "path"},
	}
}

func (t *RunLinterTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	linter, _ := args["linter"].(string)
	path, _ := args["path"].(string)

	var cmd *exec.Cmd
	if linter == "ruff" {
		cmd = exec.CommandContext(ctx, "python3", "-m", "ruff", "check", path)
	} else if linter == "golangci-lint" {
		cmd = exec.CommandContext(ctx, "golangci-lint", "run", path)
	} else {
		return "", fmt.Errorf("unsupported linter: %s", linter)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	_ = cmd.Run() // Linters often return non-zero for findings
	result := out.String()
	if result == "" {
		return fmt.Sprintf("%s found no issues in %s.", linter, path), nil
	}
	return fmt.Sprintf("%s findings for %s:\n\n%s", linter, path, result), nil
}
