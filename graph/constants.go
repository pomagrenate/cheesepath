// Package graph provides the core LangGraph-inspired state graph engine for Cheesepath.
// It enables cyclic, stateful multi-actor workflows with super-step parallel execution,
// state reducers, time-travel checkpointing, human-in-the-loop interrupts, and streaming.
package graph

// START is the sentinel entry point node in a StateGraph.
const START = "__start__"

// END is the sentinel terminal node in a StateGraph.
const END = "__end__"
