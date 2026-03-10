// Package tools defines the CrabTool interface and the tool registry.
// Every built-in or user-supplied tool must implement this interface.
package tools

import "context"

// ─── CrabTool Interface ───────────────────────────────────────────────────────

// CrabTool is the single interface you implement to add any capability to the agent.
// All built-in tools (fs, shell, models, coding) implement this.
type CrabTool interface {
	// Name returns the snake_case identifier that the model uses in tool_calls.
	Name() string

	// Description is sent to the LLM in the system prompt to explain what the tool does.
	Description() string

	// Schema returns a JSON Schema object describing the expected args.
	// This is embedded in the grammar so the model outputs valid JSON.
	Schema() map[string]any

	// Dangerous returns true if this tool should pause for user approval.
	// E.g. shell execution, file deletion, git push.
	Dangerous() bool

	// Execute runs the tool with the provided args and returns a human-readable result.
	Execute(ctx context.Context, args map[string]any) (string, error)
}

// ─── Registry ────────────────────────────────────────────────────────────────

// Registry holds all registered CrabTools by name.
type Registry struct {
	tools map[string]CrabTool
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]CrabTool)}
}

// Register adds a tool. Panics on duplicate name (fail fast at startup).
func (r *Registry) Register(t CrabTool) {
	if _, ok := r.tools[t.Name()]; ok {
		panic("crabpath: duplicate tool name: " + t.Name())
	}
	r.tools[t.Name()] = t
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (CrabTool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns all tools as a slice (for listing in the system prompt / API).
func (r *Registry) All() []CrabTool {
	out := make([]CrabTool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// DefaultRegistry builds the canonical set of built-in tools.
// Call this from the server to get a ready-to-use Registry.
func DefaultRegistry(cheesecrabAddr string) *Registry {
	r := NewRegistry()
	r.Register(NewReadFileTool())
	r.Register(NewWriteFileTool())
	r.Register(NewListDirTool())
	r.Register(NewShellTool())
	r.Register(NewSysInfoTool())
	r.Register(NewListModelsTool(cheesecrabAddr))
	r.Register(NewSwitchModelTool(cheesecrabAddr))
	r.Register(NewApplyDiffTool())
	r.Register(NewGitTool())
	return r
}
