// Package agent contains the CrabAgent — the main ReAct-loop execution engine.
// It takes a goal, reasons over it via the LLM, dispatches tool calls,
// feeds observations back, and repeats until it has a final answer.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AutoCookies/crabpath/llm"
	"github.com/AutoCookies/crabpath/tools"
)

// ─── Config ──────────────────────────────────────────────────────────────────

// Config controls agent runtime behaviour.
type Config struct {
	// MaxSteps is the hard cap on ReAct iterations before the agent gives up.
	MaxSteps int
	// Model is the GGUF model name to use (must be loaded in cheese-server).
	Model string
	// LLMAddr is the base URL of the cheese-server, e.g. "http://127.0.0.1:8081"
	LLMAddr string
}

func DefaultConfig() Config {
	return Config{
		MaxSteps: 20,
		Model:    "default",
		LLMAddr:  "http://127.0.0.1:8081",
	}
}

// ─── CrabAgent ────────────────────────────────────────────────────────────────

// CrabAgent orchestrates the full path execution loop.
type CrabAgent struct {
	cfg      Config
	registry *tools.Registry
	memory   *Memory
	llm      *llm.Client
}

// NewCrabAgent constructs an agent with the given config and tool registry.
func NewCrabAgent(cfg Config, registry *tools.Registry) *CrabAgent {
	return &CrabAgent{
		cfg:      cfg,
		registry: registry,
		memory:   NewMemory(),
		llm:      llm.NewClient(cfg.LLMAddr),
	}
}

// RunPath executes the full agent loop for the given goal, streaming events
// back on the returned channel. The channel is closed when the run ends.
// Callers should drain the channel fully to avoid goroutine leaks.
func (a *CrabAgent) RunPath(ctx context.Context, goal string) (<-chan StreamEvent, *CrabPath) {
	events := make(chan StreamEvent, 32)

	path := &CrabPath{
		ID:        fmt.Sprintf("crab-%d", time.Now().UnixNano()),
		Goal:      goal,
		Status:    PathRunning,
		StartedAt: time.Now(),
	}

	go func() {
		defer close(events)
		defer func() {
			now := time.Now()
			path.EndedAt = &now
		}()

		// Build conversation history starting from the system prompt
		history := []llm.Message{
			{Role: "system", Content: a.buildSystemPrompt()},
			{Role: "user", Content: "Goal: " + goal + "\n\nCrawling the path... 🦀 Begin reasoning."},
		}

		for step := 0; step < a.cfg.MaxSteps; step++ {
			// ── 1. Call LLM (streaming so UI shows tokens in real time) ────────
			events <- StreamEvent{Type: EventThinking, Step: step, Payload: ""}

			tokenCh, fullCh, errCh := a.llm.CompleteStream(ctx, llm.Request{
				Model:    a.cfg.Model,
				Messages: history,
				// NOTE: stream:true — grammar is NOT sent here (llama.cpp does not
				// support grammar + streaming simultaneously in many builds).
				// We stream freely, get full text, then re-parse.
			})

			var raw string
			for {
				select {
				case tok, ok := <-tokenCh:
					if !ok {
						tokenCh = nil
					} else if tok != "" {
						events <- StreamEvent{Type: EventStreamToken, Step: step, Payload: tok}
					}
				case full, ok := <-fullCh:
					if ok {
						raw = full
					}
					fullCh = nil
				case err, ok := <-errCh:
					if ok && err != nil {
						events <- StreamEvent{Type: EventError, Step: step, Payload: err.Error()}
						path.Status = PathFailed
						return
					}
					errCh = nil
				case <-ctx.Done():
					events <- StreamEvent{Type: EventError, Step: step, Payload: "context cancelled"}
					path.Status = PathFailed
					return
				}
				if tokenCh == nil && fullCh == nil && errCh == nil {
					break
				}
			}
			if raw == "" {
				events <- StreamEvent{Type: EventError, Step: step, Payload: "empty response from model"}
				path.Status = PathFailed
				return
			}

			// ── 2. Parse the model output ─────────────────────────────────
			thought, toolCalls, err := parseOutput(raw)
			if err != nil {
				// Retry hint: just re-ask the model to fix its output
				history = append(history, llm.Message{
					Role:    "user",
					Content: "Your response was not valid JSON. Please respond with valid JSON matching the required schema.",
				})
				continue
			}

			events <- StreamEvent{Type: EventThought, Step: step, Payload: thought}

			// ── 3. Final answer → done ────────────────────────────────────
			if thought.IsFinal {
				path.Answer = thought.FinalAnswer
				path.Status = PathCompleted
				events <- StreamEvent{Type: EventFinalAnswer, Step: step, Payload: thought.FinalAnswer}
				history = append(history, llm.Message{Role: "assistant", Content: raw})
				_ = a.memory.SavePath(path)
				return
			}

			// ── 4. Execute tool calls ─────────────────────────────────────
			crabStep := CrabStep{Index: step, Thought: thought, ToolCalls: toolCalls}
			var observations []string

			for i, tc := range toolCalls {
				tool, ok := a.registry.Get(tc.ToolName)
				if !ok {
					crabStep.ToolCalls[i].Error = "unknown tool: " + tc.ToolName
					observations = append(observations, fmt.Sprintf("[%s]: ERROR — unknown tool", tc.ToolName))
					continue
				}

				// Mark dangerous tools; the server/UI handles approval gating
				if tool.Dangerous() {
					crabStep.ToolCalls[i].Dangerous = true
					events <- StreamEvent{Type: EventApprovalReq, Step: step, Payload: tc}
					// In the HTTP handler, the caller can pause and wait for approval signal.
					// For CLI/test use, we auto-approve here.
				}

				result, execErr := tool.Execute(ctx, tc.Args)
				if execErr != nil {
					crabStep.ToolCalls[i].Error = execErr.Error()
					observations = append(observations, fmt.Sprintf("[%s]: ERROR — %v", tc.ToolName, execErr))
				} else {
					crabStep.ToolCalls[i].Result = result
					observations = append(observations, fmt.Sprintf("[%s]: %s", tc.ToolName, result))
				}
			}

			obs := strings.Join(observations, "\n")
			crabStep.Observation = obs
			path.Steps = append(path.Steps, crabStep)

			events <- StreamEvent{Type: EventObservation, Step: step, Payload: obs}

			// Feed tool results back into conversation
			history = append(history,
				llm.Message{Role: "assistant", Content: raw},
				llm.Message{Role: "user", Content: "Observation:\n" + obs + "\n\nContinue reasoning toward the goal."},
			)
		}

		// Exceeded max steps
		path.Status = PathFailed
		events <- StreamEvent{Type: EventError, Step: a.cfg.MaxSteps, Payload: "max steps exceeded — path abandoned 🦀"}
	}()

	return events, path
}

