package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// WebFetchTool fetches a URL and returns the page content as plain text,
// stripping HTML tags with a pure-stdlib rune-by-rune state machine.
type WebFetchTool struct {
	client *http.Client
}

func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (t *WebFetchTool) Name() string    { return "web_fetch" }
func (t *WebFetchTool) Dangerous() bool { return false }
func (t *WebFetchTool) Description() string {
	return "Fetch a URL and return its content as plain text (HTML tags stripped). Useful for reading web pages, documentation, or API responses."
}
func (t *WebFetchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":       map[string]any{"type": "string", "description": "The URL to fetch"},
			"max_chars": map[string]any{"type": "number", "description": "Maximum characters to return (default: 6000)"},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return "", fmt.Errorf("web_fetch: url is required")
	}

	maxChars := 6000
	if v, ok := args["max_chars"].(float64); ok && v > 0 {
		maxChars = int(v)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("web_fetch: invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "CrabAgent/1.0 (cheeserag web_fetch)")
	req.Header.Set("Accept", "text/html,text/plain,application/json;q=0.9,*/*;q=0.8")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_fetch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("web_fetch: HTTP %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxChars*10)))
	if err != nil {
		return "", fmt.Errorf("web_fetch: read error: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	var text string
	if strings.Contains(contentType, "json") {
		text = string(body)
	} else {
		text = stripHTML(string(body))
	}

	text = collapseWhitespace(text)
	if len(text) > maxChars {
		text = text[:maxChars] + fmt.Sprintf("\n... (truncated, fetched from %s)", url)
	}
	return text, nil
}

// stripHTML removes HTML/XML tags using a rune-by-rune state machine.
// No regex, no external packages.
func stripHTML(html string) string {
	var sb strings.Builder
	inTag := false
	inScript := false
	inStyle := false
	buf := make([]rune, 0, 32) // tag name buffer

	runes := []rune(html)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == '<':
			inTag = true
			buf = buf[:0]
			i++
		case r == '>' && inTag:
			inTag = false
			tagName := strings.ToLower(strings.TrimLeft(string(buf), "/ \t\n"))
			// strip tag attributes — just keep the element name
			if idx := strings.IndexAny(tagName, " \t\n/"); idx != -1 {
				tagName = tagName[:idx]
			}
			switch tagName {
			case "script":
				inScript = true
			case "/script":
				inScript = false
			case "style":
				inStyle = true
			case "/style":
				inStyle = false
			}
			if !inScript && !inStyle {
				// Add whitespace around block-level elements
				switch tagName {
				case "br", "/p", "/div", "/li", "/h1", "/h2", "/h3", "/h4", "/h5", "/h6", "/tr":
					sb.WriteRune('\n')
				}
			}
			i++
		case inTag:
			buf = append(buf, r)
			i++
		case inScript || inStyle:
			i++
		case r == '&':
			// Decode common HTML entities
			entity, skip := decodeEntity(runes[i:])
			sb.WriteString(entity)
			i += skip
		default:
			if !unicode.IsControl(r) || r == '\n' || r == '\t' {
				sb.WriteRune(r)
			}
			i++
		}
	}
	return sb.String()
}

// decodeEntity decodes a limited set of HTML entities, returns (decoded, consumed_runes).
func decodeEntity(runes []rune) (string, int) {
	if len(runes) < 3 || runes[0] != '&' {
		return "&", 1
	}
	// Find semicolon
	end := -1
	for j := 1; j < len(runes) && j < 10; j++ {
		if runes[j] == ';' {
			end = j
			break
		}
	}
	if end == -1 {
		return "&", 1
	}
	entity := string(runes[1:end])
	switch entity {
	case "amp":
		return "&", end + 1
	case "lt":
		return "<", end + 1
	case "gt":
		return ">", end + 1
	case "quot":
		return `"`, end + 1
	case "apos":
		return "'", end + 1
	case "nbsp":
		return " ", end + 1
	case "mdash":
		return "—", end + 1
	case "ndash":
		return "–", end + 1
	}
	return "&" + entity + ";", end + 1
}

// collapseWhitespace merges runs of whitespace into single spaces/newlines.
func collapseWhitespace(s string) string {
	var sb strings.Builder
	prevNewline := false
	prevSpace := false
	newlineCount := 0

	for _, r := range s {
		switch {
		case r == '\n':
			newlineCount++
			prevSpace = false
			if newlineCount <= 2 {
				sb.WriteRune('\n')
			}
			prevNewline = true
		case r == '\r':
			// skip
		case r == ' ' || r == '\t':
			if !prevSpace && !prevNewline {
				sb.WriteRune(' ')
			}
			prevSpace = true
		default:
			newlineCount = 0
			prevSpace = false
			prevNewline = false
			sb.WriteRune(r)
		}
	}
	return strings.TrimSpace(sb.String())
}
