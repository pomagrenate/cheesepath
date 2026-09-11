"""
Publication-grade benchmark script for LangGraph (Python 3.11+)
Designed to mirror the exact same 4 adversarial benchmark scenarios in Cheesepath (Go):
- Scenario A: Deep Sequential Pipeline (50 Nodes)
- Scenario B: Cyclic ReAct Loop (20 Tool Invocations)
- Scenario C: High-Concurrency Saturation (AsyncIO)
- Scenario D: Dynamic Conditional Branching & Subgraph Nesting
"""

import asyncio
import gc
import json
import os
import sys
import time
from typing import Annotated, List, TypedDict
import operator

try:
    from langgraph.graph import StateGraph, START, END
except ImportError:
    print("Error: langgraph is not installed. Run: pip install langgraph")
    sys.exit(1)


def percentile(data, p):
    if not data:
        return 0.0
    sorted_data = sorted(data)
    idx = int(len(sorted_data) * p)
    idx = min(idx, len(sorted_data) - 1)
    return sorted_data[idx]


# ============================================================================
# Scenario A: Deep Sequential State Graph Traversal (50 Nodes)
# ============================================================================

class StateA(TypedDict):
    counter: Annotated[int, operator.add]
    trail: Annotated[List[str], operator.add]


def run_scenario_a(iterations=200, num_nodes=50):
    g = StateGraph(StateA)

    for i in range(num_nodes):
        node_name = f"node_{i}"
        def make_node_func(name):
            def node_func(state: StateA):
                return {"counter": 1, "trail": [name]}
            return node_func

        g.add_node(node_name, make_node_func(node_name))

        if i == 0:
            g.add_edge(START, node_name)
        else:
            g.add_edge(f"node_{i-1}", node_name)

    g.add_edge(f"node_{num_nodes-1}", END)
    app = g.compile()

    # Warmup
    for _ in range(10):
        app.invoke({"counter": 0, "trail": []})

    latencies = []
    gc.disable()
    t0 = time.perf_counter()

    for _ in range(iterations):
        start = time.perf_counter()
        res = app.invoke({"counter": 0, "trail": []})
        dur = time.perf_counter() - start
        assert res["counter"] == num_nodes
        latencies.append(dur * 1e6) # in microseconds

    total_time = time.perf_counter() - t0
    gc.enable()

    return {
        "name": "Scenario A: 50-Node Deep Pipeline",
        "iterations": iterations,
        "p50_us": percentile(latencies, 0.50),
        "p90_us": percentile(latencies, 0.90),
        "p99_us": percentile(latencies, 0.99),
        "mean_us": sum(latencies) / len(latencies),
        "step_transition_us": (sum(latencies) / len(latencies)) / num_nodes,
        "throughput": iterations / total_time,
    }


# ============================================================================
# Scenario B: Cyclic ReAct Loop Execution (20 Turns)
# ============================================================================

class ReactState(TypedDict):
    messages: Annotated[List[dict], operator.add]
    turns: Annotated[int, operator.add]


