package agent

import (
	"encoding/json"
	"strings"
)

// reflectGrammar extends the ReAct grammar with an optional "reflection" field.
// The reflection field lets the model rate its own progress and detect when stuck.
var reflectGrammar = `
root ::= "{" ws "\"reasoning\"" ws ":" ws string ws ( "," ws "\"plan\"" ws ":" ws string ws )? "," ws "\"is_final\"" ws ":" ws boolean ws ( "," ws "\"final_answer\"" ws ":" ws string ws )? ( "," ws "\"tool_calls\"" ws ":" ws tool-array ws )? ( "," ws "\"reflection\"" ws ":" ws string ws )? "}"
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

// ReflectionStrategy embeds ReActStrategy and adds a mandatory self-assessment step.
// After each tool observation the model includes a "reflection" field that rates
// progress and identifies whether a course correction is needed.
type ReflectionStrategy struct {
	ReActStrategy
}

func NewReflectionStrategy() *ReflectionStrategy { return &ReflectionStrategy{} }

func (s *ReflectionStrategy) Name() string    { return "reflection" }
func (s *ReflectionStrategy) Grammar() string { return reflectGrammar }

func (s *ReflectionStrategy) BuildSystemPrompt(toolDescs string) string {
	var sb strings.Builder
	sb.WriteString(`You are CrabAgent operating in Reflection mode.

After every tool observation, you MUST include a "reflection" field in your JSON response.
The reflection should:
1. Rate your progress toward the goal (1=stuck, 5=almost done).
2. Note whether the last action helped or not.
3. Identify the next best action or if a different approach is needed.

RESPONSE FORMAT (JSON ONLY):
{
  "reasoning": "<why you are calling the tool / what you learned>",
  "plan": "<brief next step>",
  "is_final": false,
  "tool_calls": [{"tool": "<name>", "args": {...}}],
  "reflection": "Progress 3/5: the file was found but the function signature differs. Next I should read the function body."
}

OR when done:
{
  "reasoning": "<summary>",
  "is_final": true,
  "final_answer": "<complete answer>",
  "tool_calls": [],
  "reflection": "Progress 5/5: goal fully accomplished."
}

Available tools:
`)
	sb.WriteString(toolDescs)
	sb.WriteString("\n\nBe honest in your reflection. If you are stuck after 2 turns, propose a completely different approach.")
	return sb.String()
}

// ParseResponse delegates to ReActStrategy then extracts and prepends the reflection.
func (s *ReflectionStrategy) ParseResponse(raw string) (CrabThought, []CrabToolCall, error) {
	thought, calls, err := s.ReActStrategy.ParseResponse(raw)
	if err != nil {
		return thought, calls, err
	}

	// Extract the optional reflection field.
	start := strings.Index(raw, "{")
	last := strings.LastIndex(raw, "}")
	if start != -1 && last > start {
		var extra struct {
			Reflection string `json:"reflection"`
		}
		_ = json.Unmarshal([]byte(raw[start:last+1]), &extra)
		if extra.Reflection != "" {
			thought.Reasoning = "[Reflection]: " + extra.Reflection + "\n\n" + thought.Reasoning
		}
	}

	return thought, calls, nil
}
