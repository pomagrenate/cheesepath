# Cheesepath 🧀⚡

**Cheesepath** is an ultra-fast, lightweight alternative to **LangChain** and **LangGraph** written in pure Go (zero CGO, standard library only).

It delivers the complete **LangGraph** paradigm—cyclic state graphs, parallel super-steps, time-travel checkpointing, human-in-the-loop interrupts, and multi-mode streaming—with compile-time type safety and microsecond-level execution latency (**50x–300x faster** than Python).

---

## 📊 Publication-Grade Benchmark Results (Empirical Hardware Isolation)

Both frameworks were evaluated on the **exact same physical machine** (Intel Core i5-4310U @ 2.00GHz, Windows 11 AMD64) using **Zero-Network Mock Isolation** (stubbed LLM & tools at 0ms latency to measure pure framework overhead: state transitions, allocations, serialization, and event loop scheduling).

| Benchmark Scenario | Metric | LangGraph 1.2+ (Python 3.11) | Cheesepath (Go 1.23) | Cheesepath Advantage |
| :--- | :--- | :--- | :--- | :--- |
| **Scenario A: Deep Pipeline**<br>*(50 Sequential Nodes)* | Step Transition Latency<br>Total Traversal (Mean)<br>Total Traversal (p99)<br>Throughput | 666.49 µs<br>33.32 ms<br>118.60 ms<br>30.0 traversals/s | **5.48 µs**<br>**0.27 ms**<br>**1.52 ms**<br>**3,648.3 traversals/s** | ⚡ **122× faster**<br>⚡ **122× faster**<br>⚡ **78× lower tail**<br>⚡ **121.6× throughput** |
| **Scenario B: Cyclic ReAct Loop**<br>*(20 Tool Calls + JSON Schema)* | Loop Completion (Mean)<br>p50 Latency<br>p99 Tail Latency<br>Throughput | 27.23 ms<br>24.71 ms<br>113.20 ms<br>36.7 runs/s | **0.56 ms (563 µs)**<br>**0.99 ms**<br>**2.01 ms**<br>**1,775.4 runs/s** | ⚡ **48.3× faster**<br>⚡ **25× faster**<br>⚡ **56× lower tail**<br>⚡ **48.3× throughput** |
| **Scenario C: Concurrency Saturation**<br>*(Multi-Agent Triage Workflows)* | 500 Workflows Wall-time<br>1,000 Workflows Throughput<br>10,000 Workflows Throughput<br>10,000 Workflows Peak RSS | 2.44 s (500 runs)<br>~200 req/s<br>*OOM / Unviable*<br>*(> 2–4 GB)* | **< 0.01 s**<br>**64,467 req/s**<br>**58,488 req/s**<br>**72.2 MB RAM** | ⚡ **240× faster**<br>⚡ **314× throughput**<br>⚡ **Scales to 10k+**<br>⚡ **30×–50× less RAM** |
| **Scenario D: Nested Subgraphs**<br>*(3 Subgraphs + Conditional Edges)* | Subgraph Run (Mean)<br>p50 Latency<br>p99 Tail Latency<br>Throughput | 5.90 ms<br>5.39 ms<br>14.94 ms<br>169.4 runs/s | **0.043 ms (43.2 µs)**<br>**< 10 µs**<br>**0.55 ms**<br>**23,128.0 runs/s** | ⚡ **136.5× faster**<br>⚡ **500×+ faster**<br>⚡ **27× lower tail**<br>⚡ **136.5× throughput** |

---

## Key Features

- **🚀 Type-Safe StateGraph Engine (`graph.StateGraph[S any]`)**: Build cyclic, stateful multi-agent workflows using Go generics.
- **⚡ Parallel Goroutine Super-Steps**: Automatically executes branching nodes concurrently with zero thread overhead (~5–18µs per step).
- **🔄 State Channels & Reducers**: Merge state updates reliably with composable reducers (`Overwrite`, `Append`, or custom functions).
- **⏳ Checkpointing & Time-Travel**: Thread-scoped state snapshots (`MemorySaver`, `FileSaver`) with state history and branching time-travel.
- **🛑 Human-in-the-Loop (HITL)**: Interrupt graph execution before or after specific nodes, inspect state, and resume with external approval/feedback.
- **🔌 Unified ChatModel Abstraction**: Native tool calling, streaming tokens, and full OpenAI / Cheesecrab / Ollama / vLLM compatibility.
- **🧱 Composable Runnables (LCEL in Go)**: Build pipelines using `Pipe`, `Parallel`, `Branch`, and `Fallback`.
- **🤖 Prebuilt ReAct Agent**: Instantiate production-ready ReAct agents as compiled state graphs in seconds.

---

## Architecture

```
cheesepath/
├── core/                  # Core primitives: Message, Tool, ChatModel, Runnable (LCEL)
├── graph/                 # 🚀 The LangGraph Engine (StateGraph, Checkpointer, Interrupts, Supersteps)
├── providers/
│   ├── openai/            # OpenAI-compatible ChatModel with native tool calling & streaming
│   └── mock/              # Blazing-fast in-memory mock ChatModel for testing
├── agent/                 # Prebuilt Graph Agents (ReAct Graph, Multi-Agent Supervisors)
├── chain/                 # Composable LLM pipelines (LLMChain, Sequential, MapReduce)
├── memory/                # Conversation memory (Buffer, Sliding Window, Summary, Vector)
├── tools/                 # Pure standard library tools (fs, shell, git, http)
├── benchmark/             # Publication-grade benchmark suite (Scenarios A, B, C, D)
└── cmd/benchmark/         # Standalone CLI benchmark runner
```

---

## Quick Start: ReAct Agent on StateGraph

```go
package main

import (
	"context"
	"fmt"

	"github.com/AutoCookies/cheesepath/agent"
	"github.com/AutoCookies/cheesepath/core"
	"github.com/AutoCookies/cheesepath/providers"
)

func main() {
	ctx := context.Background()

	// 1. Initialize an OpenAI-compatible ChatModel (Cheesecrab, Ollama, OpenAI)
	model := providers.NewOpenAIClient(
		providers.WithBaseURL("http://127.0.0.1:8081"),
		providers.WithModel("qwen2.5"),
	)

	// 2. Define tools
	calcTool := core.NewFuncTool(
		"calculator",
		"Evaluate basic arithmetic calculations",
		map[string]any{"type": "object"},
		func(ctx context.Context, args map[string]any) (string, error) {
			return "42", nil
		},
	)

	// 3. Create the ReAct agent (compiled as a StateGraph: Agent ⇄ Tools)
	app, err := agent.CreateReactAgent(model, []core.Tool{calcTool})
	if err != nil {
		panic(err)
	}

	// 4. Run the graph
	finalState, err := app.Invoke(ctx, agent.ReactState{
		Messages: []core.Message{
			core.NewHumanMessage("What is 6 * 7?"),
		},
	})
	if err != nil {
		panic(err)
	}

	lastMsg := finalState.Messages[len(finalState.Messages)-1]
	fmt.Println("Agent response:", lastMsg.Content)
}
```

---

## Reproducing the Benchmarks

Both the Go and Python benchmark suites can be executed directly:

```bash
# 1. Run Cheesepath (Go) benchmarks
go run ./cmd/benchmark/main.go

# 2. Run LangGraph (Python) benchmarks
python benchmark/python/benchmark_langgraph.py
```

---

## Requirements

- Go 1.23+
- Any local or remote OpenAI-compatible server ([Cheesecrab](https://github.com/AutoCookies/cheesecrab), Ollama, vLLM, OpenAI, Groq)
- Pure Go standard library (zero CGO, zero external dependencies)
