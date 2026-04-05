package panel

import (
	"strings"

	"github.com/AutoCookies/crabpath/agent"
)

// BuiltinRole returns a predefined Role configuration by name.
// Returns false if the name is not recognised.
//
// Supported names (case-insensitive):
//
//	researcher  — ReAct, reads files and searches for facts
//	critic      — Reflection, evaluates quality and finds flaws
//	planner     — PlanAndExecute, breaks work into ordered steps
//	architect   — Architect, designs systems before acting
//	implementer — ReAct, executes tasks using all available tools
//	synthesizer — ReAct, pure-reasoning role with no tool access
func BuiltinRole(name string) (Role, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "researcher":
		return Role{
			Name:     "Researcher",
			Strategy: agent.NewReActStrategy(),
			SystemPrefix: "You are a thorough researcher. Your job is to gather all relevant " +
				"facts, read files, search the codebase, and fetch information. " +
				"Do not evaluate or plan — only investigate and report what you find.",
			ToolFilter: []string{
				"read_file", "search_files", "list_dir",
				"grep_in_files", "web_fetch", "web_search",
				"git_context", "diff_files", "json_query",
			},
		}, true

	case "critic":
		return Role{
			Name:     "Critic",
			Strategy: agent.NewReflectionStrategy(),
			SystemPrefix: "You are a strict critic and quality evaluator. Your job is to identify " +
				"weaknesses, risks, edge cases, and flaws in the approach or implementation. " +
				"Be specific and actionable. Do not implement — only evaluate.",
			ToolFilter: []string{
				"read_file", "diff_files", "grep_in_files",
				"critic_review", "search_files", "git_context",
			},
		}, true

	case "planner":
		return Role{
			Name:     "Planner",
			Strategy: agent.NewPlanAndExecuteStrategy(),
			SystemPrefix: "You are a meticulous planner. Your job is to produce a clear, " +
				"ordered execution plan with concrete steps. Think about dependencies, risks, " +
				"and sequencing. Output a numbered plan — do not execute the steps.",
			ToolFilter: []string{
				"read_file", "list_dir", "search_files",
				"git_context", "grep_in_files",
			},
		}, true

	case "architect":
		return Role{
			Name:     "Architect",
			Strategy: agent.NewArchitectStrategy(),
			SystemPrefix: "You are a system architect. Your job is to design the structure, " +
				"interfaces, and high-level approach before any implementation. " +
				"Focus on correctness of design, separation of concerns, and extensibility.",
			ToolFilter: []string{
				"read_file", "list_dir", "git_context",
				"search_files", "grep_in_files",
			},
		}, true

	case "implementer":
		return Role{
			Name:     "Implementer",
			Strategy: agent.NewReActStrategy(),
			SystemPrefix: "You are a hands-on implementer. Your job is to carry out concrete " +
				"tasks using the available tools — read files, run tests, write code, " +
				"and verify results. Focus on making things work.",
			// No ToolFilter — implementer gets all tools.
		}, true

	case "synthesizer":
		return Role{
			Name:     "Synthesizer",
			Strategy: agent.NewReActStrategy(),
			SystemPrefix: "You are a synthesis expert. Your job is to take all available " +
				"information and produce a unified, clear, concise final answer. " +
				"Do not call tools — reason only from what you already know.",
			// ToolFilter blocks all tools so this role is pure reasoning.
			ToolFilter: []string{"__none__"},
		}, true
	}

	return Role{}, false
}

// ParseRoles converts a comma-separated string of role names into a []Role slice.
// Unknown names fall back to a generic ReAct role with that name.
func ParseRoles(roleNames string) []Role {
	var roles []Role
	for _, name := range strings.Split(roleNames, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if r, ok := BuiltinRole(name); ok {
			roles = append(roles, r)
		} else {
			// Generic fallback: ReAct with the given name, no tool filter.
			roles = append(roles, Role{
				Name:     strings.Title(name),
				Strategy: agent.NewReActStrategy(),
			})
		}
	}
	if len(roles) == 0 {
		// Default panel if nothing specified.
		r1, _ := BuiltinRole("researcher")
		r2, _ := BuiltinRole("critic")
		r3, _ := BuiltinRole("planner")
		roles = []Role{r1, r2, r3}
	}
	return roles
}
