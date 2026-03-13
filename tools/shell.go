package tools

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ─── safe_exec_shell ──────────────────────────────────────────────────────────

type ShellTool struct{}

func NewShellTool() *ShellTool { return &ShellTool{} }

func (t *ShellTool) Name()      string { return "safe_exec_shell" }
func (t *ShellTool) Dangerous() bool   { return true }
func (t *ShellTool) Description() string {
	return "Executes a shell command and returns its stdout. DANGEROUS: user must approve before execution."
}
func (t *ShellTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"command": map[string]any{"type": "string", "description": "Shell command to run"}},
		"required":   []string{"command"},
	}
}
func (t *ShellTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	cmd, _ := args["command"].(string)
	if cmd == "" {
		return "", fmt.Errorf("safe_exec_shell: 'command' required")
	}
	for _, blocked := range []string{"rm -rf /", "mkfs", "dd if=", ":(){:|:&};:"} {
		if strings.Contains(cmd, blocked) {
			return "", fmt.Errorf("safe_exec_shell: blocked dangerous pattern: %s", blocked)
		}
	}
	var shell, flag string
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/C"
	} else {
		shell, flag = "sh", "-c"
	}
	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(execCtx, shell, flag, cmd).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("safe_exec_shell: exit error: %w", err)
	}
	result := strings.TrimSpace(string(out))
	if len(result) > 4000 {
		result = result[:4000] + "\n... [truncated]"
	}
	return result, nil
}

// ─── get_system_info ──────────────────────────────────────────────────────────

type SysInfoTool struct{}

func NewSysInfoTool() *SysInfoTool { return &SysInfoTool{} }

func (t *SysInfoTool) Name()        string { return "get_system_info" }
func (t *SysInfoTool) Dangerous()   bool   { return false }
func (t *SysInfoTool) Description() string {
	return "Returns current system resource usage: CPU %, RAM used/total, OS, uptime."
}
func (t *SysInfoTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *SysInfoTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	out, err := exec.Command("sh", "-c",
		`echo "OS: $(uname -s) $(uname -r)"; `+
			`echo "CPU cores: $(nproc)"; `+
			`free -h 2>/dev/null || vm_stat 2>/dev/null | head -5`).CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
