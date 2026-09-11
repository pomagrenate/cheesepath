package core

import (
	"context"
)

// ModelConfig holds runtime options for LLM generation.
type ModelConfig struct {
	Model       string
	Temperature float32
	TopP        float32
	MaxTokens   int
	Stop        []string
	Tools       []Tool
}

// ModelOption allows customizing generation parameters.
type ModelOption func(*ModelConfig)

func WithModelName(name string) ModelOption {
	return func(c *ModelConfig) { c.Model = name }
}

func WithTemperature(t float32) ModelOption {
	return func(c *ModelConfig) { c.Temperature = t }
}

func WithMaxTokens(n int) ModelOption {
	return func(c *ModelConfig) { c.MaxTokens = n }
}

func WithStop(stop ...string) ModelOption {
	return func(c *ModelConfig) { c.Stop = append(c.Stop, stop...) }
}

func WithTools(tools ...Tool) ModelOption {
	return func(c *ModelConfig) { c.Tools = append(c.Tools, tools...) }
}

// ToolCallDelta represents an incremental piece of a tool call in a streaming response.
type ToolCallDelta struct {
	Index          int
	ID             string
	Name           string
	ArgumentsDelta string
}

// StreamChunk is one token/chunk emitted during streaming.
type StreamChunk struct {
	ContentDelta   string
	ToolCallDeltas []ToolCallDelta
	Done           bool
	Error          error
}

// ChatModel is the core interface for language models in Cheesepath.
type ChatModel interface {
	// Generate produces a complete non-streaming chat response.
	Generate(ctx context.Context, messages []Message, opts ...ModelOption) (*Message, error)
	// Stream produces a streaming response of chunks.
	Stream(ctx context.Context, messages []Message, opts ...ModelOption) (<-chan StreamChunk, error)
	// BindTools returns a clone of the model configured to call the given tools.
	BindTools(tools ...Tool) ChatModel
}
