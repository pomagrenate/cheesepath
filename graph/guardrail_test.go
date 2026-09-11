package graph

import (
	"context"
	"errors"
	"testing"
)

type FinancialState struct {
	Balance int
	Action  string
}

func TestGraph_GuardrailViolationHalt(t *testing.T) {
	ctx := context.Background()

	g := NewStateGraph[FinancialState]()
	g.AddNode("withdraw", func(ctx context.Context, s FinancialState) (FinancialState, error) {
		// Attempt illegal negative balance
		return FinancialState{Balance: -50, Action: "withdraw"}, nil
	})

	// Add Guardrail: Balance must be >= 0
	g.AddGuardrail("withdraw", NewGuardrail[FinancialState](
		"non_negative_balance",
		func(ctx context.Context, state, update FinancialState) error {
			if update.Balance < 0 {
				return errors.New("insufficient funds: balance cannot be negative")
			}
			return nil
		},
		GuardrailActionHalt,
	))

	g.SetEntryPoint("withdraw")
	g.SetFinishPoint("withdraw")

	app, err := g.Compile()
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	_, err = app.Invoke(ctx, FinancialState{Balance: 100})
	if err == nil {
		t.Fatal("expected guardrail violation error, got nil")
	}

	var gErr *GuardrailViolationError[FinancialState]
	if !errors.As(err, &gErr) {
		t.Fatalf("expected *GuardrailViolationError, got: %T (%v)", err, err)
	}
	if gErr.GuardrailName != "non_negative_balance" {
		t.Fatalf("expected guardrail name 'non_negative_balance', got: %s", gErr.GuardrailName)
	}
}

func TestGraph_GuardrailFallback(t *testing.T) {
	ctx := context.Background()

	g := NewStateGraph[FinancialState]()
	g.AddNode("withdraw", func(ctx context.Context, s FinancialState) (FinancialState, error) {
		return FinancialState{Balance: -100, Action: "withdraw"}, nil
	})
	g.AddNode("safe_reject", func(ctx context.Context, s FinancialState) (FinancialState, error) {
		return FinancialState{Balance: s.Balance, Action: "rejected_by_guardrail"}, nil
	})

	g.AddGuardrail("withdraw", NewGuardrail[FinancialState](
		"non_negative_balance",
		func(ctx context.Context, state, update FinancialState) error {
			if update.Balance < 0 {
				return errors.New("balance cannot be negative")
			}
			return nil
		},
		GuardrailActionFallback,
		"safe_reject",
	))

	g.SetEntryPoint("withdraw")
	g.SetFinishPoint("withdraw")
	g.SetFinishPoint("safe_reject")

	app, err := g.Compile()
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	finalState, err := app.Invoke(ctx, FinancialState{Balance: 100})
	if err != nil {
		t.Fatalf("expected successful fallback, got: %v", err)
	}

	if finalState.Action != "rejected_by_guardrail" || finalState.Balance != 100 {
		t.Fatalf("unexpected state after guardrail fallback: %+v", finalState)
	}
}