def run_scenario_b(iterations=200, max_turns=20):
    g = StateGraph(ReactState)

    def agent_node(state: ReactState):
        current_turns = state.get("turns", 0)
        if current_turns < max_turns:
            tool_call = {
                "role": "assistant",
                "tool_calls": [{
                    "id": f"call_{current_turns+1}",
                    "name": "data_fetcher",
                    "arguments": json.dumps({"item_id": current_turns + 1, "query": "fetch_record"})
                }]
            }
            return {"messages": [tool_call], "turns": 1}
        return {"messages": [{"role": "assistant", "content": "Done"}]}

    def tool_node(state: ReactState):
        last_msg = state["messages"][-1]
        tc = last_msg["tool_calls"][0]
        args = json.loads(tc["arguments"])
        result = json.dumps({"status": "ok", "record": args["item_id"], "query": args["query"]})
        return {"messages": [{"role": "tool", "tool_call_id": tc["id"], "content": result}]}

    def router(state: ReactState):
        if state.get("turns", 0) >= max_turns:
            return END
        return "tools"

    g.add_node("agent", agent_node)
    g.add_node("tools", tool_node)

    g.add_edge(START, "agent")
    g.add_conditional_edges("agent", router, {"tools": "tools", END: END})
    g.add_edge("tools", "agent")

    app = g.compile()

    # Warmup
    for _ in range(5):
        app.invoke({"messages": [{"role": "user", "content": "Start"}], "turns": 0})

    latencies = []
    gc.disable()
    t0 = time.perf_counter()

    for _ in range(iterations):
        start = time.perf_counter()
        res = app.invoke({"messages": [{"role": "user", "content": "Start"}], "turns": 0})
        dur = time.perf_counter() - start
        latencies.append(dur * 1e3) # in milliseconds

    total_time = time.perf_counter() - t0
    gc.enable()

    return {
        "name": "Scenario B: Cyclic ReAct Loop (20 Turns)",
        "iterations": iterations,
        "p50_ms": percentile(latencies, 0.50),
        "p90_ms": percentile(latencies, 0.90),
        "p99_ms": percentile(latencies, 0.99),
        "mean_ms": sum(latencies) / len(latencies),
        "throughput": iterations / total_time,
    }


# ============================================================================
# Scenario C: Concurrency Saturation (AsyncIO)
# ============================================================================

class TriageState(TypedDict):
    query: str
    intent: str
    retrieved: Annotated[List[str], operator.add]
    answer: str


async def run_scenario_c(concurrency=500):
    g = StateGraph(TriageState)

    def classifier(state: TriageState):
        return {"intent": "support"}

    def retriever_alpha(state: TriageState):
        return {"retrieved": ["doc_alpha"]}

    def retriever_beta(state: TriageState):
        return {"retrieved": ["doc_beta"]}

    def synthesizer(state: TriageState):
        return {"answer": f"Answer for {state['query']}"}

    g.add_node("classifier", classifier)
    g.add_node("retriever_alpha", retriever_alpha)
    g.add_node("retriever_beta", retriever_beta)
    g.add_node("synthesizer", synthesizer)

    g.add_edge(START, "classifier")
    g.add_edge("classifier", "retriever_alpha")
    g.add_edge("classifier", "retriever_beta")
    g.add_edge("retriever_alpha", "synthesizer")
    g.add_edge("retriever_beta", "synthesizer")
    g.add_edge("synthesizer", END)

    app = g.compile()

    # Warmup
    for _ in range(10):
        await app.ainvoke({"query": "warmup", "intent": "", "retrieved": [], "answer": ""})

    latencies = []
    t0 = time.perf_counter()

    async def worker(idx):
        start = time.perf_counter()
        await app.ainvoke({"query": f"req_{idx}", "intent": "", "retrieved": [], "answer": ""})
        dur = time.perf_counter() - start
        latencies.append(dur * 1e3) # in ms

    tasks = [worker(i) for i in range(concurrency)]
    await asyncio.gather(*tasks)

    total_time = time.perf_counter() - t0

    return {
        "name": f"Scenario C: Concurrency ({concurrency} Workflows)",
        "concurrency": concurrency,
        "total_time_s": total_time,
        "p50_ms": percentile(latencies, 0.50),
        "p90_ms": percentile(latencies, 0.90),
        "p99_ms": percentile(latencies, 0.99),
        "mean_ms": sum(latencies) / len(latencies),
        "throughput": concurrency / total_time,
    }


# ============================================================================
# Scenario D: Dynamic Conditional Branching & Subgraph Nesting
# ============================================================================

class NestedState(TypedDict):
    task_type: str
    depth: Annotated[int, operator.add]
    trail: Annotated[List[str], operator.add]
    completed: bool


def make_subgraph(name: str):
    g = StateGraph(NestedState)
    g.add_node("sub_1", lambda s: {"depth": 1, "trail": [f"{name}_1"]})
    g.add_node("sub_2", lambda s: {"depth": 1, "trail": [f"{name}_2"], "completed": True})
    g.add_edge(START, "sub_1")
    g.add_edge("sub_1", "sub_2")
    g.add_edge("sub_2", END)
    return g.compile()


