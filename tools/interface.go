// Package tools defines the CrabTool interface and the tool registry.
package tools

import "context"

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
	return r
}
