package parser

import "strings"

// TextParser returns the raw LLM output trimmed of whitespace.
type TextParser struct{}

func NewTextParser() *TextParser { return &TextParser{} }

func (p *TextParser) Parse(raw string) (string, error) {
	return strings.TrimSpace(raw), nil
}
