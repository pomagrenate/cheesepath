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
You MUST respond with valid JSON exactly matching this schema:

{
  "reasoning": "<your chain-of-thought>",
  "plan": "<brief next step>",
  "is_final": false,
  "tool_calls": [
    {"tool": "<tool_name>", "args": {<key>: <value>}}
  ]
}

OR when you have the final answer:

{
  "reasoning": "<why you are done>",
  "is_final": true,
  "final_answer": "<complete answer to the user goal>",
  "tool_calls": []
}

Available tools:
`)
	sb.WriteString(toolDescs)
	sb.WriteString("\n\nBe thorough, safe, and precise. Never fabricate results. Use tools to verify.")
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

// thoughtGrammar is the GBNF grammar enforcing the ReAct JSON format.
// Passed to cheese-server to constrain model decoding.
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
