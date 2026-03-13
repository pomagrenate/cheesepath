package prompt

import (
	"fmt"

	"github.com/AutoCookies/crabpath/llm"
)

// MessageTemplate is one turn of a ChatTemplate: a role + a PromptTemplate.
type MessageTemplate struct {
	Role     string
	Template *PromptTemplate
}

// ChatTemplate builds a []llm.Message from role/template pairs.
type ChatTemplate struct {
	messages []MessageTemplate
}

// NewChatTemplate constructs a ChatTemplate from the provided MessageTemplates.
func NewChatTemplate(msgs ...MessageTemplate) *ChatTemplate {
	return &ChatTemplate{messages: msgs}
}

// System returns a MessageTemplate for the system role.
func System(tmpl string) MessageTemplate {
	return MessageTemplate{Role: "system", Template: NewTemplate(tmpl)}
}

// Human returns a MessageTemplate for the user role.
func Human(tmpl string) MessageTemplate {
	return MessageTemplate{Role: "user", Template: NewTemplate(tmpl)}
}

// AI returns a MessageTemplate for the assistant role.
func AI(tmpl string) MessageTemplate {
	return MessageTemplate{Role: "assistant", Template: NewTemplate(tmpl)}
}

// Format renders all templates with vars, returning []llm.Message.
func (c *ChatTemplate) Format(vars map[string]any) ([]llm.Message, error) {
	out := make([]llm.Message, 0, len(c.messages))
	for i, mt := range c.messages {
		content, err := mt.Template.Format(vars)
		if err != nil {
			return nil, fmt.Errorf("prompt: ChatTemplate message %d: %w", i, err)
		}
		out = append(out, llm.Message{Role: mt.Role, Content: content})
	}
	return out, nil
}
