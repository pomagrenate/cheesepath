package llm

import "strings"

// EstimateTokens returns an approximate token count for text.
// Uses a word-based heuristic: 1 word ≈ 1.3 tokens for English prose,
// plus a code bonus for punctuation characters that LLM tokenizers
// typically split as individual tokens ({, }, (, ), :, ;, [, ], <, >).
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	wordCount := len(strings.Fields(text))
	var codeBonus int
	for _, r := range text {
		switch r {
		case '{', '}', '(', ')', '[', ']', '<', '>', ':', ';', ',', '=':
			codeBonus++
		}
	}
	return int(float64(wordCount)*1.3) + codeBonus
}
