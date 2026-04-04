package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ReActStrategy implements the classic Reasoning + Acting loop.
// The model outputs a JSON block with reasoning, plan, is_final, and tool_calls.
type ReActStrategy struct{}

func NewReActStrategy() *ReActStrategy { return &ReActStrategy{} }

func (s *ReActStrategy) Name() string { return "react" }

func (s *ReActStrategy) Grammar() string { return thoughtGrammar }

func (s *ReActStrategy) BuildSystemPrompt(toolDescs string) string {
	var sb strings.Builder
	sb.WriteString(`You are CrabAgent, a powerful local AI agent running inside Cheesecrab.
You execute goals autonomously on the user's machine using available tools.

CRITICAL RULES:
1. You MUST respond with valid JSON exactly matching the schema.
2. If the user's goal involves a spreadsheet, you MUST use the "crabtable" tool.
3. Do not just reason about the task—call tools when they are needed, then read observations.
4. When you have enough information (including after a tool returns empty or an error), stop calling tools and respond with is_final true and final_answer.

RESPONSE FORMAT (JSON ONLY):
{
  "reasoning": "<why you are calling the tool>",
  "plan": "<brief next step>",
  "is_final": false,
  "tool_calls": [
    {"tool": "<tool_name>", "args": {<key>: <value>}}
  ]
}

OR when you have actually verified the task is complete:
{
  "reasoning": "<summary of actions taken>",
  "is_final": true,
  "final_answer": "<complete answer>",
  "tool_calls": []
}

EXAMPLES:

User: hello
{
  "reasoning": "Simple greeting, no tools needed.",
  "is_final": true,
  "final_answer": "Hello! How can I help you today?",
  "tool_calls": []
}

User: what is the current project version?
{
  "reasoning": "I need to check the project files for a version string.",
  "plan": "List files in the current directory to find a VERSION or package file.",
  "is_final": false,
  "tool_calls": [{"tool": "list_dir", "args": {"path": "."}}]
}

User: what is quantum entanglement?
{
  "reasoning": "This is a factual question. I should search the local PomaiDB database.",
  "plan": "Fetch context from the local store.",
  "is_final": false,
  "tool_calls": [{"tool": "rag_retrieve", "args": {"query": "what is quantum entanglement"}}]
}

Available tools:
`)
	sb.WriteString(toolDescs)
	sb.WriteString("\n\nBe thorough, safe, and precise. Do not invent quoted text from tools; if a tool returned nothing useful, say so in final_answer.")
	return sb.String()
}

func (s *ReActStrategy) ParseResponse(raw string) (CrabThought, []CrabToolCall, error) {
	start := strings.Index(raw, "{")
	last := strings.LastIndex(raw, "}")
	if start == -1 || last == -1 || last <= start {
		return CrabThought{}, nil, fmt.Errorf("no JSON object found in response")
	}
	jsonStr := raw[start : last+1]

	var parsed struct {
		Reasoning   string        `json:"reasoning"`
		Plan        string        `json:"plan"`
		IsFinal     bool          `json:"is_final"`
		FinalAnswer string        `json:"final_answer"`
		ToolCalls   []CrabToolCall `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
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

// thoughtGrammar constrains ReAct JSON; root is one line (cheese GBNF rejects a split root rule).
var thoughtGrammar = `
root ::= "{" ws "\"reasoning\"" ws ":" ws string ws ( "," ws "\"plan\"" ws ":" ws string ws )? "," ws "\"is_final\"" ws ":" ws boolean ws ( "," ws "\"final_answer\"" ws ":" ws string ws )? ( "," ws "\"tool_calls\"" ws ":" ws tool-array ws )? "}"
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
