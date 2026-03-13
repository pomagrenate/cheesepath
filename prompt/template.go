// Package prompt provides prompt-template primitives for building LLM inputs.
package prompt

import (
	"bytes"
	"fmt"
	"text/template"
)

// PromptTemplate renders a string from a Go text/template and a variable map.
type PromptTemplate struct {
	tmpl *template.Template
}

// NewTemplate parses tmplStr. Panics on syntax error.
func NewTemplate(tmplStr string) *PromptTemplate {
	return &PromptTemplate{tmpl: template.Must(template.New("").Parse(tmplStr))}
}

// MustNewTemplate parses tmplStr and returns an error instead of panicking.
func MustNewTemplate(tmplStr string) (*PromptTemplate, error) {
	t, err := template.New("").Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("prompt: parse: %w", err)
	}
	return &PromptTemplate{tmpl: t}, nil
}

// Format executes the template with vars.
func (p *PromptTemplate) Format(vars map[string]any) (string, error) {
	var buf bytes.Buffer
	if err := p.tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("prompt: execute: %w", err)
	}
	return buf.String(), nil
}
