package memory

import (
	"context"
	"fmt"

	"github.com/AutoCookies/crabpath/llm"
)

// SummaryMemory wraps a base Memory and compresses it via an LLM when the
// conversation exceeds a token threshold. After compression the history is
// replaced by a single assistant message containing the summary.
type SummaryMemory struct {
	base         Memory
	client       *llm.Client
	model        string
	tokenLimit   int
	summaryPrompt string
}

const defaultSummaryPrompt = `You are a concise summarisation assistant. Given the following conversation history, produce a single compact summary paragraph that preserves all important facts, decisions, and code produced. Do not include filler.`

// NewSummaryMemory creates a SummaryMemory wrapping base.
// tokenLimit is approximate: when total chars > tokenLimit*4 a compression is triggered.
func NewSummaryMemory(base Memory, client *llm.Client, model string, tokenLimit int) *SummaryMemory {
	return &SummaryMemory{
		base:          base,
		client:        client,
		model:         model,
		tokenLimit:    tokenLimit,
		summaryPrompt: defaultSummaryPrompt,
	}
}

func (s *SummaryMemory) Add(msg llm.Message) error      { return s.base.Add(msg) }
func (s *SummaryMemory) Messages() []llm.Message        { return s.base.Messages() }
func (s *SummaryMemory) Clear() error                   { return s.base.Clear() }

// Compress summarises history if it exceeds the token limit.
func (s *SummaryMemory) Compress(ctx context.Context) error {
	msgs := s.base.Messages()
	totalChars := 0
	for _, m := range msgs {
		totalChars += len(m.Content)
	}
	if totalChars <= s.tokenLimit*4 {
		return nil
	}

	// Build a "conversation" string for the LLM to summarise.
	var histText string
	for _, m := range msgs {
		histText += fmt.Sprintf("%s: %s\n\n", m.Role, m.Content)
	}

	summary, err := s.client.Complete(ctx, llm.Request{
		Model: s.model,
		Messages: []llm.Message{
			{Role: "system", Content: s.summaryPrompt},
			{Role: "user", Content: "Conversation to summarise:\n\n" + histText},
		},
	})
	if err != nil {
		return fmt.Errorf("memory/summary: compress: %w", err)
	}

	if err := s.base.Clear(); err != nil {
		return err
	}
	return s.base.Add(llm.Message{
		Role:    "assistant",
		Content: "[Summary of prior conversation]: " + summary,
	})
}
