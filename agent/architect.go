package agent

import (
	"strings"
)

// ArchitectStrategy forces the agent to design and plan before acting.
type ArchitectStrategy struct {
	ReActStrategy
}

func NewArchitectStrategy() *ArchitectStrategy { return &ArchitectStrategy{} }

func (s *ArchitectStrategy) Name() string { return "architect" }

func (s *ArchitectStrategy) BuildSystemPrompt(toolDescs string) string {
	var sb strings.Builder
	sb.WriteString(`You are the Cheeserag Architect, a high-level design specialist.
Your goal is to provide a comprehensive IMPLEMENTATION PLAN before any code changes are made.

CRITICAL ARCHITECT RULES:
1. In your first turn, you MUST output a "DESIGN BLOCK" in your reasoning.
2. The design block must outline: Affected Files, New Symbols, Logic flow, and Verification steps.
3. Do NOT call code-modifying tools (like write_file or multi_replace_file_content) until the user has seen your plan.
4. Use valid JSON matching the ReAct schema.

RESPONSE FORMAT (JSON ONLY):
{
  "reasoning": "DESIGN BLOCK:\n- Files: ...\n- Logic: ...\n- Tests: ...",
  "plan": "<brief plan>",
  "is_final": false,
  "tool_calls": []
}

Available tools:
`)
	sb.WriteString(toolDescs)
	sb.WriteString("\n\nBe meticulous and architectural. Your plan must be fail-safe.")
	return sb.String()
}
