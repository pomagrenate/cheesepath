package llm

// EstimateTokens returns an approximate token count for text.
// Uses a word-based heuristic: 1 word ≈ 1.3 tokens for English prose,
// plus a code bonus for punctuation characters that LLM tokenizers
// typically split as individual tokens ({, }, (, ), :, ;, [, ], <, >).
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Single pass: count words and code-punctuation simultaneously.
	words, codeBonus := 0, 0
	inWord := false
	for _, r := range text {
		switch r {
		case '{', '}', '(', ')', '[', ']', '<', '>', ':', ';', ',', '=':
			codeBonus++
			inWord = false
		case ' ', '\t', '\n', '\r':
			inWord = false
		default:
			if !inWord {
				words++
				inWord = true
			}
		}
	}
	return int(float64(words)*1.3) + codeBonus
}
