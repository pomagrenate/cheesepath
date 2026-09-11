package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AutoCookies/cheesepath/core"
)

// MessageValidator checks whether a model output adheres to required constraints.
type MessageValidator func(msg core.Message) error

// RequireValidJSON validates that the message content is valid parseable JSON.
func RequireValidJSON() MessageValidator {
	return func(msg core.Message) error {
		var js any
		if err := json.Unmarshal([]byte(msg.Content), &js); err != nil {
			return fmt.Errorf("response is not valid JSON: %w", err)
		}
		return nil
	}
}

// ForbidKeywords validates that the message content does not contain forbidden keywords.
func ForbidKeywords(keywords ...string) MessageValidator {
	return func(msg core.Message) error {
		lower := strings.ToLower(msg.Content)
		for _, kw := range keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return fmt.Errorf("response contains forbidden keyword %q", kw)
			}
		}
		return nil
	}
}

// RequireKeywords validates that the message content contains all required keywords.
func RequireKeywords(keywords ...string) MessageValidator {
	return func(msg core.Message) error {
		lower := strings.ToLower(msg.Content)
		for _, kw := range keywords {
			if !strings.Contains(lower, strings.ToLower(kw)) {
				return fmt.Errorf("response is missing required keyword %q", kw)
			}
		}
		return nil
	}
}

// SelfCorrectingChatModel wraps a core.ChatModel with automated reflection and self-correction.
type SelfCorrectingChatModel struct {
	base        core.ChatModel
	validator   MessageValidator
	maxAttempts int
}

// NewSelfCorrectingChatModel creates a ChatModel with built-in reflection loops.
func NewSelfCorrectingChatModel(
	base core.ChatModel,
	validator MessageValidator,
	maxAttempts int,
) *SelfCorrectingChatModel {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return &SelfCorrectingChatModel{
		base:        base,
		validator:   validator,
		maxAttempts: maxAttempts,
	}
}

func (m *SelfCorrectingChatModel) BindTools(tools ...core.Tool) core.ChatModel {
	return &SelfCorrectingChatModel{
		base:        m.base.BindTools(tools...),
		validator:   m.validator,
		maxAttempts: m.maxAttempts,
	}
}

func (m *SelfCorrectingChatModel) Generate(ctx context.Context, messages []core.Message, opts ...core.ModelOption) (*core.Message, error) {
	currentMessages := make([]core.Message, len(messages))
	copy(currentMessages, messages)

	var lastErr error

	for attempt := 1; attempt <= m.maxAttempts; attempt++ {
		resp, err := m.base.Generate(ctx, currentMessages, opts...)
		if err != nil {
			return nil, err
		}

		if m.validator == nil {
			return resp, nil
		}

		valErr := m.validator(*resp)
		if valErr == nil {
			return resp, nil
		}

		lastErr = valErr
		if attempt == m.maxAttempts {
			break
		}

		// Inject self-correction prompt and retry
		currentMessages = append(currentMessages, *resp)
		currentMessages = append(currentMessages, core.NewHumanMessage(
			fmt.Sprintf("Your previous response failed validation: %v. Please rectify and provide a corrected response.", valErr),
		))
	}

	return nil, fmt.Errorf("self-correction exhausted after %d attempts: %w", m.maxAttempts, lastErr)
}

func (m *SelfCorrectingChatModel) Stream(ctx context.Context, messages []core.Message, opts ...core.ModelOption) (<-chan core.StreamChunk, error) {
	// For streaming, generate and stream the final validated output
	resp, err := m.Generate(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}

	ch := make(chan core.StreamChunk, 2)
	ch <- core.StreamChunk{
		ContentDelta: resp.Content,
		Done:         true,
	}
	close(ch)
	return ch, nil
}
