package benchmark

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/AutoCookies/cheesepath/graph"
)

// TriageState represents state in the concurrent triage workflow.
type TriageState struct {
	Query     string   `json:"query"`
	Intent    string   `json:"intent"`
	Retrieved []string `json:"retrieved"`
	Answer    string   `json:"answer"`
}

func triageReducer(cur, up TriageState) TriageState {
	intent := cur.Intent
	if up.Intent != "" {
		intent = up.Intent
	}
	ans := cur.Answer
	if up.Answer != "" {
		ans = up.Answer
	}
	return TriageState{
		Query:     cur.Query,
		Intent:    intent,
		Retrieved: append(cur.Retrieved, up.Retrieved...),
		Answer:    ans,
	}
}

// BuildTriageGraph builds the multi-agent triage graph with parallel retrieval.
func BuildTriageGraph() (*graph.CompiledGraph[TriageState], error) {
	g := graph.NewStateGraph[TriageState](triageReducer)

	g.AddNode("classifier", func(ctx context.Context, s TriageState) (TriageState, error) {
		return TriageState{Intent: "technical_support"}, nil
	})

	g.AddNode("retriever_alpha", func(ctx context.Context, s TriageState) (TriageState, error) {
		return TriageState{Retrieved: []string{"doc_manual_page_42"}}, nil
	})

	g.AddNode("retriever_beta", func(ctx context.Context, s TriageState) (TriageState, error) {
		return TriageState{Retrieved: []string{"doc_knowledge_base_article_99"}}, nil
	})

	g.AddNode("synthesizer", func(ctx context.Context, s TriageState) (TriageState, error) {
		ans := fmt.Sprintf("Answer for %q based on %d docs", s.Query, len(s.Retrieved))
		return TriageState{Answer: ans}, nil
	})

	g.SetEntryPoint("classifier")
	// Parallel fan-out
	g.AddEdge("classifier", "retriever_alpha")
	g.AddEdge("classifier", "retriever_beta")
	// Fan-in
	g.AddEdge("retriever_alpha", "synthesizer")
	g.AddEdge("retriever_beta", "synthesizer")
	g.SetFinishPoint("synthesizer")

	return g.Compile()
}

// RunScenarioC executes Scenario C with the given concurrency level.
func RunScenarioC(concurrency int) (Result, error) {
	app, err := BuildTriageGraph()
	if err != nil {
		return Result{}, err
	}

	ctx := context.Background()

	// Warmup
	for i := 0; i < 20; i++ {
		_, _ = app.Invoke(ctx, TriageState{Query: "warmup"})
	}

	latencies := make([]time.Duration, concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)

	runtime.GC()
	var mStart, mEnd runtime.MemStats
	runtime.ReadMemStats(&mStart)

	overallStart := time.Now()

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			start := time.Now()
			_, _ = app.Invoke(ctx, TriageState{Query: fmt.Sprintf("req-%d", idx)})
			latencies[idx] = time.Since(start)
		}(i)
	}

	wg.Wait()
	overallDur := time.Since(overallStart)
	runtime.ReadMemStats(&mEnd)

	res := CalculateStats(fmt.Sprintf("Scenario C: Concurrency %d Workflows", concurrency), latencies, mStart, mEnd)
	res.TotalTime = overallDur
	if overallDur > 0 {
		res.Throughput = float64(concurrency) / overallDur.Seconds()
	}
	return res, nil
}
