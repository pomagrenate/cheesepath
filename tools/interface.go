// Package tools defines the CrabTool interface and the tool registry.
package tools

import (
	"context"
	"fmt"
)

// ValidateArgs checks args against a JSON Schema subset (object with required
// fields and shallow type assertions). Returns a descriptive error on the first
// violation; returns nil if schema is nil or has no constraints to check.
// This does not replace full JSON Schema validation — it catches the most common
// mistake of the LLM omitting required fields or sending the wrong primitive type.
func ValidateArgs(schema map[string]any, args map[string]any) error {
	if schema == nil {
		return nil
	}
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)

	for _, field := range required {
		v, ok := args[field]
		if !ok || v == nil {
			return fmt.Errorf("missing required arg %q", field)
		}
		if s, ok := v.(string); ok && s == "" {
			return fmt.Errorf("required arg %q must not be empty", field)
		}
	}

	if props == nil {
		return nil
	}
	for field, spec := range props {
		val, ok := args[field]
		if !ok {
			continue // optional fields may be absent
		}
		specMap, ok := spec.(map[string]any)
		if !ok {
			continue
		}
		expected, _ := specMap["type"].(string)
		if expected == "" {
			continue
		}
		if err := checkArgType(field, val, expected); err != nil {
			return err
		}
	}
	return nil
}

func checkArgType(field string, val any, expected string) error {
	switch expected {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("arg %q: expected string, got %T", field, val)
		}
	case "number", "integer":
		switch val.(type) {
		case int, int32, int64, float32, float64:
			// ok
		default:
			return fmt.Errorf("arg %q: expected number, got %T", field, val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("arg %q: expected boolean, got %T", field, val)
		}
	case "array":
		if _, ok := val.([]any); !ok {
			return fmt.Errorf("arg %q: expected array, got %T", field, val)
		}
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("arg %q: expected object, got %T", field, val)
		}
	}
	return nil
}

// DangerousRanker is an optional extension to CrabTool. If a tool implements
// this interface, its danger level is used to customise the approval prompt.
type DangerousRanker interface {
	// DangerLevel returns 1 (low), 2 (medium), or 3 (high).
	DangerLevel() int
}

// ToolDangerLevel returns the numeric danger level of a tool.
// Returns 0 for safe tools, and 1/2/3 for dangerous tools that implement
// DangerousRanker (defaulting to 2 for dangerous tools that do not).
func ToolDangerLevel(t CrabTool) int {
	if !t.Dangerous() {
		return 0
	}
	if dr, ok := t.(DangerousRanker); ok {
		return dr.DangerLevel()
	}
	return 2
}

// CrabTool is the single interface every tool must implement.
type CrabTool interface {
	Name() string
	Description() string
	Schema() map[string]any
	Dangerous() bool
	Execute(ctx context.Context, args map[string]any) (string, error)
}

// Registry holds all registered CrabTools by name.
type Registry struct {
	tools map[string]CrabTool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]CrabTool)}
}

func (r *Registry) Register(t CrabTool) {
	if _, ok := r.tools[t.Name()]; ok {
		panic("crabchain: duplicate tool name: " + t.Name())
	}
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (CrabTool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) All() []CrabTool {
	out := make([]CrabTool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// DefaultRegistry returns a registry pre-loaded with all built-in tools.
func DefaultRegistry(cheesecrabAddr string) *Registry {
	r := NewRegistry()
	r.Register(NewReadFileTool())
	r.Register(NewWriteFileTool())
	r.Register(NewListDirTool())
	r.Register(NewGetFileInfoTool())
	r.Register(NewListDirRecursiveTool())
	r.Register(NewSearchFilesTool())
	r.Register(NewFindFilesTool())
	r.Register(NewCreateDirTool())
	r.Register(NewDeleteFileTool())
	r.Register(NewShellTool())
	r.Register(NewSysInfoTool())
	r.Register(NewListModelsTool(cheesecrabAddr))
	r.Register(NewSwitchModelTool(cheesecrabAddr))
	r.Register(NewApplyDiffTool())
	r.Register(NewGitTool())
	r.Register(NewHTTPRequestTool())
	r.Register(NewCrabTableTool())
	r.Register(NewMultiReplaceFileContentTool())
	r.Register(NewRunPythonTestTool())
	r.Register(NewRunGoTestTool())
	r.Register(NewRunLinterTool())
	// Extended tools: diff, search, JSON query, web fetch
	r.Register(NewDiffFilesTool())
	r.Register(NewGrepInFilesTool())
	r.Register(NewJSONQueryTool())
	r.Register(NewWebFetchTool())
	// Phase 8: Clinical Breakthrough Tools
	r.Register(NewVitalsMonitorTool(cheesecrabAddr)) 
	return r
}
