package core

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestIdempotentToolRegistry_Deduplication(t *testing.T) {
	ctx := context.Background()
	var executionCount int32

	// Non-idempotent tool: e.g. bank charge or database insert
	paymentTool := NewFuncTool(
		"charge_card",
		"Charges a credit card",
		map[string]any{"type": "object"},
		func(ctx context.Context, args map[string]any) (string, error) {
			atomic.AddInt32(&executionCount, 1)
			return "charge_success_tx_123", nil
		},
	)

	baseRegistry := NewToolRegistry()
	baseRegistry.Register(paymentTool)

	store := NewMemoryIdempotencyStore()
	idempotentReg := NewIdempotentToolRegistry(baseRegistry, store)

	threadID := "checkout-session-999"
	args := map[string]any{"amount": 500, "currency": "USD"}

	// First execution: should call tool
	res1, wasCached1, err := idempotentReg.ExecuteIdempotent(ctx, threadID, "charge_card", args)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if wasCached1 {
		t.Fatal("expected first call wasCached=false")
	}
	if res1 != "charge_success_tx_123" {
		t.Fatalf("unexpected result: %s", res1)
	}
	if executionCount != 1 {
		t.Fatalf("expected executionCount=1, got %d", executionCount)
	}

	// Second execution (replay after crash or HITL resume with identical args):
	// MUST return cached result and NOT execute tool a second time!
	res2, wasCached2, err := idempotentReg.ExecuteIdempotent(ctx, threadID, "charge_card", args)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if !wasCached2 {
		t.Fatal("expected second call wasCached=true")
	}
	if res2 != "charge_success_tx_123" {
		t.Fatalf("unexpected cached result: %s", res2)
	}
	if executionCount != 1 {
		t.Fatalf("CRITICAL IDEMPOTENCY VIOLATION: tool was executed %d times instead of 1!", executionCount)
	}

	// Different arguments: should execute
	args2 := map[string]any{"amount": 600, "currency": "USD"}
	_, wasCached3, err := idempotentReg.ExecuteIdempotent(ctx, threadID, "charge_card", args2)
	if err != nil || wasCached3 {
		t.Fatalf("expected fresh execution for different args")
	}
	if executionCount != 2 {
		t.Fatalf("expected executionCount=2 for different args, got %d", executionCount)
	}
}
