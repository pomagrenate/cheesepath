package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/AutoCookies/cheesepath/core"
	"github.com/AutoCookies/cheesepath/providers"
)

func TestSelfCorrectingChatModel_SuccessAfterReflection(t *testing.T) {
	ctx := context.Background()

	// Attempt 1: Invalid non-JSON output
	// Attempt 2: Valid JSON output
	mockModel := providers.NewMockChatModel(
		core.NewAIMessage("Sure! Here is the data: item=apple, price=1.5"),
		core.NewAIMessage(`{"item": "apple", "price": 1.5}`),
	)

	selfCorrectingModel := NewSelfCorrectingChatModel(
		mockModel,
		RequireValidJSON(),
		3,
	)

	res, err := selfCorrectingModel.Generate(ctx, []core.Message{
		core.NewHumanMessage("Extract item and price as JSON"),
	})
	if err != nil {
		t.Fatalf("expected self-correction to succeed, got: %v", err)
	}

	if !strings.Contains(res.Content, `"item": "apple"`) {
		t.Fatalf("expected valid JSON, got: %s", res.Content)
	}

	// Verify history contains reflection prompt
	history := mockModel.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 calls to underlying model, got %d", len(history))
	}
	secondCallPrompt := history[1][len(history[1])-1].Content
	if !strings.Contains(secondCallPrompt, "failed validation") {
		t.Fatalf("expected reflection prompt in second call, got: %s", secondCallPrompt)
	}
}

func TestSelfCorrectingChatModel_ExhaustedAttempts(t *testing.T) {
	ctx := context.Background()

	// Both attempts fail the forbidden keyword check
	mockModel := providers.NewMockChatModel(
		core.NewAIMessage("This contains confidential info"),
		core.NewAIMessage("Still contains confidential info"),
	)

	selfCorrectingModel := NewSelfCorrectingChatModel(
		mockModel,
		ForbidKeywords("confidential"),
		2,
	)

	_, err := selfCorrectingModel.Generate(ctx, []core.Message{
		core.NewHumanMessage("Tell me a secret"),
	})
	if err == nil {
		t.Fatal("expected error after exhausted attempts, got nil")
	}

	if !strings.Contains(err.Error(), "self-correction exhausted") {
		t.Fatalf("expected exhausted error message, got: %v", err)
	}
}
