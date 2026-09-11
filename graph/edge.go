package graph

import (
	"context"
)

// RouterFunc determines the next node destination based on the current state.
type RouterFunc[S any] func(ctx context.Context, state S) (string, error)

// ConditionalEdge represents a dynamic branch in the graph whose target node
// is decided at runtime by evaluating a RouterFunc.
type ConditionalEdge[S any] struct {
	From    string
	Router  RouterFunc[S]
	PathMap map[string]string
}

// Route evaluates the router and resolves the destination node name.
func (c *ConditionalEdge[S]) Route(ctx context.Context, state S) (string, error) {
	key, err := c.Router(ctx, state)
	if err != nil {
		return "", err
	}
	if c.PathMap != nil {
		if dest, ok := c.PathMap[key]; ok {
			return dest, nil
		}
	}
	return key, nil
}
