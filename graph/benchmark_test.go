package graph

import (
	"context"
	"testing"
)

func BenchmarkGraphSupersteps(b *testing.B) {
	ctx := context.Background()
	g := NewStateGraph[CounterState](counterReducer)

	g.AddNode("step1", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 1}, nil
	})
	g.AddNode("step2", func(ctx context.Context, s CounterState) (CounterState, error) {
		return CounterState{Count: 1}, nil
	})
	g.SetEntryPoint("step1")
	g.AddEdge("step1", "step2")
	g.SetFinishPoint("step2")

	compiled, err := g.Compile()
	if err != nil {
		b.Fatalf("compile failed: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = compiled.Invoke(ctx, CounterState{})
	}
}
