package providers

import (
	"context"
	"fmt"
	"sync"

	"github.com/AutoCookies/cheesepath/core"
)

// MockChatModel is an in-memory mock implementation of core.ChatModel.
type MockChatModel struct {
	mu              sync.Mutex
	responses       []core.Message
	history         [][]core.Message
	customResponder func(messages []core.Message) (*core.Message, error)
	boundTools      []core.Tool
}

// NewMockChatModel creates a MockChatModel with pre-loaded responses.
func NewMockChatModel(responses ...core.Message) *MockChatModel {
	return &MockChatModel{
		responses: responses,
	}
}

// SetResponder sets a dynamic responder function to compute outputs dynamically.
func (m *MockChatModel) SetResponder(fn func(messages []core.Message) (*core.Message, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.customResponder = fn
}

// AddResponse enqueues an additional canned response.
func (m *MockChatModel) AddResponse(resp core.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = append(m.responses, resp)
}

// History returns a copy of all message batches passed to Generate or Stream.
func (m *MockChatModel) History() [][]core.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]core.Message, len(m.history))
	for i, h := range m.history {
		copied := make([]core.Message, len(h))
		copy(copied, h)
		out[i] = copied
	}
	return out
}

func (m *MockChatModel) BindTools(tools ...core.Tool) core.ChatModel {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := &MockChatModel{
		responses:       m.responses,
		history:         m.history,
		customResponder: m.customResponder,
		boundTools:      append(m.boundTools, tools...),
	}
	return clone
}

func (m *MockChatModel) Generate(_ context.Context, messages []core.Message, _ ...core.ModelOption) (*core.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := make([]core.Message, len(messages))
	copy(record, messages)
	m.history = append(m.history, record)

	if m.customResponder != nil {
		return m.customResponder(messages)
	}

	if len(m.responses) == 0 {
		return nil, fmt.Errorf("mock: no canned responses remaining")
	}

	next := m.responses[0]
	m.responses = m.responses[1:]
	return &next, nil
}

func (m *MockChatModel) Stream(ctx context.Context, messages []core.Message, opts ...core.ModelOption) (<-chan core.StreamChunk, error) {
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
