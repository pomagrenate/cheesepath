package benchmark

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/AutoCookies/cheesepath/graph"
)

// StateA represents state in the 50-node deep sequential pipeline.
type StateA struct {
	Counter int      `json:"counter"`
	Trail   []string `json:"trail"`
}

func stateAReducer(cur, up StateA) StateA {
	return StateA{
		Counter: cur.Counter + up.Counter,
		Trail:   append(cur.Trail, up.Trail...),
	}
}

// BuildScenarioAGraph builds a 50-node strictly linear StateGraph.
func BuildScenarioAGraph(numNodes int) (*graph.CompiledGraph[StateA], error) {
	if numNodes <= 0 {
		numNodes = 50
	}
	g := graph.NewStateGraph[StateA](stateAReducer)

	for i := 0; i < numNodes; i++ {
		nodeName := fmt.Sprintf("node_%d", i)
		g.AddNode(nodeName, func(ctx context.Context, s StateA) (StateA, error) {
			return StateA{
				Counter: 1,
				Trail:   []string{nodeName},
			}, nil
		})

		if i == 0 {
			g.SetEntryPoint(nodeName)
		} else {
			prevNode := fmt.Sprintf("node_%d", i-1)
			g.AddEdge(prevNode, nodeName)
		}
	}

	lastNode := fmt.Sprintf("node_%d", numNodes-1)
	g.SetFinishPoint(lastNode)

	return g.Compile(graph.WithMaxRecursionLimit[StateA](numNodes + 10))
}

// RunScenarioA executes Scenario A for iterations runs after warmup.
func RunScenarioA(iterations int, numNodes int) (Result, error) {
	app, err := BuildScenarioAGraph(numNodes)
	if err != nil {
		return Result{}, err
	}

	ctx := context.Background()

	// Warmup phase (mitigate cold CPU cache)
	for i := 0; i < 20; i++ {
		_, _ = app.Invoke(ctx, StateA{})
	}

	latencies := make([]time.Duration, iterations)
	runtime.GC()

	var mStart, mEnd runtime.MemStats
	runtime.ReadMemStats(&mStart)

	for i := 0; i < iterations; i++ {
		start := time.Now()
		res, err := app.Invoke(ctx, StateA{})
		dur := time.Since(start)
		if err != nil {
			return Result{}, fmt.Errorf("invoke failed at iteration %d: %w", i, err)
		}
		if res.Counter != numNodes {
			return Result{}, fmt.Errorf("expected counter %d, got %d", numNodes, res.Counter)
		}
		latencies[i] = dur
	}

	runtime.ReadMemStats(&mEnd)
	return CalculateStats("Scenario A: Deep Sequential Pipeline (50 Nodes)", latencies, mStart, mEnd), nil
}
