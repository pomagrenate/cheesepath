package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// planGrammar constrains the planning turn to a JSON list of step strings.
const planGrammar = `
root       ::= "{" ws "\"steps\"" ws ":" ws step-array ws "}"
step-array ::= "[" ws string ( ws "," ws string )* ws "]"
string     ::= "\"" ( [^"\\] | "\\" ["\\/bfnrt] )* "\""
ws         ::= [ \t\n\r]*
`

// PlanAndExecuteStrategy is a two-phase strategy:
//  1. Planning turn: LLM emits a numbered step list ({"steps": [...]}).
//  2. Execution turns: LLM executes each step in order using standard ReAct JSON.
//
// The executor's Run loop is unchanged; all state lives inside the strategy struct.
type PlanAndExecuteStrategy struct {
	mu          sync.Mutex
	phase       string   // "plan" | "execute"
	plan        []string // populated after planning turn
	currentStep int
}

// NewPlanAndExecuteStrategy creates a PlanAndExecuteStrategy in the plan phase.
func NewPlanAndExecuteStrategy() *PlanAndExecuteStrategy {
	return &PlanAndExecuteStrategy{phase: "plan"}
}

func (s *PlanAndExecuteStrategy) Name() string { return "plan_and_execute" }

// Grammar returns the planning grammar for turn 1, and the ReAct grammar thereafter.
func (s *PlanAndExecuteStrategy) Grammar() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase == "plan" {
		return planGrammar
	}
	return thoughtGrammar
}

// BuildSystemPrompt informs the model about the two-phase protocol.
// It is called once before the Run loop so it cannot reference per-step state;
// step context is communicated via CrabThought.Reasoning in prior history turns.
func (s *PlanAndExecuteStrategy) BuildSystemPrompt(toolDescs string) string {
	var sb strings.Builder
	sb.WriteString(`You are CrabAgent operating in Plan-and-Execute mode.

PHASE 1 — PLANNING (your very first response):
Output ONLY a JSON object listing the concrete steps needed to accomplish the goal:
{"steps": ["step1 description", "step2 description", ...]}

Do not call any tools. Do not include any other text. List 2-8 specific, actionable steps.

PHASE 2 — EXECUTION (all subsequent responses):
Execute the steps you planned, in order, using the ReAct JSON format:
{
  "reasoning": "[Step N/M: <step description>] <why this tool / what you observe>",
  "plan": "<next micro-action>",
  "is_final": false,
  "tool_calls": [{"tool": "<name>", "args": {...}}]
}

When all steps are done:
{
  "reasoning": "<summary of what was accomplished>",
  "is_final": true,
  "final_answer": "<complete answer>",
  "tool_calls": []
}

Available tools:
`)
	sb.WriteString(toolDescs)
	sb.WriteString("\n\nAlways reference your plan step in the reasoning field so progress is visible.")
	return sb.String()
}

// ParseResponse handles both phases by inspecting internal state.
func (s *PlanAndExecuteStrategy) ParseResponse(raw string) (CrabThought, []CrabToolCall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.phase == "plan" {
		return s.parsePlanPhase(raw)
	}
	return s.parseExecutePhase(raw)
}

func (s *PlanAndExecuteStrategy) parsePlanPhase(raw string) (CrabThought, []CrabToolCall, error) {
	start := strings.Index(raw, "{")
	last := strings.LastIndex(raw, "}")
	if start == -1 || last == -1 || last <= start {
		return CrabThought{}, nil, fmt.Errorf("plan phase: no JSON object found in response")
	}
	jsonStr := raw[start : last+1]

	var parsed struct {
		Steps []string `json:"steps"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil || len(parsed.Steps) == 0 {
		return CrabThought{}, nil, fmt.Errorf("plan phase: expected {\"steps\":[...]}, got: %s", jsonStr)
	}

	s.plan = parsed.Steps
	s.phase = "execute"
	s.currentStep = 0

	// Build a readable plan summary for the UI/callbacks.
	var planText strings.Builder
	planText.WriteString("Execution plan:\n")
	for i, step := range parsed.Steps {
		planText.WriteString(fmt.Sprintf("  %d. %s\n", i+1, step))
	}

	reasoning := strings.Join(func() []string {
		parts := make([]string, len(parsed.Steps))
		for i, p := range parsed.Steps {
			parts[i] = fmt.Sprintf("%d. %s", i+1, p)
		}
		return parts
	}(), " → ")

	thought := CrabThought{
		Reasoning: "Plan created: " + reasoning,
		Plan:      planText.String(),
		IsFinal:   false,
	}
	return thought, nil, nil
}

func (s *PlanAndExecuteStrategy) parseExecutePhase(raw string) (CrabThought, []CrabToolCall, error) {
	thought, calls, err := (&ReActStrategy{}).ParseResponse(raw)
	if err != nil {
		return thought, calls, err
	}

	// Inject step context into reasoning if not already present.
	if len(s.plan) > 0 && s.currentStep < len(s.plan) {
		stepLabel := fmt.Sprintf("[Step %d/%d: %s] ", s.currentStep+1, len(s.plan), s.plan[s.currentStep])
		if !strings.HasPrefix(thought.Reasoning, "[Step") {
			thought.Reasoning = stepLabel + thought.Reasoning
		}
	}

	if !thought.IsFinal {
		s.currentStep++
		if s.currentStep >= len(s.plan) {
			s.currentStep = len(s.plan) - 1
		}
	}

	return thought, calls, nil
}
