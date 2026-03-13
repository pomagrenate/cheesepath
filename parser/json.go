package parser

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSONParser[T] parses a JSON object or array from LLM output into T.
// Strips markdown code fences and tolerates conversational text around the JSON.
type JSONParser[T any] struct{}

func NewJSONParser[T any]() *JSONParser[T] { return &JSONParser[T]{} }

func (p *JSONParser[T]) Parse(raw string) (T, error) {
	var zero T
	cleaned := stripMarkdownFences(raw)
	jsonStr, err := extractJSON(cleaned)
	if err != nil {
		return zero, fmt.Errorf("parser: %w (raw: %.200s)", err, raw)
	}
	var out T
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return zero, fmt.Errorf("parser: unmarshal: %w", err)
	}
	return out, nil
}

func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"```json", "```JSON", "```"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			if idx := strings.LastIndex(s, "```"); idx != -1 {
				s = s[:idx]
			}
			return strings.TrimSpace(s)
		}
	}
	return s
}

func extractJSON(s string) (string, error) {
	for i, ch := range s {
		if ch == '{' || ch == '[' {
			open, close := ch, matching(ch)
			depth := 0
			inStr := false
			escape := false
			for j := i; j < len(s); j++ {
				c := rune(s[j])
				if escape {
					escape = false
					continue
				}
				if c == '\\' && inStr {
					escape = true
					continue
				}
				if c == '"' {
					inStr = !inStr
					continue
				}
				if inStr {
					continue
				}
				if c == open {
					depth++
				} else if c == close {
					depth--
					if depth == 0 {
						return s[i : j+1], nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("no JSON object or array found in output")
}

func matching(open rune) rune {
	if open == '{' {
		return '}'
	}
	return ']'
}

// ListParser splits LLM output into a []string by a separator.
type ListParser struct{ sep string }

func NewListParser(sep string) *ListParser {
	if sep == "" {
		sep = "\n"
	}
	return &ListParser{sep: sep}
}

func (p *ListParser) Parse(raw string) ([]string, error) {
	parts := strings.Split(strings.TrimSpace(raw), p.sep)
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}
