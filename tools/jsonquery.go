package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// JSONQueryTool applies a jq-style query to a JSON string.
// It shells out to jq if available, and falls back to a minimal stdlib
// implementation that handles simple dot-path expressions.
type JSONQueryTool struct{}

func NewJSONQueryTool() *JSONQueryTool { return &JSONQueryTool{} }

func (t *JSONQueryTool) Name() string    { return "json_query" }
func (t *JSONQueryTool) Dangerous() bool { return false }
func (t *JSONQueryTool) Description() string {
	return "Apply a jq query to a JSON string and return the result. Uses the system jq binary when available, with a stdlib fallback for simple dot-path queries like .foo, .foo.bar, .[0]."
}
func (t *JSONQueryTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"json":  map[string]any{"type": "string", "description": "The JSON string to query"},
			"query": map[string]any{"type": "string", "description": "The jq query expression, e.g. '.name', '.items[0].id', '.[] | .title'"},
		},
		"required": []string{"json", "query"},
	}
}

func (t *JSONQueryTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	jsonStr, _ := args["json"].(string)
	query, _ := args["query"].(string)
	if jsonStr == "" || query == "" {
		return "", fmt.Errorf("json_query: json and query are required")
	}

	// Prefer system jq for full expression support.
	if jqPath, err := exec.LookPath("jq"); err == nil {
		return runJQ(ctx, jqPath, jsonStr, query)
	}

	// Stdlib fallback for simple dot-path expressions.
	result, err := stdlibQuery(jsonStr, query)
	if err != nil {
		return "", fmt.Errorf("json_query (stdlib mode, jq not installed): %w\nInstall jq for full expression support", err)
	}
	return result, nil
}

func runJQ(ctx context.Context, jqPath, jsonStr, query string) (string, error) {
	cmd := exec.CommandContext(ctx, jqPath, "-r", query)
	cmd.Stdin = strings.NewReader(jsonStr)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("jq error: %s", strings.TrimSpace(errBuf.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// stdlibQuery handles simple dot-path expressions: .foo, .foo.bar, .[0], .foo[0].bar
func stdlibQuery(jsonStr, query string) (string, error) {
	var root any
	if err := json.Unmarshal([]byte(jsonStr), &root); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}

	// Normalize query: strip leading dot
	q := strings.TrimSpace(query)
	if q == "." {
		b, _ := json.MarshalIndent(root, "", "  ")
		return string(b), nil
	}
	if !strings.HasPrefix(q, ".") {
		return "", fmt.Errorf("only dot-path queries supported in stdlib mode (e.g. .foo, .items[0])")
	}
	q = q[1:] // strip leading dot

	// Walk the path segments
	current := root
	for q != "" {
		var segment string
		if strings.HasPrefix(q, "[") {
			// Array index: [N]
			end := strings.Index(q, "]")
			if end == -1 {
				return "", fmt.Errorf("unmatched '[' in query")
			}
			idxStr := q[1:end]
			idx, err := strconv.Atoi(idxStr)
			if err != nil {
				return "", fmt.Errorf("invalid array index: %s", idxStr)
			}
			arr, ok := current.([]any)
			if !ok {
				return "", fmt.Errorf("expected array at this position")
			}
			if idx < 0 || idx >= len(arr) {
				return "", fmt.Errorf("index %d out of bounds (len=%d)", idx, len(arr))
			}
			current = arr[idx]
			q = q[end+1:]
			if strings.HasPrefix(q, ".") {
				q = q[1:]
			}
			continue
		}

		// Object key: foo or foo[...
		dotIdx := strings.Index(q, ".")
		bracketIdx := strings.Index(q, "[")
		switch {
		case dotIdx == -1 && bracketIdx == -1:
			segment = q
			q = ""
		case dotIdx == -1:
			segment = q[:bracketIdx]
			q = q[bracketIdx:]
		case bracketIdx == -1:
			segment = q[:dotIdx]
			q = q[dotIdx+1:]
		case dotIdx < bracketIdx:
			segment = q[:dotIdx]
			q = q[dotIdx+1:]
		default:
			segment = q[:bracketIdx]
			q = q[bracketIdx:]
		}

		if segment == "" {
			continue
		}
		obj, ok := current.(map[string]any)
		if !ok {
			return "", fmt.Errorf("expected object at key %q", segment)
		}
		val, exists := obj[segment]
		if !exists {
			return "null", nil
		}
		current = val
	}

	switch v := current.(type) {
	case string:
		return v, nil
	default:
		b, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}
