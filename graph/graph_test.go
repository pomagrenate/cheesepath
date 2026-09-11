package graph

import (
	"context"
	"sync"
	"testing"
)

type CounterState struct {
	Count   int
	History []string
}

func counterReducer(cur CounterState, up CounterState) CounterState {
	return CounterState{
		Count:   cur.Count + up.Count,
		History: append(cur.History, up.History...),
	}
}

func TestLinearPipeline(t *testing.T) {
	ctx := context.Background()
	g := NewStateGraph[CounterState](counterReducer)

	g.AddNode("step1", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 1, History: []string{"step1"}}, nil
	})
	g.AddNode("step2", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 2, History: []string{"step2"}}, nil
	})

	g.SetEntryPoint("step1")
	g.AddEdge("step1", "step2")
	g.SetFinishPoint("step2")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	final, err := compiled.Invoke(ctx, CounterState{})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}

	if final.Count != 3 {
		t.Fatalf("expected count 3, got %d", final.Count)
	}
	if len(final.History) != 2 || final.History[0] != "step1" || final.History[1] != "step2" {
		t.Fatalf("unexpected history: %+v", final.History)
	}
}

func TestCyclicGraph(t *testing.T) {
	ctx := context.Background()
	g := NewStateGraph[CounterState](counterReducer)

	g.AddNode("loop", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 1, History: []string{"loop"}}, nil
	})

	g.SetEntryPoint("loop")
	g.AddConditionalEdges("loop", func(ctx context.Context, s CounterState) (string, error) {
		if s.Count >= 5 {
			return END, nil
		}
		return "loop", nil
	}, nil)

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	final, err := compiled.Invoke(ctx, CounterState{})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}

	if final.Count != 5 {
		t.Fatalf("expected count 5, got %d", final.Count)
	}
	if len(final.History) != 5 {
		t.Fatalf("expected 5 loop iterations, got %d", len(final.History))
	}
}

func TestParallelBranching(t *testing.T) {
	ctx := context.Background()
	g := NewStateGraph[CounterState](counterReducer)

	var mu sync.Mutex
	var concurrentRuns int
	var maxConcurrent int

	g.AddNode("start", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 1, History: []string{"start"}}, nil
	})
	g.AddNode("branchA", func(ctx context.Context, s CounterState) (CounterState, error) {
		mu.Lock()
		concurrentRuns++
		if concurrentRuns > maxConcurrent {
			maxConcurrent = concurrentRuns
		}
		mu.Unlock()

		mu.Lock()
		concurrentRuns--
		mu.Unlock()

		return CounterState{Count: 10, History: []string{"branchA"}}, nil
	})
	g.AddNode("branchB", func(ctx context.Context, s CounterState) (CounterState, error) {
		mu.Lock()
		concurrentRuns++
		if concurrentRuns > maxConcurrent {
			maxConcurrent = concurrentRuns
		}
		mu.Unlock()

		mu.Lock()
		concurrentRuns--
		mu.Unlock()

		return CounterState{Count: 20, History: []string{"branchB"}}, nil
	})
	g.AddNode("join", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 100, History: []string{"join"}}, nil
	})

	g.SetEntryPoint("start")
	g.AddEdge("start", "branchA")
	g.AddEdge("start", "branchB")
	g.AddEdge("branchA", "join")
	g.AddEdge("branchB", "join")
	g.SetFinishPoint("join")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	final, err := compiled.Invoke(ctx, CounterState{})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}

	// Total count: start(1) + branchA(10) + branchB(20) + join(100) = 131
	if final.Count != 131 {
		t.Fatalf("expected count 131, got %d", final.Count)
	}
}

func TestRecursionLimitExceeded(t *testing.T) {
	ctx := context.Background()
	g := NewStateGraph[CounterState](counterReducer)

	g.AddNode("infinite", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 1}, nil
	})
	g.SetEntryPoint("infinite")
	g.AddEdge("infinite", "infinite")

	compiled, err := g.Compile(WithMaxRecursionLimit[CounterState](5))
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	_, err = compiled.Invoke(ctx, CounterState{})
	if err == nil {
		t.Fatal("expected recursion limit error, got nil")
	}
}