// ─── Prompt helpers ────────────────────────────────────────────────────────

func (a *CrabAgent) buildSystemPrompt() string {
	var sb strings.Builder
	sb.WriteString(`You are CrabAgent, a powerful local AI agent running inside Cheesecrab Super.
You execute goals autonomously on the user's machine using available tools.
You MUST respond with valid JSON exactly matching this schema:

{
  "reasoning": "<your chain-of-thought>",
  "plan": "<brief next step>",
  "is_final": false,
  "tool_calls": [
    {"tool": "<tool_name>", "args": {<arg_key>: <arg_value>}}
  ]
}

OR if you have the final answer:

{
  "reasoning": "<why you are done>",
  "is_final": true,
  "final_answer": "<complete answer to the user's goal>",
  "tool_calls": []
}

Available tools:
`)
	for _, t := range a.registry.All() {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", t.Name(), t.Description()))
	}
	sb.WriteString("\nBe thorough, safe, and precise. Never make up results. Execute tools to verify.")
	return sb.String()
}

// parseOutput extracts a CrabThought and any tool calls from a raw LLM string.
func parseOutput(raw string) (CrabThought, []CrabToolCall, error) {
	// Extract JSON — strip any markdown fences if present
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		if len(lines) > 2 {
			raw = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var parsed struct {
		Reasoning   string         `json:"reasoning"`
		Plan        string         `json:"plan"`
		IsFinal     bool           `json:"is_final"`
		FinalAnswer string         `json:"final_answer"`
		ToolCalls   []CrabToolCall `json:"tool_calls"`
	}

	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return CrabThought{}, nil, fmt.Errorf("parse error: %w", err)
	}

	thought := CrabThought{
		Reasoning:   parsed.Reasoning,
		Plan:        parsed.Plan,
		IsFinal:     parsed.IsFinal,
		FinalAnswer: parsed.FinalAnswer,
	}
	return thought, parsed.ToolCalls, nil
}

// thoughtGrammar is a GBNF grammar string passed to cheese-server to enforce
// structured JSON output from the model.
// This corresponds to the CrabThought schema.
const thoughtGrammar = `
root   ::= "{" ws "\"reasoning\"" ws ":" ws string ws
             ( "," ws "\"plan\"" ws ":" ws string ws )?
           "," ws "\"is_final\"" ws ":" ws boolean ws
           ( "," ws "\"final_answer\"" ws ":" ws string ws )?
           ( "," ws "\"tool_calls\"" ws ":" ws tool-array ws )?
         "}"
tool-array ::= "[" ws ( tool-call ( ws "," ws tool-call )* )? ws "]"
tool-call  ::= "{" ws "\"tool\"" ws ":" ws string ws "," ws "\"args\"" ws ":" ws object ws "}"
object     ::= "{" ws ( kv ( ws "," ws kv )* )? ws "}"
kv         ::= string ws ":" ws value
value      ::= string | number | boolean | "null" | array | object
array      ::= "[" ws ( value ( ws "," ws value )* )? ws "]"
string     ::= "\"" ( [^"\\] | "\\" ["\\/bfnrt] )* "\""
number     ::= "-"? ( "0" | [1-9][0-9]* ) ( "." [0-9]+ )? ( [eE] [+-]? [0-9]+ )?
boolean    ::= "true" | "false"
ws         ::= [ \t\n\r]*
`
