package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/AutoCookies/cheesepath/benchmark"
)

func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%.0f ns", float64(d.Nanoseconds()))
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.2f µs", float64(d.Nanoseconds())/1000.0)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2f ms", float64(d.Nanoseconds())/1000000.0)
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}

func runBenchmarkSuite() {
	fmt.Println("======================================================================")
	fmt.Println(" Cheesepath (Go) Publication-Grade Benchmark Suite")
	fmt.Println(" Philosophy: Zero-Network Isolation | Hardware: Performance Fixed")
	fmt.Println("======================================================================")

	// Scenario A
	fmt.Println("\n[1/4] Running Scenario A: Deep Sequential Pipeline (50 Nodes)...")
	resA, err := benchmark.RunScenarioA(1000, 50)
	if err != nil {
		fmt.Printf("Scenario A error: %v\n", err)
		return
	}
	stepA := resA.Mean / 50
	stepP50 := resA.P50 / 50
	stepP99 := resA.P99 / 50
	fmt.Printf("  Traversals: %d | Total p50: %s | Total p99: %s\n",
		resA.Iterations, formatDuration(resA.P50), formatDuration(resA.P99))
	fmt.Printf("  Step Transition (50 nodes): p50=%s | p99=%s | Mean=%s\n",
		formatDuration(stepP50), formatDuration(stepP99), formatDuration(stepA))
	fmt.Printf("  Throughput: %.1f traversals/sec | Heap: %d B/op | %d allocs/op\n",
		resA.Throughput, resA.BytesPerOp, resA.AllocsPerOp)

	// Scenario B
	fmt.Println("\n[2/4] Running Scenario B: Cyclic ReAct Loop (20 Tool Invocations)...")
	resB, err := benchmark.RunScenarioB(500, 20)
	if err != nil {
		fmt.Printf("Scenario B error: %v\n", err)
		return
	}
	stepB := resB.Mean / 20
	fmt.Printf("  Runs: %d | Total p50: %s | Total p99: %s | Mean=%s\n",
		resB.Iterations, formatDuration(resB.P50), formatDuration(resB.P99), formatDuration(resB.Mean))
	fmt.Printf("  Per-Cycle Latency: %s | Throughput: %.1f loops/sec\n",
		formatDuration(stepB), resB.Throughput)
	fmt.Printf("  Allocations: %d B/op | %d allocs/op\n", resB.BytesPerOp, resB.AllocsPerOp)

	// Scenario C
	fmt.Println("\n[3/4] Running Scenario C: High-Concurrency Saturation...")
	concurrencyLevels := []int{100, 1000, 5000, 10000}
	resultsC := make([]benchmark.Result, len(concurrencyLevels))
	for i, c := range concurrencyLevels {
		fmt.Printf("  -> Testing %d simultaneous concurrent workflows...\n", c)
		r, err := benchmark.RunScenarioC(c)
		if err != nil {
			fmt.Printf("Scenario C (%d) error: %v\n", c, err)
			return
		}
		resultsC[i] = r
		fmt.Printf("     Finished in %s | Throughput: %.1f req/s | p50: %s | p99: %s | RSS: %.2f MB\n",
			formatDuration(r.TotalTime), r.Throughput, formatDuration(r.P50), formatDuration(r.P99), r.PeakRSSMB)
	}

	// Scenario D
	fmt.Println("\n[4/4] Running Scenario D: Dynamic Conditional Branching & Subgraphs...")
	resD, err := benchmark.RunScenarioD(1000)
	if err != nil {
		fmt.Printf("Scenario D error: %v\n", err)
		return
	}
	fmt.Printf("  Runs: %d | p50: %s | p99: %s | Mean: %s\n",
		resD.Iterations, formatDuration(resD.P50), formatDuration(resD.P99), formatDuration(resD.Mean))
	fmt.Printf("  Throughput: %.1f runs/sec | Heap: %d B/op | %d allocs/op\n",
		resD.Throughput, resD.BytesPerOp, resD.AllocsPerOp)

	// Save JSON results
	summary := map[string]any{
		"scenario_a": resA,
		"scenario_b": resB,
		"scenario_c": resultsC,
		"scenario_d": resD,
	}
	data, _ := json.MarshalIndent(summary, "", "  ")
	_ = os.WriteFile("benchmark_cheesepath_results.json", data, 0o644)
	fmt.Println("\nSaved Cheesepath (Go) benchmark results to benchmark_cheesepath_results.json")
}
