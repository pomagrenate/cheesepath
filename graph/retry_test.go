package graph

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecuteWithRetry_SuccessAfterRetries(t *testing.T) {
	ctx := context.Background()
	var attempts int32

	fn := func(ctx context.Context, state int) (int, error) {
		att := atomic.AddInt32(&attempts, 1)
		if att < 3 {
			return 0, errors.New("transient error")
		}
		return state + 10, nil
	}

	policy := &RetryPolicy{
		MaxRetries:      3,
		InitialInterval: 5 * time.Millisecond,
		Multiplier:      1.5,
	}

	res, err := ExecuteWithRetry(ctx, policy, fn, 5)
	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}
	if res != 15 {
		t.Fatalf("expected 15, got %d", res)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestExecuteWithRetry_PredicateFilter(t *testing.T) {
	ctx := context.Background()
	var attempts int32

	fatalErr := errors.New("fatal non-retryable error")
	fn := func(ctx context.Context, state int) (int, error) {
		atomic.AddInt32(&attempts, 1)
		return 0, fatalErr
	}

	policy := &RetryPolicy{
		MaxRetries:      5,
		InitialInterval: 5 * time.Millisecond,
		RetryIf: func(err error) bool {
			return !errors.Is(err, fatalErr)
		},
	}

	_, err := ExecuteWithRetry(ctx, policy, fn, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 {
		t.Fatalf("expected non-retryable error to halt after 1 attempt, got %d", attempts)
	}
}

func TestGraph_NodeRetryWithFallback(t *testing.T) {
	ctx := context.Background()

	g := NewStateGraph[int]()

	// Node 1: Always fails
	g.AddNodeWithRetry("primary", func(ctx context.Context, s int) (int, error) {
		return 0, errors.New("primary service unavailable")
	}, &RetryPolicy{
		MaxRetries:      2,
		InitialInterval: 2 * time.Millisecond,
	}, "fallback")

	// Fallback Node: Returns safe value
	g.AddNode("fallback", func(ctx context.Context, s int) (int, error) {
		return 999, nil
	})

	g.SetEntryPoint("primary")
	g.SetFinishPoint("primary")
	g.SetFinishPoint("fallback")

	app, err := g.Compile()
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	res, err := app.Invoke(ctx, 1)
	if err != nil {
		t.Fatalf("expected fallback to succeed, got: %v", err)
	}
	if res != 999 {
		t.Fatalf("expected 999 from fallback node, got %d", res)
	}
}
