package benchmark

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/AutoCookies/cheesepath/graph"
)

// NestedState represents state across parent and child subgraphs.
type NestedState struct {
	TaskType  string   `json:"task_type"`
	Depth     int      `json:"depth"`
	Trail     []string `json:"trail"`
	Completed bool     `json:"completed"`
}

func nestedStateReducer(cur, up NestedState) NestedState {
	taskType := cur.TaskType
	if up.TaskType != "" {
		taskType = up.TaskType
	}
	completed := cur.Completed || up.Completed
	return NestedState{
		TaskType:  taskType,
		Depth:     cur.Depth + up.Depth,
		Trail:     append(cur.Trail, up.Trail...),
		Completed: completed,
	}
}

// BuildSubGraph creates a 2-node child subgraph for a specific task.
func BuildSubGraph(subName string) (*graph.CompiledGraph[NestedState], error) {
	g := graph.NewStateGraph[NestedState](nestedStateReducer)

	g.AddNode("sub_step1", func(ctx context.Context, s NestedState) (NestedState, error) {
		return NestedState{
			Depth: 1,
			Trail: []string{subName + "_step1"},
		}, nil
	})

	g.AddNode("sub_step2", func(ctx context.Context, s NestedState) (NestedState, error) {
		return NestedState{
			Depth:     1,
			Trail:     []string{subName + "_step2"},
			Completed: true,
		}, nil
	})

	g.SetEntryPoint("sub_step1")
	g.AddEdge("sub_step1", "sub_step2")
	g.SetFinishPoint("sub_step2")

	return g.Compile()
}

// BuildParentGraph creates a parent graph routing to 3 nested subgraphs.
func BuildParentGraph() (*graph.CompiledGraph[NestedState], error) {
	subA, err := BuildSubGraph("subgraph_A")
	if err != nil {
		return nil, err
	}
	subB, err := BuildSubGraph("subgraph_B")
	if err != nil {
		return nil, err
	}
	subC, err := BuildSubGraph("subgraph_C")
	if err != nil {
		return nil, err
	}

	g := graph.NewStateGraph[NestedState](nestedStateReducer)

	g.AddNode("orchestrator", func(ctx context.Context, s NestedState) (NestedState, error) {
		return NestedState{Trail: []string{"orchestrator"}}, nil
	})

	g.AddNode("run_sub_a", func(ctx context.Context, s NestedState) (NestedState, error) {
		subRes, err := subA.Invoke(ctx, s)
		if err != nil {
			return NestedState{}, err
		}
		return subRes, nil
	})

	g.AddNode("run_sub_b", func(ctx context.Context, s NestedState) (NestedState, error) {
		subRes, err := subB.Invoke(ctx, s)
		if err != nil {
			return NestedState{}, err
		}
		return subRes, nil
	})

	g.AddNode("run_sub_c", func(ctx context.Context, s NestedState) (NestedState, error) {
		subRes, err := subC.Invoke(ctx, s)
		if err != nil {
			return NestedState{}, err
		}
		return subRes, nil
	})

	g.AddNode("finalize", func(ctx context.Context, s NestedState) (NestedState, error) {
		return NestedState{Trail: []string{"finalized"}}, nil
	})

	g.SetEntryPoint("orchestrator")

	// Dynamic conditional edge routing to the right subgraph
	g.AddConditionalEdges("orchestrator", func(ctx context.Context, s NestedState) (string, error) {
		switch s.TaskType {
		case "task_a":
			return "run_sub_a", nil
		case "task_b":
			return "run_sub_b", nil
		default:
			return "run_sub_c", nil
		}
	}, nil)

	g.AddEdge("run_sub_a", "finalize")
	g.AddEdge("run_sub_b", "finalize")
	g.AddEdge("run_sub_c", "finalize")
	g.SetFinishPoint("finalize")

	return g.Compile()
}

// RunScenarioD executes Scenario D for iterations runs after warmup.
func RunScenarioD(iterations int) (Result, error) {
	app, err := BuildParentGraph()
	if err != nil {
		return Result{}, err
	}

	ctx := context.Background()

	// Warmup
	tasks := []string{"task_a", "task_b", "task_c"}
	for i := 0; i < 20; i++ {
		_, _ = app.Invoke(ctx, NestedState{TaskType: tasks[i%3]})
	}

	latencies := make([]time.Duration, iterations)
	runtime.GC()

	var mStart, mEnd runtime.MemStats
	runtime.ReadMemStats(&mStart)

	for i := 0; i < iterations; i++ {
		task := tasks[i%3]
		start := time.Now()
		res, err := app.Invoke(ctx, NestedState{TaskType: task})
		dur := time.Since(start)
		if err != nil {
			return Result{}, fmt.Errorf("scenario D failed at %d: %w", i, err)
		}
		if !res.Completed {
			return Result{}, fmt.Errorf("subgraph did not complete at %d", i)
		}
		latencies[i] = dur
	}

	runtime.ReadMemStats(&mEnd)
	return CalculateStats("Scenario D: Dynamic Branching & Subgraph Nesting", latencies, mStart, mEnd), nil
}
