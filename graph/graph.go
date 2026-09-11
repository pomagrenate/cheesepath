package graph

import (
	"fmt"
)

// StateGraph is the builder for stateful, cyclic workflow graphs.
type StateGraph[S any] struct {
	nodes            map[string]NodeFunc[S]
	nodePolicies     map[string]*RetryPolicy
	nodeFallbacks    map[string]string
	nodeGuardrails   map[string][]Guardrail[S]
	edges            map[string][]string
	conditionalEdges map[string][]ConditionalEdge[S]
	entryPoint       string
	finishPoints     map[string]bool
	reducer          Reducer[S]
}

// NewStateGraph creates a new StateGraph builder with an optional state reducer.
// If no reducer is specified, OverwriteReducer is used by default.
func NewStateGraph[S any](reducer ...Reducer[S]) *StateGraph[S] {
	r := OverwriteReducer[S]
	if len(reducer) > 0 && reducer[0] != nil {
		r = reducer[0]
	}
	return &StateGraph[S]{
		nodes:            make(map[string]NodeFunc[S]),
		nodePolicies:     make(map[string]*RetryPolicy),
		nodeFallbacks:    make(map[string]string),
		nodeGuardrails:   make(map[string][]Guardrail[S]),
		edges:            make(map[string][]string),
		conditionalEdges: make(map[string][]ConditionalEdge[S]),
		finishPoints:     make(map[string]bool),
		reducer:          r,
	}
}

// AddNode registers a node function under the given name.
func (g *StateGraph[S]) AddNode(name string, fn NodeFunc[S]) *StateGraph[S] {
	if name == START || name == END {
		panic(fmt.Sprintf("cannot use reserved sentinel name %q as node name", name))
	}
	g.nodes[name] = fn
	return g
}

// AddNodeWithRetry registers a node with an automated retry policy and optional fallback node.
func (g *StateGraph[S]) AddNodeWithRetry(name string, fn NodeFunc[S], policy *RetryPolicy, fallbackNode ...string) *StateGraph[S] {
	g.AddNode(name, fn)
	if policy != nil {
		g.nodePolicies[name] = policy
	}
	if len(fallbackNode) > 0 && fallbackNode[0] != "" {
		g.nodeFallbacks[name] = fallbackNode[0]
	}
	return g
}

// AddGuardrail registers an active state invariant validator on a node.
func (g *StateGraph[S]) AddGuardrail(nodeName string, guardrail Guardrail[S]) *StateGraph[S] {
	g.nodeGuardrails[nodeName] = append(g.nodeGuardrails[nodeName], guardrail)
	return g
}

// AddEdge registers a direct transition from node 'from' to node 'to'.
func (g *StateGraph[S]) AddEdge(from string, to string) *StateGraph[S] {
	if from == START {
		g.entryPoint = to
		return g
	}
	if to == END {
		g.finishPoints[from] = true
		return g
	}
	g.edges[from] = append(g.edges[from], to)
	return g
}

// AddConditionalEdges registers dynamic routing out of node 'from'.
func (g *StateGraph[S]) AddConditionalEdges(from string, router RouterFunc[S], pathMap map[string]string) *StateGraph[S] {
	g.conditionalEdges[from] = append(g.conditionalEdges[from], ConditionalEdge[S]{
		From:    from,
		Router:  router,
		PathMap: pathMap,
	})
	return g
}

// SetEntryPoint defines the initial node to execute when the graph runs.
func (g *StateGraph[S]) SetEntryPoint(name string) *StateGraph[S] {
	g.entryPoint = name
	return g
}

// SetFinishPoint marks a node as terminating into END.
func (g *StateGraph[S]) SetFinishPoint(name string) *StateGraph[S] {
	g.finishPoints[name] = true
	return g
}

// Compile compiles and validates the StateGraph into an executable CompiledGraph.
func (g *StateGraph[S]) Compile(opts ...CompileOption) (*CompiledGraph[S], error) {
	if g.entryPoint == "" {
		return nil, fmt.Errorf("compile: graph must have an entry point (use SetEntryPoint or AddEdge(START, ...))")
	}
	if _, ok := g.nodes[g.entryPoint]; !ok && g.entryPoint != END {
		return nil, fmt.Errorf("compile: entry point %q is not a registered node", g.entryPoint)
	}

	// Validate normal edges
	for from, targets := range g.edges {
		if _, ok := g.nodes[from]; !ok {
			return nil, fmt.Errorf("compile: edge source %q is not a registered node", from)
		}
		for _, to := range targets {
			if _, ok := g.nodes[to]; !ok && to != END {
				return nil, fmt.Errorf("compile: edge target %q from %q is not a registered node", to, from)
			}
		}
	}

	// Validate conditional edge sources
	for from := range g.conditionalEdges {
		if _, ok := g.nodes[from]; !ok {
			return nil, fmt.Errorf("compile: conditional edge source %q is not a registered node", from)
		}
	}

	// Validate fallback targets
	for from, fb := range g.nodeFallbacks {
		if _, ok := g.nodes[fb]; !ok && fb != END {
			return nil, fmt.Errorf("compile: fallback node %q for %q is not a registered node", fb, from)
		}
	}

	cfg := &compileConfig[S]{
		maxRecursionLimit: 50,
		interruptBefore:   make(map[string]bool),
		interruptAfter:    make(map[string]bool),
	}
	for _, o := range opts {
		o(cfg)
	}

	return &CompiledGraph[S]{
		nodes:             g.nodes,
		nodePolicies:     g.nodePolicies,
		nodeFallbacks:    g.nodeFallbacks,
		nodeGuardrails:   g.nodeGuardrails,
		edges:             g.edges,
		conditionalEdges:  g.conditionalEdges,
		entryPoint:        g.entryPoint,
		finishPoints:      g.finishPoints,
		reducer:           g.reducer,
		checkpointer:      cfg.checkpointer,
		maxRecursionLimit: cfg.maxRecursionLimit,
		interruptBefore:   cfg.interruptBefore,
		interruptAfter:    cfg.interruptAfter,
	}, nil
}
