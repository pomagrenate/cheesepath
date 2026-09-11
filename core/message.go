// Package core provides fundamental primitives for Cheesepath, including
// standardized messages, tools, chat models, and runnables.
package core

import (
	"encoding/json"
	"fmt"
)

// Role defines the originator of a Message.
type Role string

const (
	RoleSystem Role = "system"
	RoleHuman  Role = "user"
	RoleAI     Role = "assistant"
	RoleTool   Role = "tool"
)

// ToolCall represents a model's request to execute a specific tool with arguments.
type ToolCall struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Arguments    map[string]any `json:"arguments"`
	RawArguments string         `json:"raw_arguments,omitempty"`
}

// Message represents a single chat turn in Cheesepath.
type Message struct {
	Role       Role           `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// NewSystemMessage creates a system instruction message.
func NewSystemMessage(content string) Message {
	return Message{
		Role:    RoleSystem,
		Content: content,
	}
}

// NewHumanMessage creates a user message.
func NewHumanMessage(content string) Message {
	return Message{
		Role:    RoleHuman,
		Content: content,
	}
}

// NewAIMessage creates a standard assistant response message.
func NewAIMessage(content string) Message {
	return Message{
		Role:    RoleAI,
		Content: content,
	}
}

// NewAIMessageWithToolCalls creates an assistant message requesting tool invocations.
func NewAIMessageWithToolCalls(content string, toolCalls []ToolCall) Message {
	return Message{
		Role:      RoleAI,
		Content:   content,
		ToolCalls: toolCalls,
	}
}

// NewToolMessage creates a response message carrying the output of a tool execution.
func NewToolMessage(toolCallID, content string) Message {
	return Message{
		Role:       RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
	}
}

func (m Message) String() string {
	if len(m.ToolCalls) > 0 {
		data, _ := json.Marshal(m.ToolCalls)
		if m.Content != "" {
			return fmt.Sprintf("[%s]: %s (tool_calls: %s)", m.Role, m.Content, string(data))
		}
		return fmt.Sprintf("[%s tool_calls]: %s", m.Role, string(data))
	}
	if m.Role == RoleTool {
		return fmt.Sprintf("[tool:%s]: %s", m.ToolCallID, m.Content)
	}
	return fmt.Sprintf("[%s]: %s", m.Role, m.Content)
}