def run_scenario_d(iterations=200):
    sub_a = make_subgraph("sub_a")
    sub_b = make_subgraph("sub_b")
    sub_c = make_subgraph("sub_c")

    parent = StateGraph(NestedState)
    parent.add_node("orchestrator", lambda s: {"trail": ["orchestrator"]})
    parent.add_node("run_a", lambda s: sub_a.invoke(s))
    parent.add_node("run_b", lambda s: sub_b.invoke(s))
    parent.add_node("run_c", lambda s: sub_c.invoke(s))
    parent.add_node("finalize", lambda s: {"trail": ["finalized"]})

    parent.add_edge(START, "orchestrator")

    def router(s: NestedState):
        t = s.get("task_type", "")
        if t == "task_a":
            return "run_a"
        elif t == "task_b":
            return "run_b"
        return "run_c"

    parent.add_conditional_edges("orchestrator", router, {"run_a": "run_a", "run_b": "run_b", "run_c": "run_c"})
    parent.add_edge("run_a", "finalize")
    parent.add_edge("run_b", "finalize")
    parent.add_edge("run_c", "finalize")
    parent.add_edge("finalize", END)

    app = parent.compile()

    tasks = ["task_a", "task_b", "task_c"]
    # Warmup
    for i in range(10):
        app.invoke({"task_type": tasks[i % 3], "depth": 0, "trail": [], "completed": False})

    latencies = []
    gc.disable()
    t0 = time.perf_counter()

    for i in range(iterations):
        task = tasks[i % 3]
        start = time.perf_counter()
        res = app.invoke({"task_type": task, "depth": 0, "trail": [], "completed": False})
        dur = time.perf_counter() - start
        assert res["completed"] is True
        latencies.append(dur * 1e3) # in ms

    total_time = time.perf_counter() - t0
    gc.enable()

    return {
        "name": "Scenario D: Nested Subgraphs & Dynamic Routing",
        "iterations": iterations,
        "p50_ms": percentile(latencies, 0.50),
        "p90_ms": percentile(latencies, 0.90),
        "p99_ms": percentile(latencies, 0.99),
        "mean_ms": sum(latencies) / len(latencies),
        "throughput": iterations / total_time,
    }


if __name__ == "__main__":
    print("=" * 70)
    print("Running Python LangGraph 1.2+ Benchmark Suite (Zero-Network Mock)")
    print("=" * 70)

    print("\n[1/4] Running Scenario A (50-node deep pipeline)...")
    res_a = run_scenario_a(iterations=300, num_nodes=50)
    print(f"  p50: {res_a['p50_us']:.1f} us | p99: {res_a['p99_us']:.1f} us | step transition: {res_a['step_transition_us']:.2f} us")

    print("\n[2/4] Running Scenario B (20-turn ReAct loop)...")
    res_b = run_scenario_b(iterations=200, max_turns=20)
    print(f"  p50: {res_b['p50_ms']:.2f} ms | p99: {res_b['p99_ms']:.2f} ms | throughput: {res_b['throughput']:.1f} runs/sec")

    print("\n[3/4] Running Scenario C (Concurrency 500 workflows)...")
    res_c = asyncio.run(run_scenario_c(concurrency=500))
    print(f"  p50: {res_c['p50_ms']:.2f} ms | p99: {res_c['p99_ms']:.2f} ms | throughput: {res_c['throughput']:.1f} runs/sec")

    print("\n[4/4] Running Scenario D (Nested Subgraphs)...")
    res_d = run_scenario_d(iterations=200)
    print(f"  p50: {res_d['p50_ms']:.2f} ms | p99: {res_d['p99_ms']:.2f} ms | throughput: {res_d['throughput']:.1f} runs/sec")

    # Output JSON summary for machine parsing
    summary = {
        "scenario_a": res_a,
        "scenario_b": res_b,
        "scenario_c": res_c,
        "scenario_d": res_d,
    }
    with open("benchmark_langgraph_results.json", "w") as f:
        json.dump(summary, f, indent=2)
    print("\nSaved Python LangGraph benchmark results to benchmark_langgraph_results.json")
