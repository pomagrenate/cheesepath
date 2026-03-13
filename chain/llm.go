// Package chain provides composable LLM pipeline chains built on Runnable.
package chain

import (
	"context"
	"fmt"

	"github.com/AutoCookies/crabpath/llm"
	"github.com/AutoCookies/crabpath/prompt"
)

// ChainInput is the input to an LLMChain: a variable map plus optional history.
type ChainInput struct {
	Vars    map[string]any
	History []llm.Message
}

// LLMChain runs: ChatTemplate → LLM → string.
// Invoke returns the final text response.
type LLMChain struct {
	client   *llm.Client
	template *prompt.ChatTemplate
	model    string
	grammar  string
}

// LLMChainOption configures an LLMChain.
type LLMChainOption func(*LLMChain)

func WithModel(model string) LLMChainOption        { return func(c *LLMChain) { c.model = model } }
func WithGrammar(grammar string) LLMChainOption    { return func(c *LLMChain) { c.grammar = grammar } }

// NewLLMChain creates a chain: template → LLM(model) → raw string.
func NewLLMChain(client *llm.Client, tmpl *prompt.ChatTemplate, opts ...LLMChainOption) *LLMChain {
	c := &LLMChain{client: client, template: tmpl}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Invoke renders the template, calls the LLM, and returns the response text.
func (c *LLMChain) Invoke(ctx context.Context, input ChainInput) (string, error) {
	messages, err := c.template.Format(input.Vars)
	if err != nil {
		return "", fmt.Errorf("chain/llm: format: %w", err)
	}
	messages = append(input.History, messages...)

	resp, err := c.client.Complete(ctx, llm.Request{
		Model:    c.model,
		Messages: messages,
		Grammar:  c.grammar,
	})
	if err != nil {
		return "", fmt.Errorf("chain/llm: complete: %w", err)
	}
	return resp, nil
}

// Stream renders the template and streams tokens from the LLM.
func (c *LLMChain) Stream(ctx context.Context, input ChainInput) (<-chan string, error) {
	messages, err := c.template.Format(input.Vars)
	if err != nil {
		return nil, fmt.Errorf("chain/llm: format: %w", err)
	}
	messages = append(input.History, messages...)

	tCh, _, eCh := c.client.CompleteStream(ctx, llm.Request{
		Model:    c.model,
		Messages: messages,
		Grammar:  c.grammar,
	})

	out := make(chan string, 256)
	go func() {
		defer close(out)
		for {
			select {
			case t, ok := <-tCh:
				if !ok {
					return
				}
				out <- t
			case err, ok := <-eCh:
				if ok && err != nil {
					return
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
