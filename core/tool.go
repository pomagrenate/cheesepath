package core

import (
	"context"
	"fmt"
	"sync"
)

// Tool is the standard interface for callable tools in Cheesepath.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, args map[string]any) (string, error)
}

// FuncTool allows wrapping an anonymous or standalone function as a Tool.
type FuncTool struct {
	name        string
	description string
	schema      map[string]any
	fn          func(ctx context.Context, args map[string]any) (string, error)
}

// NewFuncTool constructs a Tool from a Go function.
func NewFuncTool(
	name string,
	description string,
	schema map[string]any,
	fn func(ctx context.Context, args map[string]any) (string, error),
) Tool {
	return &FuncTool{
		name:        name,
		description: description,
		schema:      schema,
		fn:          fn,
	}
}

func (f *FuncTool) Name() string        { return f.name }
func (f *FuncTool) Description() string { return f.description }
func (f *FuncTool) Schema() map[string]any {
	if f.schema == nil {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	return f.schema
}
func (f *FuncTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := ValidateToolArgs(f.Schema(), args); err != nil {
		return "", fmt.Errorf("tool %q args validation failed: %w", f.name, err)
	}
	return f.fn(ctx, args)
}

// ToolRegistry maintains a thread-safe catalog of tools.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get retrieves a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All returns a slice of all registered tools.
func (r *ToolRegistry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		list = append(list, t)
	}
	return list
}

// Names returns a list of registered tool names.
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Execute looks up and runs a tool by name.
func (r *ToolRegistry) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	tool, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("tool %q not found", name)
	}
	return tool.Execute(ctx, args)
}

// ValidateToolArgs performs lightweight validation of tool input args against JSON schema.
func ValidateToolArgs(schema map[string]any, args map[string]any) error {
	if schema == nil {
		return nil
	}
	if req, ok := schema["required"].([]string); ok {
		for _, field := range req {
			if _, exists := args[field]; !exists {
				return fmt.Errorf("missing required parameter %q", field)
			}
		}
	} else if reqAny, ok := schema["required"].([]any); ok {
		for _, item := range reqAny {
			if field, ok := item.(string); ok {
				if _, exists := args[field]; !exists {
					return fmt.Errorf("missing required parameter %q", field)
				}
			}
		}
	}
	return nil
}
